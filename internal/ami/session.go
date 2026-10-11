package ami

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var validID = regexp.MustCompile(`^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$`)

type Session struct {
	ID, Cwd, Directory string
	Persistent         bool
	Artifacts          *Artifacts
	History            []Message
	log                *os.File
	mu                 sync.Mutex
}

func latestPath(cwd string) string {
	sum := sha256.Sum256([]byte(cwd))
	return filepath.Join(stateDir(), "sessions", fmt.Sprintf("latest-%x", sum[:12]))
}
func sessionID(cwd, id string) (string, error) {
	if id == "latest" {
		b, err := os.ReadFile(latestPath(cwd))
		if err != nil {
			return "", fmt.Errorf("no previous session for this workspace")
		}
		id = strings.TrimSpace(string(b))
	}
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid session ID")
	}
	return id, nil
}
func openSession(cwd, resume string, persist bool, pageBytes int) (*Session, error) {
	var err error
	cwd, err = canonical(cwd)
	if err != nil {
		return nil, err
	}
	if !persist && resume != "" {
		return nil, fmt.Errorf("--no-session cannot be combined with --continue or --session")
	}
	id := uuid()
	dir := filepath.Join(stateDir(), "sessions", id)
	if resume != "" {
		id, err = sessionID(cwd, resume)
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(stateDir(), "sessions", id)
		if _, err = os.Stat(filepath.Join(dir, "events.jsonl")); err != nil {
			return nil, fmt.Errorf("session not found: %w", err)
		}
	} else if !persist {
		dir, err = os.MkdirTemp("", "ami-")
		if err != nil {
			return nil, err
		}
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	a, err := newArtifacts(filepath.Join(dir, "outputs"), pageBytes)
	if err != nil {
		return nil, err
	}
	s := &Session{ID: id, Cwd: cwd, Directory: dir, Persistent: persist, Artifacts: a}
	if persist {
		s.log, err = os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
		if err != nil {
			return nil, err
		}
	}
	if resume != "" {
		err = s.restore()
	} else {
		err = s.record(event{"type": "session.started", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "sessionId": id, "cwd": cwd, "version": 1})
	}
	if err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}
func (s *Session) activate() error {
	if !s.Persistent {
		return nil
	}
	return atomicWrite(latestPath(s.Cwd), []byte(s.ID), 0600)
}
func (s *Session) record(e event) error {
	if !s.Persistent || e["type"] == "message.delta" || e["type"] == "process.output" {
		return nil
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.log.Write(append(b, '\n'))
	return err
}
func (s *Session) add(m Message) error {
	if err := s.record(event{"type": "message.added", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": m}); err != nil {
		return err
	}
	s.History = append(s.History, m)
	return nil
}
func (s *Session) compact(messages []Message) error {
	if err := s.record(event{"type": "context.replaced", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "messages": messages}); err != nil {
		return err
	}
	s.History = messages
	return nil
}
func (s *Session) close() error {
	if s.log == nil {
		return nil
	}
	return s.log.Close()
}
func (s *Session) restore() error {
	f, err := os.Open(filepath.Join(s.Directory, "events.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var offset int64
	header := false
	torn := false
	newline := false
	for {
		line, readErr := r.ReadBytes('\n')
		if len(line) == 0 && readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		if torn {
			return fmt.Errorf("session history is corrupted before its final record")
		}
		var e struct {
			Type     string    `json:"type"`
			Cwd      string    `json:"cwd"`
			ID       string    `json:"sessionId"`
			Message  Message   `json:"message"`
			Messages []Message `json:"messages"`
		}
		if err = json.Unmarshal(line, &e); err != nil {
			torn = true
			if readErr != io.EOF {
				continue
			}
			break
		}
		if !header {
			if e.Type != "session.started" || e.ID != s.ID {
				return fmt.Errorf("invalid session header")
			}
			if e.Cwd != s.Cwd {
				return fmt.Errorf("session belongs to another workspace; use its original --cwd")
			}
			header = true
		}
		switch e.Type {
		case "message.added":
			s.History = append(s.History, e.Message)
		case "context.replaced":
			s.History = e.Messages
		}
		offset += int64(len(line))
		newline = line[len(line)-1] == '\n'
		if readErr == io.EOF {
			break
		}
	}
	if !header {
		return fmt.Errorf("invalid or missing session history")
	}
	if torn {
		// Windows append handles cannot truncate; reopen with write access for repair.
		if err = os.Truncate(s.log.Name(), offset); err != nil {
			return err
		}
		if err = s.record(event{"type": "session.recovered", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "reason": "Incomplete final record"}); err != nil {
			return err
		}
	} else if !newline {
		if _, err = s.log.Write([]byte{'\n'}); err != nil {
			return err
		}
	}
	repaired := false
	history := []Message{}
	for i := 0; i < len(s.History); i++ {
		m := s.History[i]
		history = append(history, m)
		if m.Role != "assistant" || len(m.Calls) == 0 {
			continue
		}
		answered := map[string]bool{}
		for i+1 < len(s.History) && s.History[i+1].Role == "tool" {
			i++
			history = append(history, s.History[i])
			answered[s.History[i].CallID] = true
		}
		for _, call := range m.Calls {
			if !answered[call.ID] {
				result := message("tool", "Interrupted before the result was recorded. The action may have executed. Inspect files and saved outputs before retrying; previous process IDs cannot be resumed.")
				result.CallID = call.ID
				history = append(history, result)
				repaired = true
			}
		}
	}
	if repaired {
		return s.compact(history)
	}
	return nil
}
func sessionHeader(cwd, id string) (event, error) {
	path := filepath.Join(stateDir(), "sessions", id, "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	line, err := bufio.NewReader(io.LimitReader(f, 8192)).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("invalid or missing session header")
	}
	var e event
	if err = json.Unmarshal(line, &e); err != nil {
		return nil, err
	}
	if e["type"] != "session.started" || e["sessionId"] != id || e["cwd"] != cwd {
		return nil, fmt.Errorf("session belongs to another workspace or has an invalid header")
	}
	return e, nil
}
