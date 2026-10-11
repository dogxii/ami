package ami

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvalidConfigStopsBeforeRequestsAndSessions(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer server.Close()
	config := filepath.Join(t.TempDir(), "config.json")
	state := t.TempDir()
	env := []string{"AMI_CONFIG_FILE=" + config, "AMI_STATE_DIR=" + state, "AMI_BASE_URL=" + server.URL, "AMI_MODEL=fixture", "AMI_API_KEY=fixture-secret"}
	for _, content := range []string{`{"model":`, `{"model":123}`, `null`} {
		os.WriteFile(config, []byte(content), 0600)
		out, _, err := cliRun(t, t.TempDir(), "", env, "--json", "task")
		if err == nil || !strings.Contains(out, "invalid config file") {
			t.Fatal("invalid config was accepted", out, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid config sent an API request")
	}
	if _, err := os.Stat(filepath.Join(state, "sessions")); !os.IsNotExist(err) {
		t.Fatal("invalid config created a session", err)
	}
}

func TestDoctorOfflineAndOptInAPI(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body event
		json.NewDecoder(r.Body).Decode(&body)
		if len(body["tools"].([]any)) != 1 {
			t.Error("tool schema not checked")
		}
		sseReply(w, message("assistant", "READY"), 10)
	}))
	defer server.Close()
	env := []string{"AMI_CONFIG_FILE=" + filepath.Join(t.TempDir(), "missing.json"), "AMI_STATE_DIR=" + t.TempDir(), "AMI_BASE_URL=" + server.URL, "AMI_MODEL=fixture", "AMI_API_KEY=private-fixture-secret"}
	cwd := t.TempDir()
	for _, check := range []bool{false, true} {
		args := []string{"doctor", "--json"}
		if check {
			args = append(args, "--check")
		}
		out, stderr, err := cliRun(t, cwd, "", env, args...)
		if err != nil {
			t.Fatal(out, stderr, err)
		}
		if strings.Contains(out, "private-fixture-secret") {
			t.Fatal("doctor leaked key")
		}
		if !check && requests.Load() != 0 {
			t.Fatal("offline doctor called API")
		}
		var report event
		if json.Unmarshal([]byte(out), &report) != nil || report["ok"] != true {
			t.Fatal(out)
		}
	}
	if requests.Load() != 1 {
		t.Fatal(requests.Load())
	}
}

func TestSessionLockHelper(t *testing.T) {
	id := os.Getenv("AMI_LOCK_TEST_ID")
	if id == "" {
		return
	}
	s, err := openSession(os.Getenv("AMI_LOCK_TEST_CWD"), id, true, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	fmt.Fprintln(os.Stdout, "locked")
	for {
		time.Sleep(time.Second)
	}
}

func TestSessionLockAcrossProcessesAndCrashRelease(t *testing.T) {
	r := testRuntime(t, true)
	id, cwd := r.Session.ID, r.Session.Cwd
	r.Session.close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, exe, "-test.run=^TestSessionLockHelper$", "--", processHelperFlag)
	child.Env = append(os.Environ(), "AMI_LOCK_TEST_ID="+id, "AMI_LOCK_TEST_CWD="+cwd)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatal(line, err, stderr.String())
	}
	if s, err := openSession(cwd, id, true, 256); !errors.Is(err, errSessionBusy) {
		if s != nil {
			s.close()
		}
		t.Fatal("concurrent session accepted", err)
	}
	if _, err := deleteSession(cwd, id, time.Time{}); !errors.Is(err, errSessionBusy) {
		t.Fatal("active session deletion accepted", err)
	}
	child.Process.Kill()
	child.Wait()
	s, err := openSession(cwd, id, true, 256)
	if err != nil {
		t.Fatal("kernel lock survived process exit", err)
	}
	s.close()
}

