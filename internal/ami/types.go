package ami

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

type Function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type Call struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type Message struct {
	Role    string  `json:"role"`
	Content *string `json:"content"`
	Calls   []Call  `json:"tool_calls,omitempty"`
	CallID  string  `json:"tool_call_id,omitempty"`
}

func message(role, text string) Message { return Message{Role: role, Content: &text} }
func text(m Message) string {
	if m.Content == nil {
		return ""
	}
	return *m.Content
}
func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }
func estimate(v any) int    { return (len(jsonText(v)) + 2) / 3 }
func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type event = map[string]any
type emitter func(string, event) error

type renderer struct {
	mu             sync.Mutex
	out, err       io.Writer
	json, textOpen bool
}

func (r *renderer) render(e event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.json {
		_, err := fmt.Fprintln(r.out, jsonText(e))
		return err
	}
	if e["type"] == "message.delta" {
		r.textOpen = true
		_, err := fmt.Fprint(r.out, e["delta"])
		return err
	}
	if r.textOpen {
		fmt.Fprintln(r.out)
		r.textOpen = false
	}
	switch e["type"] {
	case "run.started":
		if e["persistent"] == false {
			fmt.Fprintf(r.err, "Outputs: %v (history disabled)\n", e["outputDirectory"])
		}
	case "tool.started":
		fmt.Fprintf(r.err, "⚙ %v\n", e["name"])
	case "tool.completed":
		if e["error"] != nil {
			fmt.Fprintln(r.err, e["error"])
		}
	case "process.started":
		fmt.Fprintf(r.err, "$ %v\n", e["command"])
	case "process.completed":
		fmt.Fprintf(r.err, "Command %v: %v (%v)\n", e["processId"], e["status"], e["exitCode"])
	case "run.completed":
		fmt.Fprintf(r.err, "%v · %v tool calls · %v tokens · session %v\n", e["model"], e["toolCalls"], e["tokens"], e["sessionId"])
	case "run.failed", "run.cancelled":
		fmt.Fprintln(r.err, e["error"])
	case "context.compacted":
		fmt.Fprintln(r.err, "Context compacted.")
	case "provider.compatibility":
		fmt.Fprintln(r.err, e["message"])
	case "permission.denied":
		fmt.Fprintln(r.err, e["reason"])
	}
	return nil
}
