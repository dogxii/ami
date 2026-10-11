package ami

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type APIError struct {
	Status  int
	Details string
}

func (e *APIError) Error() string {
	switch e.Status {
	case 401, 403:
		return "authentication failed; check your API key"
	case 404:
		return "model or API endpoint not found; check model and baseUrl"
	case 429:
		return "API rate limit reached"
	default:
		return fmt.Sprintf("LLM request failed (%d)", e.Status)
	}
}
func chatURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("baseUrl must be an HTTP(S) URL")
	}
	p := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(p, "/chat/completions") {
		if !strings.HasSuffix(p, "/v1") {
			p += "/v1"
		}
		p += "/chat/completions"
	}
	u.Path = p
	return u.String(), nil
}
func retryDelay(header string, attempt int) time.Duration {
	delay := time.Duration(400*(1<<attempt)) * time.Millisecond
	delay += time.Duration(rand.Int64N(int64(delay/4) + 1))
	if seconds, err := strconv.ParseInt(strings.TrimSpace(header), 10, 64); err == nil && seconds >= 0 && seconds <= 86400 {
		return max(delay, time.Duration(seconds)*time.Second)
	}
	if date, err := http.ParseTime(header); err == nil {
		return max(delay, time.Until(date))
	}
	return delay
}

func request(ctx context.Context, endpoint string, body any, key string, timeout time.Duration, onRetry func(time.Duration) error) (*http.Response, context.CancelFunc, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
		if err != nil {
			cancel()
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(req)
		retry := err != nil
		if err == nil {
			retry = res.StatusCode == 429 || res.StatusCode == 500 || res.StatusCode == 502 || res.StatusCode == 503 || res.StatusCode == 504
		}
		if !retry || attempt == 2 {
			if err != nil {
				contextErr := ctx.Err()
				cancel()
				if contextErr != nil {
					return nil, nil, contextErr
				}
				return nil, nil, fmt.Errorf("unable to connect to API: %w", err)
			}
			return res, cancel, nil
		}
		header := ""
		if res != nil {
			header = res.Header.Get("Retry-After")
			res.Body.Close()
		}
		delay := retryDelay(header, attempt)
		if onRetry != nil {
			if err := onRetry(delay); err != nil {
				cancel()
				return nil, nil, err
			}
		}
		if err = wait(ctx, delay); err != nil {
			cancel()
			return nil, nil, err
		}
	}
	cancel()
	return nil, nil, fmt.Errorf("unable to connect to API")
}

type Provider struct {
	Config        Config
	Timeout       time.Duration
	noStreamUsage bool
	OnRetry       func(time.Duration) error
}
type Reply struct {
	Message Message
	Tokens  *int
}

