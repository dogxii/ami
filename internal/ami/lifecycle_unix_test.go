//go:build darwin || linux

package ami

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCleanupBackgroundDescendants(t *testing.T) {
	r := testRuntime(t, false)
	pidFile := filepath.Join(r.Session.Cwd, "pid")
	id, err := r.Processes.start("sleep 120 >/dev/null 2>&1 & echo $! > '"+pidFile+"'", r.Session.Cwd, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r.Processes.closeInput(id)
	result, err := r.Processes.poll(id, time.Second)
	if err != nil || result["exitCode"] != 0 {
		t.Fatal(result, err)
	}
	b, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if err = syscall.Kill(pid, 0); err != nil {
		t.Fatal("fixture descendant not alive", err)
	}
	r.cleanup()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err = syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background descendant survived cleanup", pid)
}
func TestCLISIGINTCleansCommandsAndReturns130(t *testing.T) {
	cwd := t.TempDir()
	state := t.TempDir()
	request := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body event
		json.NewDecoder(r.Body).Decode(&body)
		request++
		if request == 1 {
			sseReply(w, Message{Role: "assistant", Calls: []Call{toolCall("run", "exec_command", event{"command": "sleep 120", "waitMs": float64(0)})}}, 100)
		} else {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}
	}))
	defer server.Close()
	cmd := exec.Command(cliBinary, "--json", "--yes", "run task")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "AMI_BASE_URL="+server.URL, "AMI_API_KEY=test-only-key", "AMI_STATE_DIR="+state)
	stdout, _ := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	events := make(chan event, 100)
	go func() {
		defer close(events)
		for scanner.Scan() {
			var e event
			if json.Unmarshal(scanner.Bytes(), &e) == nil {
				events <- e
			}
		}
	}()
	pid := 0
	deadline := time.After(5 * time.Second)
	for pid == 0 {
		select {
		case e := <-events:
			if e["type"] == "process.started" {
				pid = int(e["pid"].(float64))
			}
		case <-deadline:
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatal("command never started", stderr.String())
		}
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 130 {
		t.Fatal("unexpected exit", err, stderr.String())
	}
	cancelled := 0
	for e := range events {
		if e["type"] == "run.cancelled" {
			cancelled++
		}
	}
	if cancelled != 1 {
		t.Fatal("duplicate/missing cancellation", cancelled)
	}
	if err = syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatal(fmt.Sprintf("child %d survived: %v", pid, err))
	}
}
