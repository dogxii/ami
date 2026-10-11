package ami

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testRuntime(t *testing.T, persist bool) *Runtime {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AMI_STATE_DIR", filepath.Join(t.TempDir(), "state"))
	r, err := newRuntime(context.Background(), root, "", persist, 256, "auto", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.cleanup()
		r.Session.close()
		if !persist {
			os.RemoveAll(r.Session.Directory)
		}
	})
	return r
}
func callTool(t *testing.T, r *Runtime, name string, input map[string]any) event {
	t.Helper()
	s, err := executeTool(r, name, input, toolAllowlist(toolCatalog))
	if err != nil {
		t.Fatal(err)
	}
	var data event
	if err = json.Unmarshal([]byte(s), &data); err != nil {
		t.Fatal(err)
	}
	return data
}
func TestLargeUnicodeRangesAndHugeLines(t *testing.T) {
	r := testRuntime(t, false)
	path := filepath.Join(r.Session.Cwd, "large.txt")
	content := strings.Repeat("previous\n", 80000) + strings.Repeat("你🙂", 10000) + "\nlast\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	page := callTool(t, r, "read_file", map[string]any{"path": "large.txt", "startLine": float64(80001), "endLine": float64(80001)})
	var full strings.Builder
	full.WriteString(page["content"].(string))
	for page["nextOffset"] != nil {
		page = callTool(t, r, "read_file", map[string]any{"path": "large.txt", "offset": page["nextOffset"]})
		full.WriteString(page["content"].(string))
	}
	if full.String() != strings.Repeat("你🙂", 10000)+"\nlast\n" {
		t.Fatal("UTF-8 pages did not reconstruct exact source")
	}
	if len(r.Versions) != 1 {
		t.Fatal("read version missing")
	}
}
func TestBinaryArtifactsAndDispatchMetadata(t *testing.T) {
	r := testRuntime(t, false)
	id, f, err := r.Session.Artifacts.create()
	if err != nil {
		t.Fatal(err)
	}
	bytes := []byte{0, 255, 1, 128}
	f.Write(bytes)
	f.Close()
	page := callTool(t, r, "read_output", map[string]any{"outputId": id})
	got, err := base64.StdEncoding.DecodeString(page["content"].(string))
	if err != nil || string(got) != string(bytes) || page["outputId"] != id || page["encoding"] != "base64" {
		t.Fatal(page, err)
	}
	entries, _ := os.ReadDir(r.Session.Artifacts.Directory)
	if len(entries) != 1 {
		t.Fatal("dispatcher created a nested output artifact")
	}
}
func TestAtomicEditsStaleVersionsAndCrossChunkMatch(t *testing.T) {
	r := testRuntime(t, false)
	path := filepath.Join(r.Session.Cwd, "code.txt")
	original := strings.Repeat("x", 65532) + "needle" + strings.Repeat("y", 70000)
	os.WriteFile(path, []byte(original), 0755)
	allowed := toolAllowlist(toolCatalog)
	if _, err := executeTool(r, "edit_file", map[string]any{"path": "code.txt", "oldText": "needle", "newText": "fixed"}, allowed); err == nil {
		t.Fatal("edit without read accepted")
	}
	callTool(t, r, "read_file", map[string]any{"path": "code.txt"})
	result := callTool(t, r, "edit_file", map[string]any{"path": "code.txt", "oldText": "needle", "newText": "fixed"})
	if result["replacements"] != float64(1) {
		t.Fatal(result)
	}
	b, _ := os.ReadFile(path)
	if string(b) != strings.Replace(original, "needle", "fixed", 1) {
		t.Fatal("streaming edit mismatch")
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0755 {
		t.Fatal("mode changed")
	}
	os.WriteFile(path, []byte("concurrent"), 0755)
	if _, err := executeTool(r, "edit_file", map[string]any{"path": "code.txt", "oldText": "concurrent", "newText": "bad"}, allowed); err == nil {
		t.Fatal("stale edit accepted")
	}
	callTool(t, r, "read_file", map[string]any{"path": "code.txt"})
	os.WriteFile(path, []byte("same same"), 0755)
	callTool(t, r, "read_file", map[string]any{"path": "code.txt"})
	if _, err := executeTool(r, "edit_file", map[string]any{"path": "code.txt", "oldText": "same", "newText": "new"}, allowed); err == nil {
		t.Fatal("ambiguous edit accepted")
	}
	b, _ = os.ReadFile(path)
	if string(b) != "same same" {
		t.Fatal("failed edit mutated file")
	}
	callTool(t, r, "edit_file", map[string]any{"path": "code.txt", "oldText": "same", "newText": "new", "replaceAll": true})
	b, _ = os.ReadFile(path)
	if string(b) != "new new" {
		t.Fatal(string(b))
	}
}
func TestWriteCreationOverwriteAndPermissions(t *testing.T) {
	r := testRuntime(t, false)
	allowed := toolAllowlist(toolCatalog)
	callTool(t, r, "write_file", map[string]any{"path": "nested/new.txt", "content": "first"})
	if _, err := executeTool(r, "write_file", map[string]any{"path": "nested/new.txt", "content": "second"}, allowed); err == nil {
		t.Fatal("overwrite without flag accepted")
	}
	callTool(t, r, "write_file", map[string]any{"path": "nested/new.txt", "content": "second", "overwrite": true})
	r.Mode = "read-only"
	for _, name := range []string{"write_file", "exec_command"} {
		input := map[string]any{"path": "other.txt", "content": "bad"}
		if name == "exec_command" {
			input = map[string]any{"command": "echo bad"}
		}
		if _, err := executeTool(r, name, input, allowed); err == nil {
			t.Fatal("read-only bypass", name)
		}
	}
	if _, err := executeTool(r, "read_file", map[string]any{"path": "nested/new.txt", "unknown": true}, allowed); err == nil {
		t.Fatal("unknown parameter accepted")
	}
	if _, err := executeTool(r, "read_file", map[string]any{"path": "nested/new.txt"}, map[string]bool{}); err == nil {
		t.Fatal("allowlist bypass")
	}
}
func TestSafePathsSymlinksAndSecrets(t *testing.T) {
	r := testRuntime(t, false)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600)
	os.WriteFile(filepath.Join(r.Session.Cwd, ".env"), []byte("secret"), 0600)
	for _, path := range []string{"../outside", ".env", ".ENV", ".git/config", ".GIT/config", "node_modules/file"} {
		if _, _, err := safePath(r.Session.Cwd, path, true); err == nil {
			t.Fatal("unsafe path accepted", path)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	for _, link := range []struct{ name, target string }{{"escape", outside}, {"secret-link", filepath.Join(r.Session.Cwd, ".env")}, {"dangling", filepath.Join(outside, "missing")}} {
		if err := os.Symlink(link.target, filepath.Join(r.Session.Cwd, link.name)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := safePath(r.Session.Cwd, link.name+"/child", true); err == nil {
			t.Fatal("symlink escape accepted", link.name)
		}
	}
}
func TestCompleteListingAndSearchFallback(t *testing.T) {
	r := testRuntime(t, false)
	dir := filepath.Join(r.Session.Cwd, "a/b/c/d/e")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "code.txt"), []byte(strings.Repeat("match\n", 80)), 0644)
	listing := callTool(t, r, "list_files", map[string]any{"depth": float64(6), "maxEntries": float64(2)})
	if listing["nextOffset"] == nil {
		t.Fatal("listing capped complete output")
	}
	path, _ := r.Session.Artifacts.path(listing["outputId"].(string))
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "a/b/c/d/e/code.txt") {
		t.Fatal(string(b))
	}
	t.Setenv("PATH", "")
	search := callTool(t, r, "search_code", map[string]any{"query": "match", "maxResults": float64(2)})
	if search["matches"] != float64(80) || search["nextOffset"] == nil {
		t.Fatal(search)
	}
	os.WriteFile(filepath.Join(r.Session.Cwd, "large.txt"), []byte(strings.Repeat("nonmatching line\n", 80000)+"target-at-end\n"), 0644)
	search = callTool(t, r, "search_code", map[string]any{"query": "target-at-end", "contextLines": float64(2)})
	if search["matches"] != float64(1) {
		t.Fatal(search)
	}
	path, _ = r.Session.Artifacts.path(search["outputId"].(string))
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "80001:target-at-end") {
		t.Fatal(string(b))
	}
}
