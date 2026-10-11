package ami

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitTest(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	s, err := gitCommand(context.Background(), cwd, args...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func initRepo(t *testing.T, path string) {
	gitTest(t, path, "init", "-b", "main")
	gitTest(t, path, "config", "user.email", "fixture@example.invalid")
	gitTest(t, path, "config", "user.name", "fixture")
	gitTest(t, path, "config", "commit.gpgsign", "false")
	gitTest(t, path, "config", "core.autocrlf", "false")
}
func TestGitCommitAndPushLocalRemote(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	os.Mkdir(remote, 0755)
	gitTest(t, remote, "init", "--bare", "-b", "main")
	repo := filepath.Join(root, "repo")
	os.Mkdir(repo, 0755)
	initRepo(t, repo)
	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0644)
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "initial")
	gitTest(t, repo, "remote", "add", "origin", remote)
	gitTest(t, repo, "push", "-u", "origin", "main")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { completeReply(w, "fix: update fixture", 100) }))
	defer server.Close()
	t.Setenv("AMI_BASE_URL", server.URL)
	t.Setenv("AMI_API_KEY", "test-only-key")
	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("updated\n"), 0644)
	app := application{out: io.Discard, err: io.Discard}
	ctx := context.Background()
	committed, err := app.commitFlow(ctx, repo, true, true, "")
	if err != nil || !committed {
		t.Fatal(committed, err)
	}
	if gitTest(t, repo, "log", "-1", "--pretty=%s") != "fix: update fixture" {
		t.Fatal("commit message incorrect")
	}
	if err = app.push(ctx, repo, arguments{Bools: map[string]bool{"yes": true}}); err != nil {
		t.Fatal(err)
	}
	if gitTest(t, remote, "rev-parse", "main") != gitTest(t, repo, "rev-parse", "HEAD") {
		t.Fatal("push not applied")
	}
	if err = app.push(ctx, repo, arguments{Bools: map[string]bool{"yes": true}}); err != nil {
		t.Fatal("no-op push", err)
	}
}
func TestGitRebaseRequiresExplicitApprovalAndPreservesConflicts(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	os.Mkdir(remote, 0755)
	gitTest(t, remote, "init", "--bare", "-b", "main")
	a := filepath.Join(root, "a")
	os.Mkdir(a, 0755)
	initRepo(t, a)
	os.WriteFile(filepath.Join(a, "file.txt"), []byte("base\n"), 0644)
	gitTest(t, a, "add", ".")
	gitTest(t, a, "commit", "-m", "base")
	gitTest(t, a, "remote", "add", "origin", remote)
	gitTest(t, a, "push", "-u", "origin", "main")
	b := filepath.Join(root, "b")
	gitTest(t, root, "clone", remote, b)
	gitTest(t, b, "config", "user.email", "fixture@example.invalid")
	gitTest(t, b, "config", "user.name", "fixture")
	gitTest(t, b, "config", "commit.gpgsign", "false")
	os.WriteFile(filepath.Join(b, "file.txt"), []byte("remote\n"), 0644)
	gitTest(t, b, "add", ".")
	gitTest(t, b, "commit", "-m", "remote")
	gitTest(t, b, "push")
	os.WriteFile(filepath.Join(a, "file.txt"), []byte("local\n"), 0644)
	gitTest(t, a, "add", ".")
	gitTest(t, a, "commit", "-m", "local")
	before := gitTest(t, a, "rev-parse", "HEAD")
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	app := application{in: stdin, out: io.Discard, err: io.Discard}
	err = app.push(context.Background(), a, arguments{Bools: map[string]bool{"yes": true}})
	if err == nil || !strings.Contains(err.Error(), "--rebase --yes") {
		t.Fatal("--yes silently authorized rebase", err)
	}
	if gitTest(t, a, "rev-parse", "HEAD") != before {
		t.Fatal("unapproved rebase changed branch")
	}
	err = app.push(context.Background(), a, arguments{Bools: map[string]bool{"yes": true, "rebase": true}})
	if err == nil || !strings.Contains(err.Error(), "working state preserved") {
		t.Fatal(err)
	}
	if gitTest(t, a, "diff", "--name-only", "--diff-filter=U") != "file.txt" {
		t.Fatal("rebase conflicts were resolved/aborted automatically")
	}
}
func TestNativeDownloadVerifiesChecksumBeforeReplacement(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			payload := []byte("native fixture bytes")
			sum := sha256.Sum256(payload)
			checksum := fmt.Sprintf("%x", sum)
			if !valid {
				checksum = strings.Repeat("0", 64)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/checksums.txt" {
					fmt.Fprintf(w, "%s  %s\n", checksum, assetName())
				} else {
					w.Write(payload)
				}
			}))
			defer server.Close()
			release := release{Tag: "v0.5.0"}
			release.Assets = append(release.Assets, struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}{"checksums.txt", server.URL + "/checksums.txt"}, struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}{assetName(), server.URL + "/binary"})
			target := filepath.Join(t.TempDir(), "new-ami")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := downloadNative(ctx, release, target)
			if valid {
				if err != nil {
					t.Fatal(err)
				}
				b, _ := os.ReadFile(target)
				if string(b) != string(payload) {
					t.Fatal("download mismatch")
				}
			} else {
				if err == nil {
					t.Fatal("invalid checksum accepted")
				}
				if _, err = os.Stat(target); !os.IsNotExist(err) {
					t.Fatal("failed download left executable")
				}
			}
		})
	}
}

func TestCachedUpdateNoticeDoesNotWaitForNetwork(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path := filepath.Join(cache, "ami", "native-update.json")
	if err := saveJSON(path, event{"checked": time.Now().Unix(), "version": "v0.6.0"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, stop := updateNotice(ctx, "0.5.0")
	defer stop()
	select {
	case version := <-result:
		if version != "v0.6.0" {
			t.Fatal(version)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cache read waited for network")
	}
}
