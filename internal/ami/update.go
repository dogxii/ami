package ami

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releaseAPI = "https://api.github.com/repos/dogxii/ami/releases/latest"

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func get(ctx context.Context, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ami")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("download failed (%d)", res.StatusCode)
	}
	return res, nil
}
func latestRelease(ctx context.Context, endpoint string) (release, error) {
	res, err := get(ctx, endpoint)
	if err != nil {
		return release{}, err
	}
	defer res.Body.Close()
	var r release
	err = json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&r)
	if err == nil && !validReleaseTag(r.Tag) {
		err = fmt.Errorf("invalid release version")
	}
	return r, err
}
func validReleaseTag(tag string) bool {
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return false
		}
	}
	return true
}
func newerVersion(current, next string) bool {
	if !validReleaseTag(next) {
		return false
	}
	a := strings.Split(strings.TrimPrefix(current, "v"), ".")
	b := strings.Split(strings.TrimPrefix(next, "v"), ".")
	if len(a) != 3 {
		return true
	}
	for i := range b {
		left, err := strconv.Atoi(a[i])
		if err != nil {
			return true
		}
		right, _ := strconv.Atoi(b[i])
		if right != left {
			return right > left
		}
	}
	return false
}
func assetName() string {
	name := "ami_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}
func downloadNative(ctx context.Context, r release, target string) error {
	assetURL, checksumURL := "", ""
	for _, asset := range r.Assets {
		if asset.Name == assetName() {
			assetURL = asset.URL
		}
		if asset.Name == "checksums.txt" {
			checksumURL = asset.URL
		}
	}
	if assetURL == "" || checksumURL == "" {
		return fmt.Errorf("release has no executable/checksum for %s", assetName())
	}
	res, err := get(ctx, checksumURL)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(io.LimitReader(res.Body, 1024*1024))
	expected := ""
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == assetName() {
			expected = fields[0]
		}
	}
	scanErr := scanner.Err()
	res.Body.Close()
	if scanErr != nil {
		return scanErr
	}
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("missing or invalid executable checksum")
	}
	res, err = get(ctx, assetURL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			os.Remove(target)
		}
	}()
	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(f, hash), res.Body); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("executable checksum mismatch")
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}
func (app *application) update(ctx context.Context, a arguments) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	r, err := latestRelease(ctx, releaseAPI)
	if err != nil {
		return err
	}
	fmt.Fprintf(app.out, "Current: %s\nLatest: %s\n", app.version, r.Tag)
	if !newerVersion(app.version, r.Tag) {
		fmt.Fprintln(app.out, "Ami is up to date")
		return nil
	}
	if !a.Bools["yes"] {
		yes, err := app.confirm(ctx, "Update ami to "+r.Tag+"?")
		if err != nil {
			return fmt.Errorf("use ami update --yes to confirm non-interactive update")
		}
		if !yes {
			return nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	target := filepath.Join(filepath.Dir(exe), ".ami-update-"+uuid())
	if err = downloadNative(ctx, r, target); err != nil {
		return err
	}
	defer os.Remove(target)
	if err = replaceExecutable(target, exe); err != nil {
		return fmt.Errorf("cannot replace executable; install the release manually: %w", err)
	}
	fmt.Fprintln(app.out, "Updated to "+r.Tag)
	return nil
}
func replaceExecutable(source, target string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(source, target)
	}
	backup := target + ".old"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

// A cached, optional notification never waits on the network at task completion.
func updateNotice(ctx context.Context, version string) (<-chan string, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	result := make(chan string, 1)
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache")
	}
	path := filepath.Join(cacheDir, "ami", "native-update.json")
	go func() {
		var cached struct {
			Checked int64  `json:"checked"`
			Version string `json:"version"`
		}
		b, err := os.ReadFile(path)
		if err == nil && json.Unmarshal(b, &cached) == nil && time.Now().Unix()-cached.Checked < 86400 && time.Now().Unix() >= cached.Checked {
			if newerVersion(version, cached.Version) {
				result <- cached.Version
			}
			return
		}
		r, err := latestRelease(ctx, releaseAPI)
		if err != nil {
			return
		}
		cached.Checked = time.Now().Unix()
		cached.Version = r.Tag
		_ = saveJSON(path, cached)
		if newerVersion(version, r.Tag) {
			result <- r.Tag
		}
	}()
	return result, cancel
}
