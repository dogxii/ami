package ami

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	if os.Getenv("AMI_PROCESS_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		os.Exit(3)
	}
	switch args[1] {
	case "large":
		os.Stdout.WriteString(strings.Repeat("x", 12*1024*1024))
		fmt.Fprint(os.Stderr, "failure details")
		os.Exit(7)
	case "echo":
		buffer := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buffer)
			if n > 0 {
				os.Stdout.Write(buffer[:n])
			}
			if err != nil {
				os.Exit(0)
			}
		}
	case "sleep":
		for {
			time.Sleep(time.Second)
		}
	case "binary":
		os.Stdout.Write([]byte{0, 255, 1, 128})
		os.Exit(9)
	}
	os.Exit(0)
}
func helperProgram(t *testing.T, mode string) (string, []string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe, []string{"-test.run=TestProcessHelper", "--", mode}
}

// Credential filtering intentionally excludes AMI_*; use a dedicated marker in helper argv environment.
func startHelper(t *testing.T, r *Runtime, mode string, timeout time.Duration) string {
	t.Helper()
	exe, args := helperProgram(t, mode)
	wrapper := filepath.Join(t.TempDir(), "helper")
	if runtime.GOOS == "windows" {
		wrapper += ".cmd"
		os.WriteFile(wrapper, []byte("@set AMI_PROCESS_HELPER=1\r\n@\""+exe+"\" "+strings.Join(args, " ")+"\r\n"), 0755)
		program, argv := shellCommand("& '" + wrapper + "'")
		id, err := r.Processes.startProgram(program, argv, r.Session.Cwd, timeout, "")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	os.WriteFile(wrapper, []byte("#!/bin/sh\nAMI_PROCESS_HELPER=1 exec '"+exe+"' "+strings.Join(args, " ")+"\n"), 0755)
	id, err := r.Processes.startProgram(wrapper, nil, r.Session.Cwd, timeout, "")
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestProcessLargeOutputIncrementalPollAndExit(t *testing.T) {
	r := testRuntime(t, false)
	id := startHelper(t, r, "large", 10*time.Second)
	r.Processes.closeInput(id)
	j, _ := r.Processes.get(id)
	<-j.done
	page, err := r.Processes.poll(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page["exitCode"] != 7 {
		t.Fatal(page)
	}
	out := page["stdout"].(event)
	if out["nextOffset"] != int64(256) {
		t.Fatal(out)
	}
	next, err := r.Processes.poll(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next["stdout"].(event)["offset"] != int64(256) {
		t.Fatal("poll repeated output")
	}
	path, _ := r.Session.Artifacts.path(page["stdoutId"].(string))
	info, _ := os.Stat(path)
	if info.Size() != 12*1024*1024 {
		t.Fatal("output truncated")
	}
	if page["stderr"].(event)["content"] != "failure details" {
		t.Fatal(page)
	}
}
func TestProcessStdinStopTimeoutAndCancellation(t *testing.T) {
	r := testRuntime(t, false)
	id := startHelper(t, r, "echo", 5*time.Second)
	if _, err := r.Processes.write(id, "hello\n", true); err != nil {
		t.Fatal(err)
	}
	result, err := r.Processes.poll(id, 2*time.Second)
	if err != nil || result["exitCode"] != 0 || result["stdout"].(event)["content"] != "hello\n" {
		t.Fatal(result, err)
	}
	long := startHelper(t, r, "sleep", 0)
	if _, err = r.Processes.stop(long); err != nil {
		t.Fatal(err)
	}
	timed := startHelper(t, r, "sleep", 80*time.Millisecond)
	result, err = r.Processes.poll(timed, time.Second)
	if err != nil || result["status"] != "timed_out" {
		t.Fatal(result, err)
	}
	other := startHelper(t, r, "sleep", 0)
	r.cancel()
	r.cleanup()
	j, _ := r.Processes.get(other)
	select {
	case <-j.done:
	default:
		t.Fatal("cancelled child still running")
	}
}
func TestMissingProgramSettlesWithoutHanging(t *testing.T) {
	r := testRuntime(t, false)
	start := time.Now()
	if _, err := r.Processes.runProgram("ami-definitely-missing-executable", nil, r.Session.Cwd); err == nil {
		t.Fatal("missing program succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("spawn failure hung")
	}
}
func TestCommandCredentialsFiltered(t *testing.T) {
	t.Setenv("AMI_TEST_SECRET", "secret")
	t.Setenv("SERVICE_API_KEY", "key")
	env := strings.Join(commandEnvironment(), "\n")
	if strings.Contains(env, "AMI_TEST_SECRET") || strings.Contains(env, "SERVICE_API_KEY") {
		t.Fatal("credential inherited")
	}
	if !strings.Contains(env, "PATH=") {
		t.Fatal("PATH removed")
	}
}
func TestProcessBinaryAndFailingEventSink(t *testing.T) {
	r := testRuntime(t, false)
	id := startHelper(t, r, "binary", 5*time.Second)
	r.Processes.closeInput(id)
	result, err := r.Processes.poll(id, 3*time.Second)
	if err != nil || result["exitCode"] != 9 || result["stdout"].(event)["encoding"] != "base64" {
		t.Fatal(result, err)
	}
	p := newProcesses(context.Background(), r.Session.Artifacts, func(kind string, e event) error {
		if kind == "process.output" || kind == "process.completed" {
			return fmt.Errorf("sink unavailable")
		}
		return nil
	})
	defer p.dispose()
	exe, _ := os.Executable()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture below is Unix")
	}
	id, err = p.start("AMI_PROCESS_HELPER=1 '"+exe+"' -test.run=TestProcessHelper -- binary", r.Session.Cwd, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	j, _ := p.get(id)
	<-j.done
	result, err = p.poll(id, 0)
	if err != nil || result["status"] != "failed" {
		t.Fatal(jsonText(result), err)
	}
}

func TestFixedProgramsRejectFailedCompletionEvents(t *testing.T) {
	r := testRuntime(t, false)
	p := newProcesses(context.Background(), r.Session.Artifacts, func(kind string, _ event) error {
		if kind == "process.completed" {
			return fmt.Errorf("completion record failed")
		}
		return nil
	})
	defer p.dispose()
	command := "printf done"
	if runtime.GOOS == "windows" {
		command = "Write-Output done"
	}
	program, args := shellCommand(command)
	if _, err := p.runProgram(program, args, r.Session.Cwd); err == nil {
		t.Fatal("fixed program reported success after completion recording failed")
	}
}

func TestQuietCommandEventsStartBeforeCompletion(t *testing.T) {
	r := testRuntime(t, false)
	var mu sync.Mutex
	var events []string
	p := newProcesses(context.Background(), r.Session.Artifacts, func(kind string, _ event) error {
		if kind == "process.started" {
			// Keep the start event pending after a quiet process has already exited.
			time.Sleep(100 * time.Millisecond)
		}
		mu.Lock()
		events = append(events, kind)
		mu.Unlock()
		return nil
	})
	defer p.dispose()
	command := "exit 0"
	program, args := shellCommand(command)
	if _, err := p.runProgram(program, args, r.Session.Cwd); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "process.started" || events[1] != "process.completed" {
		t.Fatal("quiet command events out of order", events)
	}
}
