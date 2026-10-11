package ami

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var cliBinary string

func TestMain(m *testing.M) {
	if os.Getenv("AMI_PROCESS_HELPER") == "1" {
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "ami-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cliBinary = filepath.Join(dir, "ami")
	if runtime.GOOS == "windows" {
		cliBinary += ".exe"
	}
	build := exec.Command("go", "build", "-o", cliBinary, "../../cmd/ami")
	if b, err := build.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(b), err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func cliRun(t *testing.T, cwd, input string, env []string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliBinary, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(input)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	return out.String(), stderr.String(), err
}
func parseEvents(t *testing.T, s string) []event {
	t.Helper()
	result := []event{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		var e event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal("non-JSON CLI output", line, err)
		}
		result = append(result, e)
	}
	return result
}
func TestCLIJSONStdinPersistenceAndContinuation(t *testing.T) {
	cwd := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body event
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		requests++
		if requests == 1 && !strings.Contains(jsonText(body["messages"]), "Complete input saved") {
			t.Error("stdin artifact pointer missing")
		}
		if requests == 2 && !strings.Contains(jsonText(body["messages"]), "Completed actions must not be replayed") {
			t.Error("continuation context missing")
		}
		sseReply(w, message("assistant", "done"), 100)
	}))
	defer server.Close()
	env := []string{"AMI_BASE_URL=" + server.URL, "AMI_MODEL=fixture", "AMI_API_KEY=test-only-key", "AMI_STATE_DIR=" + state}
	out, stderr, err := cliRun(t, cwd, strings.Repeat("input\n", 10000), env, "--json", "--yes", "inspect")
	if err != nil {
		t.Fatal(err, stderr)
	}
	events := parseEvents(t, out)
	id := events[0]["sessionId"].(string)
	if events[len(events)-1]["type"] != "run.completed" {
		t.Fatal(events)
	}
	root, _ := canonical(cwd)
	latest, _ := os.ReadFile(latestPathFor(state, root))
	if string(latest) != id {
		t.Fatal("latest task not set", string(latest), id)
	}
	out, stderr, err = cliRun(t, cwd, "", env, "--json", "--continue", "continue")
	if err != nil {
		t.Fatal(err, stderr)
	}
	events = parseEvents(t, out)
	if events[0]["sessionId"] != id {
		t.Fatal("wrong resumed session")
	}
	out, stderr, err = cliRun(t, cwd, "", env, "--json", "--no-session", "inspect")
	if err != nil {
		t.Fatal(err, stderr)
	}
	events = parseEvents(t, out)
	if events[0]["persistent"] != false {
		t.Fatal(events)
	}
	directory := events[0]["outputDirectory"].(string)
	defer os.RemoveAll(filepath.Dir(directory))
	if _, err = os.Stat(filepath.Join(filepath.Dir(directory), "events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("no-session persisted transcript")
	}
}
func latestPathFor(state, cwd string) string {
	return filepath.Join(state, "sessions", filepath.Base(latestPath(cwd)))
}
func TestCLIValidationAndSingleTerminalFailure(t *testing.T) {
	cwd := t.TempDir()
	env := []string{"AMI_STATE_DIR=" + t.TempDir(), "AMI_API_KEY="}
	for _, args := range [][]string{{"--json", "--yes", "--ask", "task"}, {"--json", "--session"}, {"--json", "--context-window", "1", "task"}, {"--json", "--no-session", "--continue", "task"}} {
		out, _, err := cliRun(t, cwd, "", env, args...)
		if err == nil {
			t.Fatal("invalid options accepted", args)
		}
		events := parseEvents(t, out)
		if len(events) != 1 || events[0]["type"] != "run.failed" {
			t.Fatal("unexpected validation events", events)
		}
	}
	out, _, err := cliRun(t, cwd, "", env, "--json", "task")
	if err == nil {
		t.Fatal("missing API key accepted")
	}
	events := parseEvents(t, out)
	terminal := 0
	for _, e := range events {
		if e["type"] == "run.failed" {
			terminal++
		}
	}
	if terminal != 1 {
		t.Fatal("duplicated terminal failure", events)
	}
}
func TestLegacySessionRestoreUncertainActionsAndTornRecord(t *testing.T) {
	r := testRuntime(t, true)
	id := r.Session.ID
	header := jsonText(event{"type": "session.started", "timestamp": "2026-10-11T00:00:00Z", "sessionId": id, "cwd": r.Session.Cwd, "version": 1})
	assistant := Message{Role: "assistant", Calls: []Call{toolCall("interrupted", "write_file", event{"path": "must-not-replay", "content": "bad"})}}
	history := header + "\n" + jsonText(event{"type": "message.added", "message": message("user", "original task")}) + "\n" + jsonText(event{"type": "message.added", "message": assistant}) + "\n" + `{"type":"tool.completed"`
	r.Session.close()
	log := filepath.Join(r.Session.Directory, "events.jsonl")
	os.WriteFile(log, []byte(history), 0600)
	outID, err := r.Session.Artifacts.save("stored output")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := application{out: &out, err: io.Discard}
	if err = app.output(r.Session.Cwd, arguments{Values: map[string]string{"session": id}, Pos: []string{"output", outID}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if string(b) != history {
		t.Fatal("output inspection modified torn log")
	}
	restored, err := openSession(r.Session.Cwd, id, true, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	if len(restored.History) != 3 || restored.History[2].Role != "tool" || !strings.Contains(text(restored.History[2]), "may have executed") {
		t.Fatal(restored.History)
	}
	if _, err = os.Stat(filepath.Join(r.Session.Cwd, "must-not-replay")); !os.IsNotExist(err) {
		t.Fatal("uncertain action replayed")
	}
	b, _ = os.ReadFile(log)
	if strings.Contains(string(b), `{"type":"tool.completed"`) && !strings.Contains(string(b), "session.recovered") {
		t.Fatal("torn record not repaired")
	}
	if _, err = openSession(t.TempDir(), id, true, 256); err == nil {
		t.Fatal("cross-workspace restore accepted")
	}
	os.WriteFile(log, []byte(header+"\ninvalid\n"+jsonText(event{"type": "message.added", "message": message("user", "later")})+"\n"), 0600)
	if _, err = openSession(r.Session.Cwd, id, true, 256); err == nil {
		t.Fatal("mid-log corruption silently discarded")
	}
}
func TestTrustAndConfigCompatibility(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("HOME", fixtureHome)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", fixtureHome)
	}
	t.Setenv("AMI_STATE_DIR", t.TempDir())
	cwd := t.TempDir()
	env := []string{"AMI_STATE_DIR=" + stateDir(), "HOME=" + os.Getenv("HOME")}
	out, stderr, err := cliRun(t, cwd, "", env, "trust", "--yes")
	if err != nil {
		t.Fatal(out, stderr, err)
	}
	root, _ := canonical(cwd)
	if !isTrusted(root) || isTrusted(filepath.Dir(root)) {
		t.Fatal("trust scope expanded")
	}
	if _, _, err = cliRun(t, cwd, "", env, "trust", "--revoke"); err != nil {
		t.Fatal(err)
	}
	if isTrusted(root) {
		t.Fatal("revocation failed")
	}
	var stdout bytes.Buffer
	app := application{out: &stdout, err: io.Discard}
	if err = app.config([]string{"set", "model", "file-model"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMI_MODEL", "env-model")
	if loadConfig().Model != "env-model" {
		t.Fatal("env precedence lost")
	}
	if err = app.config([]string{"set", "apiKey", "private-key-secret"}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	app.config([]string{"get", "apiKey"})
	if strings.Contains(stdout.String(), "private-key-secret") {
		t.Fatal("config leaked API key")
	}
}
