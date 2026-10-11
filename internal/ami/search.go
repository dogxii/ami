package ami

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type previewOutput struct {
	id                   string
	file                 *os.File
	content              strings.Builder
	count, limit, budget int
	closed               bool
}

func newPreview(a *Artifacts, limit int) (*previewOutput, error) {
	id, f, err := a.create()
	if err != nil {
		return nil, err
	}
	return &previewOutput{id: id, file: f, limit: limit, budget: a.PageBytes / 2}, nil
}
func (p *previewOutput) append(line string, match bool) error {
	if match {
		p.count++
	}
	if _, err := io.WriteString(p.file, line); err != nil {
		return err
	}
	if !p.closed && p.count <= p.limit && p.content.Len()+len(line) <= p.budget {
		p.content.WriteString(line)
	} else {
		p.closed = true
	}
	return nil
}
func (p *previewOutput) result(countKey string) event {
	var next any
	if p.closed {
		next = p.content.Len()
	}
	return event{"content": p.content.String(), countKey: p.count, "outputId": p.id, "nextOffset": next}
}
func listFiles(r *Runtime, m map[string]any) (any, error) {
	path, _, err := safePath(r.Session.Cwd, stringArg(m, "path", "."), false)
	if err != nil {
		return nil, err
	}
	out, err := newPreview(r.Session.Artifacts, intArg(m, "maxEntries", 300))
	if err != nil {
		return nil, err
	}
	defer out.file.Close()
	var visit func(string, int) error
	visit = func(dir string, depth int) error {
		if err := r.Ctx.Err(); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err = r.Ctx.Err(); err != nil {
				return err
			}
			p := filepath.Join(dir, e.Name())
			rel, _ := filepath.Rel(r.Session.Cwd, p)
			line := filepath.ToSlash(rel)
			if e.IsDir() {
				line += "/"
			}
			blocked := blockedName(e.Name())
			if blocked {
				line += " [blocked]"
			}
			if e.Type()&os.ModeSymlink != 0 {
				line += " [symlink]"
			}
			if err = out.append(line+"\n", true); err != nil {
				return err
			}
			if e.IsDir() && !blocked && depth < intArg(m, "depth", 1) {
				if err = visit(p, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err = visit(path, 1); err != nil {
		return nil, err
	}
	if err = out.file.Close(); err != nil {
		return nil, err
	}
	result := out.result("entries")
	if result["content"] == "" {
		if out.count == 0 {
			result["content"] = "Directory is empty."
		} else {
			result["content"] = "Listing exceeds the preview; use read_output."
		}
	}
	return result, nil
}
func searchCode(r *Runtime, m map[string]any) (any, error) {
	query := stringArg(m, "query", "")
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	path, rel, err := safePath(r.Session.Cwd, stringArg(m, "path", "."), false)
	if err != nil {
		return nil, err
	}
	out, err := newPreview(r.Session.Artifacts, intArg(m, "maxResults", 20))
	if err != nil {
		return nil, err
	}
	defer out.file.Close()
	contextLines := intArg(m, "contextLines", 0)
	appendLine := func(path string, number int, value string, match bool) error {
		separator := "-"
		if match {
			separator = ":"
		}
		return out.append(fmt.Sprintf("%s%s%d%s%s\n", path, separator, number, separator, strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r")), match)
	}
	result, err := r.Processes.runProgram("rg", []string{"--json", "--hidden", "--fixed-strings", "--context", strconv.Itoa(contextLines), "--iglob", "!**/node_modules/**", "--iglob", "!**/.git/**", "--iglob", "!**/.env*", "--", query, filepath.FromSlash(rel)}, r.Session.Cwd)
	if errors.Is(err, exec.ErrNotFound) || os.IsNotExist(err) {
		type line struct {
			number int
			text   string
		}
		var visit func(string) error
		visit = func(path string) error {
			if err := r.Ctx.Err(); err != nil {
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if info.IsDir() {
				entries, err := os.ReadDir(path)
				if err != nil {
					return err
				}
				for _, e := range entries {
					if !blockedName(e.Name()) && e.Type()&os.ModeSymlink == 0 {
						if err = visit(filepath.Join(path, e.Name())); err != nil {
							return err
						}
					}
				}
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			reader := bufio.NewReader(f)
			previous := []line{}
			number, remaining, lastWritten := 0, 0, 0
			rel, _ := filepath.Rel(r.Session.Cwd, path)
			for {
				if err = r.Ctx.Err(); err != nil {
					return err
				}
				value, re := reader.ReadString('\n')
				if value == "" && re == io.EOF {
					break
				}
				if re != nil && re != io.EOF {
					return re
				}
				number++
				if strings.ContainsRune(value, 0) {
					break
				}
				match := strings.Contains(value, query)
				if match {
					for _, l := range previous {
						if l.number > lastWritten {
							if err = appendLine(filepath.ToSlash(rel), l.number, l.text, false); err != nil {
								return err
							}
							lastWritten = l.number
						}
					}
					if err = appendLine(filepath.ToSlash(rel), number, value, true); err != nil {
						return err
					}
					lastWritten = number
					remaining = contextLines
				} else if remaining > 0 {
					if err = appendLine(filepath.ToSlash(rel), number, value, false); err != nil {
						return err
					}
					lastWritten = number
					remaining--
				}
				if contextLines > 0 {
					previous = append(previous, line{number, value})
					if len(previous) > contextLines {
						previous = previous[1:]
					}
				}
				if re == io.EOF {
					break
				}
			}
			return nil
		}
		err = visit(path)
	} else if err == nil {
		if result["exitCode"] != 0 && result["exitCode"] != 1 {
			return nil, fmt.Errorf("search failed: %v", result["stderr"].(event)["content"])
		}
		outputPath, _ := r.Session.Artifacts.path(result["stdoutId"].(string))
		f, openErr := os.Open(outputPath)
		if openErr != nil {
			return nil, openErr
		}
		defer f.Close()
		decoder := json.NewDecoder(f)
		for {
			if err = r.Ctx.Err(); err != nil {
				break
			}
			var e struct {
				Type string `json:"type"`
				Data struct {
					Path struct {
						Text string `json:"text"`
					} `json:"path"`
					Lines struct {
						Text string `json:"text"`
					} `json:"lines"`
					Number int `json:"line_number"`
				} `json:"data"`
			}
			err = decoder.Decode(&e)
			if err == io.EOF {
				err = nil
				break
			}
			if err != nil {
				break
			}
			if e.Type == "match" || e.Type == "context" {
				if err = appendLine(e.Data.Path.Text, e.Data.Number, e.Data.Lines.Text, e.Type == "match"); err != nil {
					break
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if err = out.file.Close(); err != nil {
		return nil, err
	}
	page := out.result("matches")
	if page["content"] == "" {
		if out.count == 0 {
			page["content"] = "No matches found."
		} else {
			page["content"] = "Matches exceed the preview; use read_output."
		}
	}
	return page, nil
}