func TestSessionMaintenanceScopeDryRunAndActiveSkip(t *testing.T) {
	r := testRuntime(t, true)
	r.Session.add(message("user", "repair a test"))
	r.Session.Artifacts.save("retained output")
	old := time.Now().Add(-40 * 24 * time.Hour)
	log := filepath.Join(r.Session.Directory, "events.jsonl")
	os.Chtimes(log, old, old)
	var out bytes.Buffer
	app := application{out: &out, err: io.Discard}
	args := arguments{Values: map[string]string{"older-than": "30"}, Bools: map[string]bool{"dry-run": true}, Pos: []string{"sessions", "prune"}}
	if err := app.sessions(context.Background(), r.Session.Cwd, args); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), r.Session.ID) {
		t.Fatal("missing prune candidate", out.String())
	}
	args.Bools = map[string]bool{"yes": true}
	out.Reset()
	if err := app.sessions(context.Background(), r.Session.Cwd, args); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"skipped":["`+r.Session.ID) {
		t.Fatal("active session not skipped", out.String())
	}
	r.Session.close()
	if _, err := deleteSession(t.TempDir(), r.Session.ID, time.Time{}); err == nil {
		t.Fatal("cross-workspace deletion accepted")
	}
	info, err := inspectSession(r.Session.Cwd, r.Session.ID)
	if err != nil || info.Task != "repair a test" || info.Bytes < 15 {
		t.Fatal(info, err)
	}
	// An old listing must not delete a session that has since been used.
	os.Chtimes(log, time.Now(), time.Now())
	if removed, err := deleteSession(r.Session.Cwd, r.Session.ID, time.Now().Add(-30*24*time.Hour)); err != nil || removed {
		t.Fatal("recent session deleted", removed, err)
	}
	os.Chtimes(log, old, old)
	if err := app.sessions(context.Background(), r.Session.Cwd, args); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.Session.Directory); !os.IsNotExist(err) {
		t.Fatal("prune did not remove outputs", err)
	}
}

func TestPreviewBeforePermissionAndDeniedCommandDoesNotStart(t *testing.T) {
	r := testRuntime(t, false)
	r.Mode = "ask"
	var output bytes.Buffer
	view := renderer{out: io.Discard, err: &output}
	r.Emit = func(kind string, e event) error { e["type"] = kind; return view.render(e) }
	r.Confirm = func(ctx context.Context, label string) (bool, error) {
		if !strings.Contains(output.String(), "echo blocked") || !strings.Contains(output.String(), r.Session.Cwd) {
			t.Fatal("permission shown before command preview", output.String())
		}
		return false, nil
	}
	if _, err := executeTool(r, "exec_command", event{"command": "echo blocked"}, toolAllowlist(toolCatalog)); err == nil {
		t.Fatal("denied command ran")
	}
	if len(r.Processes.jobs) != 0 {
		t.Fatal("denied command spawned a process")
	}
}

