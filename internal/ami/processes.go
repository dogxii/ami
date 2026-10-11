package ami

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

var credentialName = regexp.MustCompile(`(?i)KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|^AMI_|^OPENAI_|^TAVILY_`)

func commandEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !credentialName.MatchString(name) {
			env = append(env, entry)
		}
	}
	return env
}

type job struct {
	ready                                              chan struct{}
	mu, pollMu                                         sync.Mutex
	id, label, cwd, stdoutID, stderrID, status, detail string
	pid                                                int
	exit                                               *int
	signal                                             string
	cmd                                                *exec.Cmd
	stdin                                              io.WriteCloser
	done                                               chan struct{}
	stopOnce                                           sync.Once
	stdoutOffset, stderrOffset                         int64
}

func (j *job) snapshot() event {
	j.mu.Lock()
	defer j.mu.Unlock()
	var exit any
	if j.exit != nil {
		exit = *j.exit
	}
	var signal any
	if j.signal != "" {
		signal = j.signal
	}
	e := event{"processId": j.id, "command": j.label, "cwd": j.cwd, "pid": j.pid, "status": j.status, "exitCode": exit, "signal": signal, "stdoutId": j.stdoutID, "stderrId": j.stderrID}
	if j.detail != "" {
		e["error"] = j.detail
	}
	return e
}

type Processes struct {
	ctx       context.Context
	artifacts *Artifacts
	emit      emitter
	mu        sync.Mutex
	jobs      map[string]*job
}

func newProcesses(ctx context.Context, a *Artifacts, emit emitter) *Processes {
	return &Processes{ctx: ctx, artifacts: a, emit: emit, jobs: map[string]*job{}}
}

type processWriter struct {
	file       *os.File
	manager    *Processes
	job        *job
	stream, id string
	bytes      int64
}

