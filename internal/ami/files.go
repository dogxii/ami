package ami

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Artifacts struct {
	Directory string
	PageBytes int
}

func newArtifacts(dir string, page int) (*Artifacts, error) {
	if page < 4 {
		return nil, fmt.Errorf("page budget must be >= 4")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Artifacts{dir, page}, nil
}
func (a *Artifacts) path(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid output ID")
	}
	return filepath.Join(a.Directory, id+".txt"), nil
}
func (a *Artifacts) create() (string, *os.File, error) {
	id := uuid()
	path, _ := a.path(id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	return id, f, err
}
func (a *Artifacts) save(s string) (string, error) {
	id, f, err := a.create()
	if err != nil {
		return "", err
	}
	_, err = io.WriteString(f, s)
	ce := f.Close()
	if err != nil {
		return "", err
	}
	return id, ce
}
func (a *Artifacts) page(id string, offset int64, maxBytes int) (event, error) {
	path, err := a.path(id)
	if err != nil {
		return nil, err
	}
	if maxBytes == 0 || maxBytes > a.PageBytes {
		maxBytes = a.PageBytes
	}
	p, err := readBytePage(path, offset, maxBytes, true)
	if err != nil {
		return nil, err
	}
	p["outputId"] = id
	return p, nil
}
func (a *Artifacts) capture(s string) (string, error) {
	if len(s) <= a.PageBytes {
		return s, nil
	}
	id, err := a.save(s)
	if err != nil {
		return "", err
	}
	p, err := a.page(id, 0, 0)
	if err != nil {
		return "", err
	}
	p["instruction"] = "Use read_output with outputId and nextOffset to continue reading the complete result."
	return jsonText(p), nil
}
func fileVersion(info os.FileInfo) string {
	return fmt.Sprintf("%s:%d:%d:%d", fileIdentity(info), info.Size(), info.ModTime().UnixNano(), info.Mode())
}
func utf8End(b []byte, n int) int {
	if n >= len(b) {
		return n
	}
	for n > 0 && b[n]&0xc0 == 0x80 {
		n--
	}
	return n
}
func readBytePage(path string, offset int64, maxBytes int, binaryAllowed bool) (event, error) {
	if offset < 0 || maxBytes < 4 {
		return nil, fmt.Errorf("invalid byte page")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path must be a regular file")
	}
	if offset > info.Size() {
		return nil, fmt.Errorf("offset exceeds file size (%d)", info.Size())
	}
	remaining := info.Size() - offset
	readSize := min(int64(maxBytes), remaining)
	readSize += min(int64(4), remaining-readSize)
	b := make([]byte, int(readSize))
	n, err := f.ReadAt(b, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	b = b[:n]
	end := utf8End(b, min(maxBytes, n))
	binary := bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b[:end])
	if binary && !binaryAllowed {
		return nil, fmt.Errorf("only text files are supported")
	}
	var content string
	if binary {
		end = min(maxBytes, n)
		content = base64.StdEncoding.EncodeToString(b[:end])
	} else {
		content = string(b[:end])
	}
	var next any
	if offset+int64(end) < info.Size() {
		next = offset + int64(end)
	}
	p := event{"content": content, "offset": offset, "nextOffset": next, "sizeBytes": info.Size(), "version": fileVersion(info)}
	if binary {
		p["encoding"] = "base64"
	}
	return p, nil
}
func readLinePage(ctx context.Context, path string, start, end, maxBytes int) (event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path must be a regular file")
	}
	r := bufio.NewReaderSize(f, 64*1024)
	line := 1
	var position, startOffset int64
	found := false
	rangeDone := false
	b := make([]byte, 0, min(maxBytes, 64*1024))
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if end > 0 && line > end {
			rangeDone = true
			break
		}
		fragment, re := r.ReadSlice('\n')
		if len(fragment) > 0 {
			if bytes.IndexByte(fragment, 0) >= 0 {
				return nil, fmt.Errorf("only text files are supported")
			}
			if line >= start {
				if !found {
					startOffset = position
					found = true
				}
				take := min(len(fragment), maxBytes+4-len(b))
				b = append(b, fragment[:take]...)
			}
			position += int64(len(fragment))
			if fragment[len(fragment)-1] == '\n' {
				line++
			}
			if len(b) >= maxBytes+4 {
				break
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil && re != bufio.ErrBufferFull {
			return nil, re
		}
	}
	if !found {
		if start > line {
			return nil, fmt.Errorf("startLine exceeds file length (%d)", line)
		}
		startOffset = info.Size()
	}
	count := utf8End(b, min(maxBytes, len(b)))
	if !utf8.Valid(b[:count]) {
		return nil, fmt.Errorf("only UTF-8 text files are supported")
	}
	content := string(b[:count])
	var next any
	if startOffset+int64(count) < info.Size() && (!rangeDone || len(b) > count) {
		next = startOffset + int64(count)
	}
	last := start + strings.Count(content, "\n")
	if strings.HasSuffix(content, "\n") {
		last--
	}
	return event{"content": content, "startLine": start, "endLine": max(start, last), "offset": startOffset, "nextOffset": next, "sizeBytes": info.Size(), "version": fileVersion(info)}, nil
}
func blockedName(name string) bool {
	name = strings.ToLower(name)
	return name == ".git" || name == "node_modules" || name == ".env" || (strings.HasPrefix(name, ".env.") && name != ".env.example" && name != ".env.sample" && name != ".env.template")
}
func checkRelative(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be inside the workspace")
	}
	for _, part := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if blockedName(part) {
			return "", fmt.Errorf("access to this path is blocked")
		}
	}
	return rel, nil
}
func safePath(root, path string, missing bool) (string, string, error) {
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target = filepath.Clean(target)
	rel, err := checkRelative(root, target)
	if err != nil {
		return "", "", err
	}
	existing := target
	for {
		info, err := os.Lstat(existing)
		if err == nil {
			real, err := filepath.EvalSymlinks(existing)
			if err != nil {
				return "", "", fmt.Errorf("dangling or invalid symlink: %w", err)
			}
			if _, err = checkRelative(root, real); err != nil {
				return "", "", err
			}
			if existing == target {
				target = real
			} else if info.Mode()&os.ModeSymlink != 0 {
				target = filepath.Join(real, strings.TrimPrefix(target, existing+string(filepath.Separator)))
			}
			break
		}
		if !os.IsNotExist(err) || !missing {
			return "", "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", "", err
		}
		existing = parent
	}
	return target, filepath.ToSlash(rel), nil
}
