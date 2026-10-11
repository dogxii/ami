package ami

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Runtime struct {
	Ctx       context.Context
	Session   *Session
	Mode      string
	Processes *Processes
	Versions  map[string]string
	Emit      emitter
	Confirm   func(context.Context, string) (bool, error)
	approved  map[string]bool
	cancel    context.CancelFunc
	closeOnce sync.Once
}

func newRuntime(ctx context.Context, cwd, resume string, persist bool, page int, mode string, render func(event) error, confirm func(context.Context, string) (bool, error), secrets []string) (*Runtime, error) {
	s, err := openSession(cwd, resume, persist, page)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	if mode == "" {
		mode = "ask"
		if isTrusted(s.Cwd) {
			mode = "auto"
		}
	}
	r := &Runtime{Ctx: ctx, Session: s, Mode: mode, Versions: map[string]string{}, Confirm: confirm, approved: map[string]bool{}, cancel: cancel}
	r.Emit = func(kind string, data event) error {
		e := event{"type": kind, "timestamp": time.Now().UTC().Format(time.RFC3339Nano)}
		for k, v := range data {
			e[k] = redact(v, secrets)
		}
		if err := s.record(e); err != nil {
			return err
		}
		if render != nil {
			return render(e)
		}
		return nil
	}
	r.Processes = newProcesses(ctx, s.Artifacts, r.Emit)
	return r, nil
}
func redact(v any, secrets []string) any {
	switch x := v.(type) {
	case string:
		for _, secret := range secrets {
			if len(secret) >= 8 {
				x = strings.ReplaceAll(x, secret, "[REDACTED]")
			}
		}
		return x
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			out[redact(k, secrets).(string)] = redact(v, secrets)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = redact(v, secrets)
		}
		return out
	default:
		return v
	}
}
func (r *Runtime) authorize(cap string) error {
	if err := r.Ctx.Err(); err != nil {
		return err
	}
	if cap != "write" && cap != "exec" {
		return nil
	}
	if r.Mode == "read-only" {
		return fmt.Errorf("read-only mode blocks file writes and shell commands")
	}
	if r.Mode == "auto" || r.approved[cap] {
		return nil
	}
	question := fmt.Sprintf("Allow %s operations for this run in %s? Shell commands use your account permissions.", cap, r.Session.Cwd)
	if r.Confirm == nil {
		reason := "execution needs authorization; use --yes for trusted local work, or --read-only"
		_ = r.Emit("permission.denied", event{"capability": cap, "reason": reason})
		return fmt.Errorf("%s", reason)
	}
	if err := r.Emit("permission.requested", event{"capability": cap, "message": question}); err != nil {
		return err
	}
	yes, err := r.Confirm(r.Ctx, question)
	if err != nil {
		return err
	}
	if !yes {
		return fmt.Errorf("permission denied: %s", cap)
	}
	r.approved[cap] = true
	return r.Emit("permission.approved", event{"capability": cap, "scope": "run"})
}
func (r *Runtime) cleanup() {
	r.closeOnce.Do(func() { r.cancel(); r.Processes.dispose() })
}
