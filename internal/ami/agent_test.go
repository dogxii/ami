package ami

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func toolCall(id, name string, input any) Call {
	return Call{ID: id, Type: "function", Function: Function{name, jsonText(input)}}
}
func sseReply(w http.ResponseWriter, m Message, tokens int) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta := event{}
	if m.Content != nil {
		delta["content"] = text(m)
	}
	calls := []any{}
	for i, c := range m.Calls {
		calls = append(calls, event{"index": i, "id": c.ID, "type": "function", "function": c.Function})
	}
	if len(calls) > 0 {
		delta["tool_calls"] = calls
	}
	fmt.Fprintf(w, "data: %s\n\n", jsonText(event{"choices": []any{event{"delta": delta}}}))
	reason := "stop"
	if len(calls) > 0 {
		reason = "tool_calls"
	}
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", jsonText(event{"choices": []any{event{"delta": event{}, "finish_reason": reason}}, "usage": event{"total_tokens": tokens}}))
}
func completeReply(w http.ResponseWriter, s string, tokens int) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, jsonText(event{"choices": []any{event{"message": message("assistant", s), "finish_reason": "stop"}}, "usage": event{"total_tokens": tokens}}))
}
func model(t *testing.T, handler func(event, http.ResponseWriter)) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body event
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad JSON", 400)
			return
		}
		handler(body, w)
	}))
	t.Cleanup(server.Close)
	return &Provider{Config: Config{BaseURL: server.URL, Model: "fixture", APIKey: "test-only-api-key"}, Timeout: 2 * time.Second}
}
func TestAgentReadRunEditVerifyBeyondFiveTools(t *testing.T) {
	r := testRuntime(t, true)
	os.WriteFile(filepath.Join(r.Session.Cwd, "source.txt"), []byte("bad"), 0644)
	reproduce, verify := "grep -q fixed source.txt", "grep -q fixed source.txt"
	if runtime.GOOS == "windows" {
		reproduce = "if (Select-String -Quiet -Pattern fixed source.txt) { exit 0 } else { exit 1 }"
		verify = reproduce
	}
	calls := []Call{toolCall("read", "read_file", event{"path": "source.txt"}), toolCall("reproduce", "exec_command", event{"command": reproduce, "waitMs": 30000}), toolCall("edit", "edit_file", event{"path": "source.txt", "oldText": "bad", "newText": "fixed"}), toolCall("verify", "exec_command", event{"command": verify, "waitMs": 30000}), toolCall("reread", "read_file", event{"path": "source.txt"}), toolCall("list", "list_files", event{})}
	step := 0
	p := model(t, func(body event, w http.ResponseWriter) {
		if step < len(calls) {
			sseReply(w, Message{Role: "assistant", Calls: []Call{calls[step]}}, 100)
		} else {
			sseReply(w, message("assistant", "fixed and verified"), 100)
		}
		step++
	})
	outcome := runAgent(r, AgentOptions{Task: "fix source", Provider: p})
	if outcome.Err != nil || outcome.ToolCalls != 6 || outcome.Status != "completed" {
		t.Fatal(outcome)
	}
	b, _ := os.ReadFile(filepath.Join(r.Session.Cwd, "source.txt"))
	if string(b) != "fixed" {
		t.Fatal(string(b))
	}
	for _, m := range r.Session.History {
		if m.CallID == "reproduce" && !strings.Contains(text(m), `"exitCode":1`) {
			t.Fatal("non-zero exit metadata hidden", text(m))
		}
	}
	if _, err := os.Stat(latestPath(r.Session.Cwd)); err != nil {
		t.Fatal(err)
	}
}
func TestProviderInterruptedMalformedAndDuplicateCallsNeverExecute(t *testing.T) {
	for _, kind := range []string{"interrupted", "invalid-arguments", "duplicate", "length"} {
		t.Run(kind, func(t *testing.T) {
			r := testRuntime(t, false)
			p := model(t, func(body event, w http.ResponseWriter) {
				call := toolCall("one", "write_file", event{"path": "forbidden", "content": "bad"})
				switch kind {
				case "interrupted":
					fmt.Fprintf(w, "data: %s\n\n", jsonText(event{"choices": []any{event{"delta": event{"tool_calls": []any{event{"index": 0, "id": call.ID, "function": call.Function}}}}}}))
				case "invalid-arguments":
					other := toolCall("two", "write_file", event{})
					other.Function.Arguments = "{"
					sseReply(w, Message{Role: "assistant", Calls: []Call{call, other}}, 100)
				case "duplicate":
					sseReply(w, Message{Role: "assistant", Calls: []Call{call, call}}, 100)
				case "length":
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", jsonText(event{"choices": []any{event{"delta": event{"tool_calls": []any{event{"index": 0, "id": call.ID, "function": call.Function}}}, "finish_reason": "length"}}}))
				}
			})
			o := runAgent(r, AgentOptions{Task: "test", Provider: p})
			if o.Status != "failed" || o.ToolCalls != 0 {
				t.Fatal(o)
			}
			if _, err := os.Stat(filepath.Join(r.Session.Cwd, "forbidden")); !os.IsNotExist(err) {
				t.Fatal("partial batch executed")
			}
		})
	}
}
func TestReadOnlyEnforcedDespiteProviderRequests(t *testing.T) {
	r := testRuntime(t, false)
	r.Mode = "read-only"
	step := 0
	p := model(t, func(body event, w http.ResponseWriter) {
		if step == 0 {
			if strings.Contains(jsonText(body["tools"]), `"name":"exec_command"`) {
				t.Error("exec tool advertised")
			}
			sseReply(w, Message{Role: "assistant", Calls: []Call{toolCall("bad", "exec_command", event{"command": "touch forbidden"})}}, 100)
		} else {
			sseReply(w, message("assistant", "blocked"), 100)
		}
		step++
	})
	o := runAgent(r, AgentOptions{Task: "analyze", Provider: p})
	if o.Err != nil {
		t.Fatal(o)
	}
	if len(r.Processes.jobs) != 0 {
		t.Fatal("unadvertised command executed")
	}
}
func TestCompatibilityFallbackReadonlyAndResults(t *testing.T) {
	r := testRuntime(t, false)
	os.WriteFile(filepath.Join(r.Session.Cwd, "file.txt"), []byte("fixture"), 0644)
	step := 0
	p := model(t, func(body event, w http.ResponseWriter) {
		switch step {
		case 0:
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"message":"tools not supported"}}`)
		case 1:
			if strings.Contains(jsonText(body["messages"]), `\"name\":\"exec_command\"`) {
				t.Error("compat exec tool advertised")
			}
			completeReply(w, `{"tool":"read_file","input":{"path":"file.txt"}}`, 100)
		default:
			if !strings.Contains(jsonText(body["messages"]), "Tool result") {
				t.Error("fallback lost tool results")
			}
			completeReply(w, "done", 100)
		}
		step++
	})
	o := runAgent(r, AgentOptions{Task: "read", Provider: p})
	if o.Err != nil || o.ToolCalls != 1 || o.Content != "done" {
		t.Fatal(o)
	}
}
func TestStepTokenAndRepeatedFailureBudgets(t *testing.T) {
	t.Run("steps", func(t *testing.T) {
		r := testRuntime(t, false)
		step := 0
		p := model(t, func(body event, w http.ResponseWriter) {
			if step == 0 {
				sseReply(w, Message{Role: "assistant", Calls: []Call{toolCall("a", "list_files", event{}), toolCall("b", "list_files", event{})}}, 100)
			} else {
				if body["tools"] != nil {
					t.Error("tools remain after step budget")
				}
				sseReply(w, message("assistant", "done"), 100)
			}
			step++
		})
		o := runAgent(r, AgentOptions{Task: "inspect", Provider: p, MaxSteps: 1})
		if o.Err != nil || o.ToolCalls != 1 {
			t.Fatal(o)
		}
		if len(r.Session.History) != 5 {
			t.Fatal("missing budget-skipped tool result")
		}
	})
	t.Run("tokens", func(t *testing.T) {
		r := testRuntime(t, false)
		requests := 0
		p := model(t, func(body event, w http.ResponseWriter) {
			requests++
			sseReply(w, Message{Role: "assistant", Calls: []Call{toolCall("a", "list_files", event{})}}, 100)
		})
		o := runAgent(r, AgentOptions{Task: "inspect", Provider: p, MaxTokens: 100})
		if o.Status != "failed" || requests != 1 || !strings.Contains(o.Err.Error(), "token budget") {
			t.Fatal(o, requests)
		}
	})
	t.Run("repeated batch failure", func(t *testing.T) {
		r := testRuntime(t, false)
		step := 0
		p := model(t, func(body event, w http.ResponseWriter) {
			step++
			calls := []Call{toolCall(fmt.Sprint(step), "read_file", event{"path": "missing"})}
			if step == 3 {
				calls = append(calls, toolCall("later", "write_file", event{"path": "forbidden", "content": "bad"}))
			}
			sseReply(w, Message{Role: "assistant", Calls: calls}, 100)
		})
		o := runAgent(r, AgentOptions{Task: "inspect", Provider: p})
		if o.Status != "failed" || o.ToolCalls != 3 {
			t.Fatal(o)
		}
		if _, err := os.Stat(filepath.Join(r.Session.Cwd, "forbidden")); !os.IsNotExist(err) {
			t.Fatal("later action executed")
		}
		last := r.Session.History[len(r.Session.History)-1]
		if last.CallID != "later" || !strings.Contains(text(last), "not executed") {
			t.Fatal(last)
		}
	})
}
func TestCompactionPreservesPairsAndBudgetBetweenBatches(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprint(limited), func(t *testing.T) {
			r := testRuntime(t, true)
			r.Session.add(message("user", "original goal and constraint"))
			for i := 0; i < 30; i++ {
				c := toolCall(fmt.Sprint(i), "read_file", event{"path": "file"})
				m := message("assistant", strings.Repeat("observation", 300))
				m.Calls = []Call{c}
				r.Session.add(m)
				result := message("tool", strings.Repeat("evidence", 400))
				result.CallID = c.ID
				r.Session.add(result)
			}
			before := len(r.Session.History)
			requests := 0
			p := model(t, func(body event, w http.ResponseWriter) {
				requests++
				completeReply(w, "preserved goal, constraints, outputs and next steps", 200)
			})
			tokens := 0
			usage := func(n *int, _ any) { tokens += *n }
			check := func() error {
				if limited && tokens >= 100 {
					return fmt.Errorf("token budget")
				}
				return nil
			}
			err := compactContext(r, p, "system", nil, 14000, usage, check)
			if limited {
				if err == nil || requests != 1 || len(r.Session.History) != before {
					t.Fatal(err, requests, len(r.Session.History))
				}
				return
			}
			if err != nil || len(r.Session.History) >= before {
				t.Fatal(err)
			}
			if text(r.Session.History[0]) != "original goal and constraint" {
				t.Fatal("original goal lost")
			}
			for i, m := range r.Session.History {
				if m.Role == "tool" && (i == 0 || r.Session.History[i-1].Role != "assistant") {
					t.Fatal("tool pair split")
				}
			}
		})
	}
}
func TestUsageOptionFallbackAndCancellation(t *testing.T) {
	t.Run("usage", func(t *testing.T) {
		step := 0
		p := model(t, func(body event, w http.ResponseWriter) {
			if step == 0 {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"unknown stream_options include_usage"}`)
			} else {
				if body["stream_options"] != nil {
					t.Error("usage option not removed")
				}
				sseReply(w, message("assistant", "ok"), 42)
			}
			step++
		})
		reply, err := p.chat(context.Background(), []Message{message("user", "hi")}, nil, true, nil)
		if err != nil || text(reply.Message) != "ok" || *reply.Tokens != 42 {
			t.Fatal(reply, err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(500 * time.Millisecond):
			}
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		p := Provider{Config: Config{BaseURL: server.URL, APIKey: "key"}, Timeout: time.Second}
		_, err := p.chat(ctx, []Message{message("user", "hi")}, nil, true, nil)
		if err == nil {
			t.Fatal("request cancellation ignored")
		}
	})
}
func TestChatEndpointNormalization(t *testing.T) {
	for _, base := range []string{"https://example.com", "https://example.com/v1", "https://example.com/v1/", "https://example.com/v1/chat/completions", "https://example.com/v1/chat/completions/"} {
		got, err := chatURL(base)
		if err != nil || got != "https://example.com/v1/chat/completions" {
			t.Fatal(base, got, err)
		}
	}
	got, err := chatURL("https://example.com/compatible-mode/")
	if err != nil || got != "https://example.com/compatible-mode/v1/chat/completions" {
		t.Fatal(got, err)
	}
}
func TestRedactionAndConcurrentEventWrites(t *testing.T) {
	r := testRuntime(t, true)
	secret := "api-secret-long"
	r.Session.close()
	var mu sync.Mutex
	var events []event
	s, err := newRuntime(context.Background(), r.Session.Cwd, "latest", true, 256, "auto", func(e event) error { mu.Lock(); defer mu.Unlock(); events = append(events, e); return nil }, nil, []string{secret})
	if err == nil {
		s.cleanup()
		s.Session.close()
		t.Fatal("manual runtime unexpectedly activated latest")
	}
	s, err = newRuntime(context.Background(), r.Session.Cwd, r.Session.ID, true, 256, "auto", func(e event) error { mu.Lock(); defer mu.Unlock(); events = append(events, e); return nil }, nil, []string{secret})
	if err != nil {
		t.Fatal(err)
	}
	defer s.cleanup()
	defer s.Session.close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Emit("test", event{"input": map[string]any{"key": secret}}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, _ := os.ReadFile(filepath.Join(s.Session.Directory, "events.jsonl"))
	if strings.Contains(string(b), secret) || len(events) != 20 {
		t.Fatal("redaction/event loss")
	}
}