func (p *Provider) chat(ctx context.Context, messages []Message, tools []Tool, stream bool, onText func(string) error) (Reply, error) {
	endpoint, err := chatURL(p.Config.BaseURL)
	if err != nil {
		return Reply{}, err
	}
	body := map[string]any{"model": p.Config.Model, "messages": messages, "stream": stream}
	if len(tools) > 0 {
		catalog := []any{}
		for _, t := range tools {
			catalog = append(catalog, map[string]any{"type": "function", "function": t})
		}
		body["tools"] = catalog
		body["tool_choice"] = "auto"
	}
	if stream && !p.noStreamUsage {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	res, cancel, err := request(ctx, endpoint, body, p.Config.APIKey, timeout, p.OnRetry)
	if err != nil {
		return Reply{}, err
	}
	defer cancel()
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1000))
		e := &APIError{res.StatusCode, string(b)}
		if stream && !p.noStreamUsage && (e.Status == 400 || e.Status == 422) && (strings.Contains(e.Details, "stream_options") || strings.Contains(e.Details, "include_usage")) {
			p.noStreamUsage = true
			res.Body.Close()
			cancel()
			return p.chat(ctx, messages, tools, stream, onText)
		}
		return Reply{}, e
	}
	if !stream {
		var data struct {
			Choices []struct {
				Message Message `json:"message"`
				Finish  string  `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				Tokens *int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err = json.NewDecoder(res.Body).Decode(&data); err != nil {
			return Reply{}, err
		}
		if len(data.Choices) == 0 || data.Choices[0].Message.Content == nil || text(data.Choices[0].Message) == "" {
			return Reply{}, fmt.Errorf("model returned an empty response")
		}
		if data.Choices[0].Finish == "length" || data.Choices[0].Finish == "content_filter" {
			return Reply{}, fmt.Errorf("provider returned an incomplete response")
		}
		m := data.Choices[0].Message
		m.Role = "assistant"
		return Reply{m, data.Usage.Tokens}, nil
	}
	calls := map[int]*Call{}
	var tokens *int
	var content strings.Builder
	completed, done := false, false
	event := func(payload string) error {
		if strings.TrimSpace(payload) == "[DONE]" {
			completed = true
			done = true
			return nil
		}
		var data struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Usage *struct {
				Tokens *int `json:"total_tokens"`
			} `json:"usage"`
			Choices []struct {
				Finish string `json:"finish_reason"`
				Delta  struct {
					Content *string `json:"content"`
					Calls   []struct {
						Index    int      `json:"index"`
						ID       string   `json:"id"`
						Function Function `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &data); err != nil {
			return fmt.Errorf("invalid provider stream: %w", err)
		}
		if data.Error != nil {
			return fmt.Errorf("provider stream failed: %s", data.Error.Message)
		}
		if data.Usage != nil && data.Usage.Tokens != nil {
			tokens = data.Usage.Tokens
		}
		if len(data.Choices) == 0 {
			return nil
		}
		choice := data.Choices[0]
		if choice.Finish == "length" || choice.Finish == "content_filter" {
			return fmt.Errorf("model stopped with %s; no partial tool calls executed", choice.Finish)
		}
		if choice.Finish != "" {
			completed = true
		}
		if choice.Delta.Content != nil {
			value := *choice.Delta.Content
			content.WriteString(value)
			if onText != nil {
				if err := onText(value); err != nil {
					return err
				}
			}
		}
		for _, delta := range choice.Delta.Calls {
			call := calls[delta.Index]
			if call == nil {
				call = &Call{Type: "function"}
				calls[delta.Index] = call
			}
			call.ID += delta.ID
			call.Function.Name += delta.Function.Name
			call.Function.Arguments += delta.Function.Arguments
		}
		return nil
	}
	reader := bufio.NewReader(res.Body)
	var data []string
	for !done {
		line, re := reader.ReadString('\n')
		if re != nil && re != io.EOF {
			return Reply{}, re
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" && len(data) > 0 {
			if err = event(strings.Join(data, "\n")); err != nil {
				return Reply{}, err
			}
			data = nil
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if re == io.EOF {
			if len(data) > 0 {
				if err = event(strings.Join(data, "\n")); err != nil {
					return Reply{}, err
				}
			}
			break
		}
	}
	if !completed {
		return Reply{}, fmt.Errorf("LLM stream ended before completion; partial tool calls were not executed")
	}
	m := Message{Role: "assistant"}
	if content.Len() > 0 {
		value := content.String()
		m.Content = &value
	}
	indices := []int{}
	for index := range calls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	seen := map[string]bool{}
	for _, index := range indices {
		call := *calls[index]
		if call.ID == "" || call.Function.Name == "" {
			return Reply{}, fmt.Errorf("provider returned an incomplete tool call")
		}
		if seen[call.ID] {
			return Reply{}, fmt.Errorf("provider returned duplicate tool call IDs")
		}
		seen[call.ID] = true
		if call.Function.Arguments == "" {
			call.Function.Arguments = "{}"
		}
		var args map[string]any
		if err = json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || args == nil {
			return Reply{}, fmt.Errorf("invalid JSON arguments for %s; no tool calls executed", call.Function.Name)
		}
		m.Calls = append(m.Calls, call)
	}
	if len(m.Calls) == 0 && m.Content == nil {
		return Reply{}, fmt.Errorf("model returned an empty response")
	}
	return Reply{m, tokens}, nil
}

var nativeUnsupported = regexp.MustCompile(`(?i)(tool|function).*(unsupported|unknown|not supported)|(unsupported|unknown).*(tool|function)`)

func toolsUnsupported(err error) bool {
	var e *APIError
	return errors.As(err, &e) && (e.Status == 400 || e.Status == 422) && nativeUnsupported.MatchString(e.Details)
}
func (p *Provider) fallback(ctx context.Context, history []Message, tools []Tool) (Reply, error) {
	messages := make([]Message, 0, len(history))
	for _, m := range history {
		switch {
		case m.Role == "system":
			m = message("system", text(m)+"\nAvailable tools (JSON schemas):\n"+jsonText(tools)+"\nIf a tool is needed, respond only with {\"tool\":\"tool_name\",\"input\":{...}}. Request one tool at a time. Otherwise answer. Compatibility mode exposes only read-only tools.")
		case m.Role == "tool":
			m = message("user", "Tool result ("+m.CallID+"): "+text(m))
		case m.Role == "assistant" && len(m.Calls) > 0:
			m = message("assistant", jsonText(m.Calls))
		}
		messages = append(messages, m)
	}
	reply, err := p.chat(ctx, messages, nil, false, nil)
	if err != nil {
		return Reply{}, err
	}
	s := strings.TrimSpace(text(reply.Message))
	if strings.HasPrefix(s, "```") {
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "```json"), "```"))
		s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	}
	var call struct {
		Tool  string         `json:"tool"`
		Input map[string]any `json:"input"`
	}
	if json.Unmarshal([]byte(s), &call) == nil && call.Tool != "" && call.Input != nil {
		reply.Message = Message{Role: "assistant", Calls: []Call{{ID: uuid(), Type: "function", Function: Function{call.Tool, jsonText(call.Input)}}}}
	}
	return reply, nil
}
func webSearch(r *Runtime, m map[string]any) (any, error) {
	query := strings.TrimSpace(stringArg(m, "query", ""))
	if query == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	config, err := loadConfig()
	if err != nil {
		return nil, err
	}
	key := config.TavilyKey
	if key == "" {
		return nil, fmt.Errorf("missing AMI_TAVILY_API_KEY")
	}
	res, cancel, err := request(r.Ctx, "https://api.tavily.com/search", map[string]any{"query": query, "max_results": 5, "search_depth": "basic"}, key, 60*time.Second, nil)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("web_search failed (%d)", res.StatusCode)
	}
	var data struct {
		Results []struct{ Title, URL, Content string } `json:"results"`
	}
	if err = json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}
	if len(data.Results) == 0 {
		return "No search results found", nil
	}
	parts := []string{}
	for i, result := range data.Results {
		parts = append(parts, fmt.Sprintf("%d. %s\nURL: %s\nContent: %s", i+1, result.Title, result.URL, result.Content))
	}
	return strings.Join(parts, "\n\n"), nil
}