func TestScopedInstructionsPreventEditUntilReviewed(t *testing.T) {
	r := testRuntime(t, false)
	root := filepath.Dir(r.Session.Cwd)
	os.Mkdir(filepath.Join(root, ".git"), 0700)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("parent convention"), 0600)
	os.Mkdir(filepath.Join(r.Session.Cwd, "child"), 0700)
	os.WriteFile(filepath.Join(r.Session.Cwd, "child", "AGENTS.md"), []byte("child convention"), 0600)
	path := filepath.Join(r.Session.Cwd, "child", "code.txt")
	os.WriteFile(path, []byte("before"), 0600)
	initial, err := loadInstructions(r)
	if err != nil || !strings.Contains(initial, "parent convention") {
		t.Fatal(initial, err)
	}
	callTool(t, r, "read_file", event{"path": "child/code.txt"})
	input := event{"path": "child/code.txt", "oldText": "before", "newText": "after"}
	_, err = executeTool(r, "edit_file", input, toolAllowlist(toolCatalog))
	if err == nil || !strings.Contains(err.Error(), "child convention") {
		t.Fatal("scoped rules were skipped", err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "before" {
		t.Fatal("edit ran before instructions were returned")
	}
	callTool(t, r, "edit_file", input)
	content, _ = os.ReadFile(path)
	if string(content) != "after" || r.Changes["child/code.txt"] != "edit_file" {
		t.Fatal("reviewed edit failed", string(content), r.Changes)
	}
}

func TestRetryAfterRespectedAndCancellationInterruptsWait(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(429)
	}))
	defer server.Close()
	started := time.Now()
	_, _, err := request(context.Background(), server.URL, event{}, "fixture", 100*time.Millisecond, nil)
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 1 || time.Since(started) > time.Second {
		t.Fatal("rate-limit retry ignored delay/cancellation", err, requests.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpdateCheckAndPinnedReleaseDoNotDownload(t *testing.T) {
	previous := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = previous })
	var endpoints []string
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		endpoints = append(endpoints, r.URL.String())
		tag := "v0.7.0"
		if strings.HasSuffix(r.URL.Path, "/tags/v0.5.1") {
			tag = "v0.5.1"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(jsonText(event{"tag_name": tag})))}, nil
	})}
	var out bytes.Buffer
	app := application{version: "0.6.0", out: &out, err: io.Discard}
	if err := app.update(context.Background(), arguments{Values: map[string]string{}, Bools: map[string]bool{"check": true}}); err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 || !strings.Contains(out.String(), "Latest: v0.7.0") {
		t.Fatal(endpoints, out.String())
	}
	if err := app.update(context.Background(), arguments{Values: map[string]string{"to": "+1.2.3"}, Bools: map[string]bool{"check": true}}); err == nil {
		t.Fatal("invalid pinned version accepted")
	}
	if len(endpoints) != 1 {
		t.Fatal("invalid version made a request")
	}
	out.Reset()
	if err := app.update(context.Background(), arguments{Values: map[string]string{"to": "0.5.1"}, Bools: map[string]bool{"check": true}}); err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 2 || !strings.HasSuffix(endpoints[1], "/tags/v0.5.1") || !strings.Contains(out.String(), "Target: v0.5.1") {
		t.Fatal(endpoints, out.String())
	}
}

func TestNetworkFailurePreservesCause(t *testing.T) {
	previous := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = previous })
	cause := errors.New("fixture connection failed")
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, cause })}
	_, _, err := request(context.Background(), "https://fixture.invalid", event{}, "fixture", 3*time.Second, nil)
	if !errors.Is(err, cause) || errors.Is(err, context.Canceled) {
		t.Fatal("network error was replaced by cancellation", err)
	}
}

func TestFailedUpdateAttemptBacksOffAndKeepsKnownNotice(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path := filepath.Join(cache, "ami", "native-update.json")
	saveJSON(path, event{"checked": time.Now().Unix() - 90000, "version": "v0.7.0"})
	previous := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = previous })
	called := make(chan struct{})
	var requests atomic.Int32
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			close(called)
		}
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
	})}
	_, stop := updateNotice(context.Background(), "0.6.0")
	defer stop()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("update attempt not made")
	}
	result, stopSecond := updateNotice(context.Background(), "0.6.0")
	defer stopSecond()
	select {
	case version := <-result:
		if version != "v0.7.0" {
			t.Fatal(version)
		}
	case <-time.After(time.Second):
		t.Fatal("failed cache lost known update")
	}
	if requests.Load() != 1 {
		t.Fatal("failed attempt retried without backoff", requests.Load())
	}
}

func TestDeletingLatestFallsBackToRemainingTask(t *testing.T) {
	r := testRuntime(t, true)
	r.Session.add(message("user", "first task"))
	r.Session.close()
	second, err := openSession(r.Session.Cwd, "", true, 256)
	if err != nil {
		t.Fatal(err)
	}
	second.add(message("user", "second task"))
	second.activate()
	second.close()
	if _, err = deleteSession(r.Session.Cwd, second.ID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	id, err := sessionID(r.Session.Cwd, "latest")
	if err != nil || id != r.Session.ID {
		t.Fatal("latest deletion stranded continuation", id, err)
	}
}
