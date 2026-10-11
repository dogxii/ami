package ami

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sessionInfo struct {
	ID      string    `json:"id"`
	Created string    `json:"created"`
	Updated time.Time `json:"updated"`
	Status  string    `json:"status"`
	Task    string    `json:"task"`
	Bytes   int64     `json:"bytes"`
}

func inspectSession(cwd, id string) (sessionInfo, error) {
	if !validID.MatchString(id) {
		return sessionInfo{}, fmt.Errorf("invalid session ID")
	}
	header, err := sessionHeader(cwd, id)
	if err != nil {
		return sessionInfo{}, err
	}
	directory := filepath.Join(stateDir(), "sessions", id)
	f, err := os.Open(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		return sessionInfo{}, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return sessionInfo{}, err
	}
	created, _ := header["timestamp"].(string)
	info := sessionInfo{ID: id, Created: created, Updated: stat.ModTime(), Status: "saved"}
	r := bufio.NewReader(f)
	corrupt := false
	for {
		line, readErr := r.ReadBytes('\n')
		if readErr != nil && readErr != io.EOF {
			return info, readErr
		}
		if len(line) != 0 {
			var e struct {
				Type    string  `json:"type"`
				Message Message `json:"message"`
			}
			if json.Unmarshal(line, &e) != nil {
				corrupt = true
			} else {
				switch e.Type {
				case "message.added":
					if info.Task == "" && e.Message.Role == "user" {
						info.Task = strings.Join(strings.Fields(text(e.Message)), " ")
						characters := []rune(info.Task)
						if len(characters) > 120 {
							info.Task = string(characters[:120]) + "…"
						}
					}
				case "run.started":
					info.Status = "unfinished"
				case "run.completed", "run.failed", "run.cancelled":
					info.Status = strings.TrimPrefix(e.Type, "run.")
				}
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	if corrupt {
		info.Status = "needs-recovery"
	}
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			stat, err := entry.Info()
			if err != nil {
				return err
			}
			info.Bytes += stat.Size()
		}
		return nil
	})
	return info, err
}

func listSessions(cwd string) ([]sessionInfo, error) {
	entries, err := os.ReadDir(filepath.Join(stateDir(), "sessions"))
	if os.IsNotExist(err) {
		return []sessionInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows := []sessionInfo{}
	for _, entry := range entries {
		if entry.IsDir() && validID.MatchString(entry.Name()) {
			info, err := inspectSession(cwd, entry.Name())
			if err == nil {
				rows = append(rows, info)
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Updated.After(rows[j].Updated) })
	return rows, nil
}

func deleteSession(cwd, id string, before time.Time) (bool, error) {
	lock, err := acquireSessionLock(id)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	if _, err = sessionHeader(cwd, id); err != nil {
		return false, err
	}
	directory := filepath.Join(stateDir(), "sessions", id)
	if !before.IsZero() {
		info, err := os.Stat(filepath.Join(directory, "events.jsonl"))
		if err != nil {
			return false, err
		}
		if !info.ModTime().Before(before) {
			return false, nil
		}
	}
	if err = os.RemoveAll(directory); err != nil {
		return false, err
	}
	return true, nil
}

func (app *application) sessions(ctx context.Context, cwd string, a arguments) error {
	root, err := canonical(cwd)
	if err != nil {
		return err
	}
	action := "list"
	if len(a.Pos) > 1 {
		action = a.Pos[1]
	}
	if a.Values["older-than"] != "" && action != "prune" {
		return fmt.Errorf("--older-than requires sessions prune")
	}
	if (a.Bools["yes"] || a.Bools["dry-run"]) && action != "delete" && action != "prune" {
		return fmt.Errorf("--yes/--dry-run require sessions delete or prune")
	}
	if len(a.Pos) > 3 || ((action == "list" || action == "prune") && len(a.Pos) > 2) {
		return fmt.Errorf("usage: ami sessions [list|show <id>|export <id>|delete <id>|prune --older-than <days>]")
	}
	if action == "show" || action == "export" || action == "delete" {
		if len(a.Pos) != 3 {
			return fmt.Errorf("usage: ami sessions %s <id>", action)
		}
		info, err := inspectSession(root, a.Pos[2])
		if err != nil {
			return err
		}
		if action == "show" {
			_, err = fmt.Fprintln(app.out, jsonText(info))
			return err
		}
		if action == "export" {
			f, err := os.Open(filepath.Join(stateDir(), "sessions", info.ID, "events.jsonl"))
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(app.out, f)
			return err
		}
		if !a.Bools["dry-run"] {
			if !a.Bools["yes"] {
				yes, err := app.confirm(ctx, "Delete session "+info.ID+" and all saved outputs?")
				if err != nil {
					return fmt.Errorf("use --yes to delete, or --dry-run to preview")
				}
				if !yes {
					return nil
				}
			}
			if _, err = deleteSession(root, info.ID, time.Time{}); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintln(app.out, jsonText(event{"type": "sessions.delete", "session": info, "dryRun": a.Bools["dry-run"]}))
		return err
	}
	if action != "list" && action != "prune" {
		return fmt.Errorf("unknown sessions action: %s", action)
	}
	rows, err := listSessions(root)
	if err != nil {
		return err
	}
	deleted, skipped := []string{}, []string{}
	if action == "prune" {
		days, err := a.number("older-than", 30, 1)
		if err != nil || days > 36500 {
			return fmt.Errorf("--older-than must be between 1 and 36500 days")
		}
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		candidates := []sessionInfo{}
		for _, row := range rows {
			if row.Updated.Before(cutoff) {
				candidates = append(candidates, row)
			}
		}
		rows = candidates
		if !a.Bools["dry-run"] && len(rows) != 0 {
			if !a.Bools["yes"] {
				yes, err := app.confirm(ctx, fmt.Sprintf("Delete %d sessions inactive for at least %d days in %s?", len(rows), days, root))
				if err != nil {
					return fmt.Errorf("use --yes to prune, or --dry-run to preview")
				}
				if !yes {
					return nil
				}
			}
			for _, row := range rows {
				removed, err := deleteSession(root, row.ID, cutoff)
				if err != nil && !errors.Is(err, errSessionBusy) {
					return err
				}
				if removed {
					deleted = append(deleted, row.ID)
				} else {
					skipped = append(skipped, row.ID)
				}
			}
		}
	}
	if a.Bools["json"] || action == "prune" {
		_, err = fmt.Fprintln(app.out, jsonText(event{"type": "sessions." + action, "sessions": rows, "deleted": deleted, "skipped": skipped, "dryRun": a.Bools["dry-run"]}))
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(app.out, "No saved sessions for this workspace.")
	}
	for _, row := range rows {
		fmt.Fprintf(app.out, "%s  %s  %s  %d bytes  %s\n", row.ID, row.Updated.Format(time.RFC3339), row.Status, row.Bytes, row.Task)
	}
	return nil
}