func (w *processWriter) Write(b []byte) (int, error) {
	<-w.job.ready
	n, err := w.file.Write(b)
	w.bytes += int64(n)
	if err == nil {
		err = w.manager.emit("process.output", event{"processId": w.job.id, "stream": w.stream, "bytes": w.bytes, "outputId": w.id})
	}
	if err != nil {
		w.job.mu.Lock()
		w.job.status = "failed"
		w.job.detail = err.Error()
		w.job.mu.Unlock()
		go w.manager.terminate(w.job)
	}
	return n, err
}
func (p *Processes) start(command, cwd string, timeout time.Duration) (string, error) {
	program, args := shellCommand(command)
	return p.startProgram(program, args, cwd, timeout, command)
}
func (p *Processes) startProgram(program string, args []string, cwd string, timeout time.Duration, label string) (string, error) {
	if err := p.ctx.Err(); err != nil {
		return "", err
	}
	outID, out, err := p.artifacts.create()
	if err != nil {
		return "", err
	}
	errID, stderr, err := p.artifacts.create()
	if err != nil {
		out.Close()
		return "", err
	}
	if label == "" {
		label = strings.Join(append([]string{program}, args...), " ")
	}
	cmd := exec.Command(program, args...)
	cmd.Dir = cwd
	cmd.Env = commandEnvironment()
	configureProcess(cmd)
	if runtime.GOOS == "windows" {
		cmd.WaitDelay = time.Second
	}
	j := &job{id: uuid(), label: label, cwd: cwd, stdoutID: outID, stderrID: errID, status: "running", cmd: cmd, done: make(chan struct{}), ready: make(chan struct{})}
	cmd.Stdout = &processWriter{file: out, manager: p, job: j, stream: "stdout", id: outID}
	cmd.Stderr = &processWriter{file: stderr, manager: p, job: j, stream: "stderr", id: errID}
	j.stdin, err = cmd.StdinPipe()
	if err != nil {
		out.Close()
		stderr.Close()
		return "", err
	}
	if err = cmd.Start(); err != nil {
		j.stdin.Close()
		out.Close()
		stderr.Close()
		return "", err
	}
	j.pid = cmd.Process.Pid
	p.mu.Lock()
	p.jobs[j.id] = j
	p.mu.Unlock()
	go func() {
		<-j.ready
		err := cmd.Wait()
		oe := out.Close()
		se := stderr.Close()
		j.mu.Lock()
		code := cmd.ProcessState.ExitCode()
		j.exit = &code
		j.signal = processSignal(cmd.ProcessState)
		if j.status == "running" {
			j.status = "exited"
		}
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				j.status = "failed"
				j.detail = err.Error()
			}
		}
		if oe != nil || se != nil {
			j.status = "failed"
			j.detail = fmt.Sprintf("output write failed: %v %v", oe, se)
		}
		j.mu.Unlock()
		if emitErr := p.emit("process.completed", j.snapshot()); emitErr != nil {
			j.mu.Lock()
			j.status = "failed"
			j.detail = emitErr.Error()
			j.mu.Unlock()
		}
		close(j.done)
	}()
	go func() {
		var deadline <-chan time.Time
		if timeout > 0 {
			timer := time.NewTimer(timeout)
			defer timer.Stop()
			deadline = timer.C
		}
		select {
		case <-j.done:
		case <-p.ctx.Done():
			p.stopJob(j, "cancelled")
		case <-deadline:
			p.stopJob(j, "timed_out")
		}
	}()
	err = p.emit("process.started", j.snapshot())
	close(j.ready)
	if err != nil {
		p.stopJob(j, "failed")
		return "", err
	}
	return j.id, nil
}
func (p *Processes) get(id string) (*job, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	j := p.jobs[id]
	if j == nil {
		return nil, fmt.Errorf("process is not from this invocation; inspect saved output before restarting it")
	}
	return j, nil
}
func (p *Processes) closeInput(id string) error {
	j, err := p.get(id)
	if err != nil {
		return err
	}
	return j.stdin.Close()
}
func (p *Processes) poll(id string, d time.Duration) (event, error) {
	j, err := p.get(id)
	if err != nil {
		return nil, err
	}
	if d > 0 {
		t := time.NewTimer(d)
		select {
		case <-j.done:
		case <-t.C:
		case <-p.ctx.Done():
			t.Stop()
			return nil, p.ctx.Err()
		}
		t.Stop()
	}
	if err = p.ctx.Err(); err != nil {
		return nil, err
	}
	j.pollMu.Lock()
	defer j.pollMu.Unlock()
	result := j.snapshot()
	out, err := p.artifacts.page(j.stdoutID, j.stdoutOffset, 0)
	if err != nil {
		return nil, err
	}
	stderr, err := p.artifacts.page(j.stderrID, j.stderrOffset, 0)
	if err != nil {
		return nil, err
	}
	j.stdoutOffset = pageEnd(out)
	j.stderrOffset = pageEnd(stderr)
	result["stdout"] = out
	result["stderr"] = stderr
	return result, nil
}
func pageEnd(page event) int64 {
	if next, ok := page["nextOffset"].(int64); ok {
		return next
	}
	return page["sizeBytes"].(int64)
}
func (p *Processes) write(id, value string, closeInput bool) (event, error) {
	j, err := p.get(id)
	if err != nil {
		return nil, err
	}
	if err = p.ctx.Err(); err != nil {
		return nil, err
	}
	n, err := io.WriteString(j.stdin, value)
	if err != nil {
		return nil, err
	}
	if closeInput {
		err = j.stdin.Close()
	}
	return event{"processId": id, "bytesWritten": n, "stdinClosed": closeInput}, err
}
func (p *Processes) stopJob(j *job, status string) {
	j.mu.Lock()
	if j.status == "running" {
		j.status = status
	}
	j.mu.Unlock()
	p.terminate(j)
}
func (p *Processes) terminate(j *job) {
	j.stopOnce.Do(func() {
		if runtime.GOOS == "windows" {
			select {
			case <-j.done:
				return
			default:
			}
		}
		if err := signalGroup(j.pid, false); err != nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
		_ = signalGroup(j.pid, true)
	})
}
func (p *Processes) stop(id string) (event, error) {
	j, err := p.get(id)
	if err != nil {
		return nil, err
	}
	p.stopJob(j, "cancelled")
	<-j.done
	return j.snapshot(), nil
}
func (p *Processes) runProgram(program string, args []string, cwd string) (event, error) {
	id, err := p.startProgram(program, args, cwd, 120*time.Second, "")
	if err != nil {
		return nil, err
	}
	_ = p.closeInput(id)
	j, _ := p.get(id)
	select {
	case <-j.done:
	case <-p.ctx.Done():
		p.stopJob(j, "cancelled")
		<-j.done
		return nil, p.ctx.Err()
	}
	result, err := p.poll(id, 0)
	if err != nil {
		return nil, err
	}
	if result["status"] != "exited" {
		return nil, fmt.Errorf("command did not complete: %v (%v)", result["status"], result["error"])
	}
	return result, nil
}
func (p *Processes) dispose() {
	p.mu.Lock()
	jobs := make([]*job, 0, len(p.jobs))
	for _, j := range p.jobs {
		jobs = append(jobs, j)
	}
	p.mu.Unlock()
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j *job) { defer wg.Done(); p.stopJob(j, "cancelled"); <-j.done }(j)
	}
	wg.Wait()
}
