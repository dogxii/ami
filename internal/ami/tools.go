package ami

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Tool struct {
	Name        string                                      `json:"name"`
	Description string                                      `json:"description"`
	Parameters  map[string]any                              `json:"parameters"`
	Capability  string                                      `json:"-"`
	Run         func(*Runtime, map[string]any) (any, error) `json:"-"`
}

func schema(required []string, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "required": required, "properties": props, "additionalProperties": false}
}
func str() map[string]any     { return map[string]any{"type": "string"} }
func boolean() map[string]any { return map[string]any{"type": "boolean"} }
func integer(minimum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum}
}
func choice(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }
func validate(input map[string]any, schema map[string]any) error {
	if input == nil {
		return fmt.Errorf("tool input must be an object")
	}
	for _, name := range schema["required"].([]string) {
		if _, ok := input[name]; !ok {
			return fmt.Errorf("missing required input: %s", name)
		}
	}
	props := schema["properties"].(map[string]any)
	for name, value := range input {
		raw, ok := props[name]
		if !ok {
			return fmt.Errorf("unknown input: %s", name)
		}
		spec := raw.(map[string]any)
		valid := false
		switch spec["type"] {
		case "string":
			_, valid = value.(string)
		case "boolean":
			_, valid = value.(bool)
		case "integer":
			n, ok := value.(float64)
			valid = ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n == math.Trunc(n) && math.Abs(n) <= 9007199254740991
			if valid {
				if low, ok := spec["minimum"].(int); ok && n < float64(low) {
					return fmt.Errorf("%s must be >= %d", name, low)
				}
				if high, ok := spec["maximum"].(int); ok && n > float64(high) {
					return fmt.Errorf("%s must be <= %d", name, high)
				}
			}
		}
		if !valid {
			return fmt.Errorf("%s must be %s", name, spec["type"])
		}
		if enums, ok := spec["enum"].([]string); ok {
			found := false
			for _, candidate := range enums {
				if value == candidate {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("%s must be one of %s", name, strings.Join(enums, ", "))
			}
		}
	}
	return nil
}
func stringArg(m map[string]any, k, def string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return def
}
func intArg(m map[string]any, k string, def int) int {
	if v, ok := m[k].(float64); ok {
		return int(v)
	}
	return def
}
func boolArg(m map[string]any, k string) bool { v, _ := m[k].(bool); return v }

var toolCatalog = []Tool{
	{"read_file", "Read a workspace UTF-8 text file. Any total size; line ranges or byte pages. Follow nextOffset. Returns a version for safe edits.", schema([]string{"path"}, map[string]any{"path": str(), "startLine": integer(1), "endLine": integer(1), "offset": integer(0), "maxBytes": integer(4)}), "read", readFile},
	{"list_files", "List files to any depth (default 1). Does not follow symlinks or enter blocked directories. Complete listing saved for read_output.", schema([]string{}, map[string]any{"path": str(), "depth": integer(1), "maxEntries": integer(1)}), "read", listFiles},
	{"search_code", "Search literal text with ripgrep or a streaming fallback. No file size or total match cap. Complete results saved.", schema([]string{"query"}, map[string]any{"query": str(), "path": str(), "contextLines": integer(0), "maxResults": integer(1)}), "read", searchCode},
	{"git_status", "Show Git working tree status. Large results can be continued with read_output.", schema([]string{}, map[string]any{}), "read", gitStatus},
	{"git_diff", "Show tracked Git changes. External diff and textconv are disabled. Complete output saved.", schema([]string{}, map[string]any{"scope": choice("all", "staged", "working"), "path": str()}), "read", gitDiff},
	{"web_search", "Search the web using Tavily. Requires AMI_TAVILY_API_KEY.", schema([]string{"query"}, map[string]any{"query": str()}), "network", webSearch},
	{"edit_file", "Replace exact text. Read first. A unique match is required unless replaceAll=true. Rejects stale file versions.", schema([]string{"path", "oldText", "newText"}, map[string]any{"path": str(), "oldText": str(), "newText": str(), "replaceAll": boolean(), "expectedVersion": str()}), "write", editFile},
	{"write_file", "Create files and missing parents. Overwriting requires overwrite=true and a current read version. Prefer edit_file.", schema([]string{"path", "content"}, map[string]any{"path": str(), "content": str(), "overwrite": boolean(), "expectedVersion": str()}), "write", writeFile},
	{"exec_command", "Run a non-interactive shell command with host user permissions. timeoutMs defaults to 120000 (0 disables), waitMs defaults to 1000. Poll long commands before completing; processes end with this invocation.", schema([]string{"command"}, map[string]any{"command": str(), "cwd": str(), "timeoutMs": integer(0), "waitMs": map[string]any{"type": "integer", "minimum": 0, "maximum": 30000}, "keepStdinOpen": boolean()}), "exec", execCommand},
	{"process_control", "Poll incremental stdout/stderr pages, write stdin, or stop a command from this invocation. Saved process IDs cannot be resumed.", schema([]string{"processId", "action"}, map[string]any{"processId": str(), "action": choice("poll", "write", "stop"), "waitMs": map[string]any{"type": "integer", "minimum": 0, "maximum": 30000}, "text": str(), "closeStdin": boolean()}), "exec", processControl},
	{"read_output", "Read complete saved command/search/diff/stdin outputs by byte offset. Follow nextOffset. Binary pages use base64.", schema([]string{"outputId"}, map[string]any{"outputId": str(), "offset": integer(0), "maxBytes": integer(4)}), "read", func(r *Runtime, m map[string]any) (any, error) {
		return r.Session.Artifacts.page(stringArg(m, "outputId", ""), int64(intArg(m, "offset", 0)), intArg(m, "maxBytes", 0))
	}},
}

func selectTools(readOnly bool) []Tool {
	all := toolCatalog
	if !readOnly {
		return all
	}
	out := []Tool{}
	for _, t := range all {
		if t.Capability != "write" && t.Capability != "exec" {
			out = append(out, t)
		}
	}
	return out
}
func executeTool(r *Runtime, name string, input map[string]any, allowed map[string]bool) (content string, err error) {
	if !allowed[name] {
		return "", fmt.Errorf("tool is unavailable in this run: %s", name)
	}
	var t *Tool
	for _, candidate := range toolCatalog {
		if candidate.Name == name {
			v := candidate
			t = &v
			break
		}
	}
	if t == nil {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if err = validate(input, t.Parameters); err != nil {
		return "", err
	}
	if err = r.authorize(t.Capability); err != nil {
		return "", err
	}
	if err = r.Emit("tool.started", event{"name": name}); err != nil {
		return "", err
	}
	result, err := t.Run(r, input)
	if err == nil {
		if s, ok := result.(string); ok {
			content, err = r.Session.Artifacts.capture(s)
		} else {
			var b []byte
			b, err = json.Marshal(result)
			content = string(b)
		}
	}
	e := event{"name": name}
	if err != nil {
		e["error"] = err.Error()
	} else {
		e["result"] = content
	}
	emitErr := r.Emit("tool.completed", e)
	if err == nil {
		err = emitErr
	}
	return content, err
}
func readFile(r *Runtime, m map[string]any) (any, error) {
	_, hasOffset := m["offset"]
	_, hasStart := m["startLine"]
	_, hasEnd := m["endLine"]
	if hasOffset && (hasStart || hasEnd) {
		return nil, fmt.Errorf("use a line range or an offset, not both")
	}
	start, end := intArg(m, "startLine", 1), intArg(m, "endLine", 0)
	if hasEnd && end < start {
		return nil, fmt.Errorf("endLine must be >= startLine")
	}
	path, rel, err := safePath(r.Session.Cwd, stringArg(m, "path", ""), false)
	if err != nil {
		return nil, err
	}
	limit := min(intArg(m, "maxBytes", r.Session.Artifacts.PageBytes), r.Session.Artifacts.PageBytes)
	var page event
	if hasStart || hasEnd {
		page, err = readLinePage(r.Ctx, path, start, end, limit)
	} else {
		page, err = readBytePage(path, int64(intArg(m, "offset", 0)), limit, false)
	}
	if err != nil {
		return nil, err
	}
	r.Versions[path] = page["version"].(string)
	page["path"] = rel
	return page, nil
}
func execCommand(r *Runtime, m map[string]any) (any, error) {
	command := stringArg(m, "command", "")
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command must not be empty")
	}
	path, _, err := safePath(r.Session.Cwd, stringArg(m, "cwd", "."), false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cwd must be a directory")
	}
	timeout := intArg(m, "timeoutMs", 120000)
	if timeout > int(math.MaxInt64/int64(time.Millisecond)) {
		return nil, fmt.Errorf("timeoutMs is too large")
	}
	id, err := r.Processes.start(command, path, time.Duration(timeout)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !boolArg(m, "keepStdinOpen") {
		_ = r.Processes.closeInput(id)
	}
	return r.Processes.poll(id, time.Duration(intArg(m, "waitMs", 1000))*time.Millisecond)
}
func processControl(r *Runtime, m map[string]any) (any, error) {
	id := stringArg(m, "processId", "")
	switch stringArg(m, "action", "") {
	case "stop":
		return r.Processes.stop(id)
	case "write":
		return r.Processes.write(id, stringArg(m, "text", ""), boolArg(m, "closeStdin"))
	default:
		return r.Processes.poll(id, time.Duration(intArg(m, "waitMs", 1000))*time.Millisecond)
	}
}
func gitResult(r *Runtime, args []string) (any, error) {
	result, err := r.Processes.runProgram("git", args, r.Session.Cwd)
	if err != nil {
		return nil, err
	}
	if result["exitCode"] != 0 {
		return nil, fmt.Errorf("git failed: %v", result["stderr"].(event)["content"])
	}
	return result["stdout"], nil
}
func gitStatus(r *Runtime, _ map[string]any) (any, error) {
	return gitResult(r, []string{"--no-pager", "-c", "core.fsmonitor=false", "status", "--short"})
}
func gitDiff(r *Runtime, m map[string]any) (any, error) {
	_, rel, err := safePath(r.Session.Cwd, stringArg(m, "path", "."), true)
	if err != nil {
		return nil, err
	}
	args := []string{"--no-pager", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv"}
	switch stringArg(m, "scope", "") {
	case "all":
		args = append(args, "HEAD")
	case "staged":
		args = append(args, "--staged")
	}
	args = append(args, "--", filepath.FromSlash(rel), ":(exclude,icase,glob).env*", ":(exclude,icase,glob)**/.env*", ":(exclude,icase,glob)**/node_modules/**")
	return gitResult(r, args)
}
