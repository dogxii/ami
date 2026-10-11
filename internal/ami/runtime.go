package ami

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Runtime struct {
	Ctx             context.Context
	Session         *Session
	Mode            string
	Processes       *Processes
	Versions        map[string]string
	Changes         map[string]string
	Emit            emitter
	Confirm         func(context.Context, string) (bool, error)
	approved        map[string]bool
	cancel          context.CancelFunc
	closeOnce       sync.Once
	instructionRoot string
	instructions    map[string]string
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
	r := &Runtime{Ctx: ctx, Session: s, Mode: mode, Versions: map[string]string{}, Changes: map[string]string{}, Confirm: confirm, approved: map[string]bool{}, cancel: cancel}
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

func previewText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return text + "\n… (preview truncated)"
}

func (r *Runtime) previewTool(t *Tool, input map[string]any) error {
	if r.Mode == "read-only" || (t.Capability != "write" && t.Capability != "exec") {
		return nil
	}
	if t.Name == "process_control" && stringArg(input, "action", "") == "poll" {
		return nil
	}
	e := event{"name": t.Name, "cwd": r.Session.Cwd}
	limit := r.Session.Artifacts.PageBytes / 2
	switch t.Name {
	case "exec_command":
		e["command"] = previewText(stringArg(input, "command", ""), limit)
		if cwd := stringArg(input, "cwd", ""); cwd != "" {
			if !filepath.IsAbs(cwd) {
				cwd = filepath.Join(r.Session.Cwd, cwd)
			}
			e["cwd"] = filepath.Clean(cwd)
		}
	case "edit_file":
		e["path"] = stringArg(input, "path", "")
		e["before"] = previewText(stringArg(input, "oldText", ""), limit)
		e["after"] = previewText(stringArg(input, "newText", ""), limit)
		e["replaceAll"] = boolArg(input, "replaceAll")
	case "write_file":
		e["path"] = stringArg(input, "path", "")
		e["after"] = previewText(stringArg(input, "content", ""), limit)
		e["overwrite"] = boolArg(input, "overwrite")
	case "process_control":
		e["processId"] = stringArg(input, "processId", "")
		e["action"] = stringArg(input, "action", "")
		e["text"] = previewText(stringArg(input, "text", ""), limit)
	}
	return r.Emit("tool.preview", e)
}
func (r *Runtime) cleanup() {
	r.closeOnce.Do(func() { r.cancel(); r.Processes.dispose() })
}
