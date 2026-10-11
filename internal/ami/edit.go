package ami

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func editFile(r *Runtime, m map[string]any) (any, error) {
	old := stringArg(m, "oldText", "")
	if old == "" {
		return nil, fmt.Errorf("oldText must not be empty; use write_file")
	}
	path, rel, err := safePath(r.Session.Cwd, stringArg(m, "path", ""), false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path must be a regular file")
	}
	version := fileVersion(info)
	expected := stringArg(m, "expectedVersion", r.Versions[path])
	if expected == "" {
		return nil, fmt.Errorf("read the file before editing it, or provide expectedVersion")
	}
	if version != expected {
		return nil, fmt.Errorf("file changed since it was read; read it again")
	}
	input, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	out, err := os.CreateTemp(filepath.Dir(path), ".ami-edit-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	if err = out.Chmod(info.Mode().Perm()); err != nil {
		return nil, err
	}
	needle, replacement := []byte(old), []byte(stringArg(m, "newText", ""))
	buffer := make([]byte, 64*1024)
	carry := []byte{}
	matches := 0
	for {
		if err = r.Ctx.Err(); err != nil {
			return nil, err
		}
		n, re := input.Read(buffer)
		data := append(carry, buffer[:n]...)
		if bytes.IndexByte(data, 0) >= 0 {
			return nil, fmt.Errorf("only text files are supported")
		}
		from := 0
		for {
			at := bytes.Index(data[from:], needle)
			if at < 0 {
				break
			}
			at += from
			matches++
			if matches > 1 && !boolArg(m, "replaceAll") {
				return nil, fmt.Errorf("oldText matches multiple locations; include more context or use replaceAll")
			}
			if _, err = out.Write(data[from:at]); err != nil {
				return nil, err
			}
			if _, err = out.Write(replacement); err != nil {
				return nil, err
			}
			from = at + len(needle)
		}
		tail := data[from:]
		safe := max(0, len(tail)-len(needle)+1)
		if _, err = out.Write(tail[:safe]); err != nil {
			return nil, err
		}
		carry = append([]byte(nil), tail[safe:]...)
		if re == io.EOF {
			break
		}
		if re != nil {
			return nil, re
		}
	}
	if _, err = out.Write(carry); err != nil {
		return nil, err
	}
	if matches == 0 {
		return nil, fmt.Errorf("oldText was not found; read the file and use exact text")
	}
	if err = out.Sync(); err != nil {
		return nil, err
	}
	if err = out.Close(); err != nil {
		return nil, err
	}
	if err = input.Close(); err != nil {
		return nil, err
	}
	if err = r.Ctx.Err(); err != nil {
		return nil, err
	}
	checked, _, err := safePath(r.Session.Cwd, stringArg(m, "path", ""), false)
	if err != nil {
		return nil, err
	}
	current, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if checked != path || fileVersion(current) != version {
		return nil, fmt.Errorf("file changed during editing; no changes applied")
	}
	if err = os.Rename(out.Name(), path); err != nil {
		return nil, err
	}
	updated, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	next := fileVersion(updated)
	r.Versions[path] = next
	return event{"path": rel, "replacements": matches, "version": next}, nil
}
func writeFile(r *Runtime, m map[string]any) (any, error) {
	path, rel, err := safePath(r.Session.Cwd, stringArg(m, "path", ""), true)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	path, rel, err = safePath(r.Session.Cwd, stringArg(m, "path", ""), true)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	exists := err == nil
	mode := os.FileMode(0644)
	version := ""
	if exists {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("path must be a regular file")
		}
		version = fileVersion(info)
		if !boolArg(m, "overwrite") {
			return nil, fmt.Errorf("file already exists; use edit_file or read before overwriting")
		}
		if stringArg(m, "expectedVersion", r.Versions[path]) != version {
			return nil, fmt.Errorf("read the current file before overwriting; its version must match")
		}
		mode = info.Mode().Perm()
	}
	out, err := os.CreateTemp(filepath.Dir(path), ".ami-write-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	if err = out.Chmod(mode); err != nil {
		return nil, err
	}
	content := stringArg(m, "content", "")
	if _, err = io.WriteString(out, content); err != nil {
		return nil, err
	}
	if err = out.Sync(); err != nil {
		return nil, err
	}
	if err = out.Close(); err != nil {
		return nil, err
	}
	if err = r.Ctx.Err(); err != nil {
		return nil, err
	}
	checked, _, err := safePath(r.Session.Cwd, stringArg(m, "path", ""), true)
	if err != nil {
		return nil, err
	}
	if checked != path {
		return nil, fmt.Errorf("file path changed during writing")
	}
	if exists {
		current, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if fileVersion(current) != version {
			return nil, fmt.Errorf("file changed during writing; no changes applied")
		}
		err = os.Rename(out.Name(), path)
	} else {
		err = os.Link(out.Name(), path)
	}
	if err != nil {
		return nil, err
	}
	if !exists {
		if err = os.Remove(out.Name()); err != nil {
			return nil, err
		}
	}
	updated, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	next := fileVersion(updated)
	r.Versions[path] = next
	return event{"path": rel, "created": !exists, "bytesWritten": len(content), "version": next}, nil
}
