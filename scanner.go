package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Session struct {
	ID         string
	Path       string
	CWD        string
	Project    string
	Entrypoint string
	Version    string
	GitBranch  string
	StartedAt  time.Time
	UpdatedAt  time.Time
	Preview    string
	Messages   int
}

type rawMsg struct {
	Type       string          `json:"type"`
	IsMeta     bool            `json:"isMeta"`
	UserType   string          `json:"userType"`
	Entrypoint string          `json:"entrypoint"`
	CWD        string          `json:"cwd"`
	SessionID  string          `json:"sessionId"`
	Version    string          `json:"version"`
	GitBranch  string          `json:"gitBranch"`
	Timestamp  string          `json:"timestamp"`
	Message    json.RawMessage `json:"message"`
}

type msgBody struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func projectsDir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude", "projects")
}

func decodeCWD(encoded string) string {
	// Claude encodes cwd by replacing '/' with '-' and prefixing '-'.
	// Best-effort: turn '-' back into '/'. Not perfectly reversible for dirs containing '-'.
	if strings.HasPrefix(encoded, "-") {
		return "/" + strings.ReplaceAll(encoded[1:], "-", "/")
	}
	return encoded
}

func scanAll() ([]Session, error) {
	root := projectsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projDir := filepath.Join(root, e.Name())
		files, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			full := filepath.Join(projDir, f.Name())
			s, err := parseSession(full)
			if err != nil {
				continue
			}
			if s.Project == "" {
				s.Project = decodeCWD(e.Name())
			}
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func parseSession(path string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()

	info, _ := f.Stat()
	s := Session{
		Path:      path,
		ID:        strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		UpdatedAt: info.ModTime(),
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var m rawMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.CWD != "" && s.CWD == "" {
			s.CWD = m.CWD
			s.Project = m.CWD
		}
		if m.Entrypoint != "" && s.Entrypoint == "" {
			s.Entrypoint = m.Entrypoint
		}
		if m.Version != "" && s.Version == "" {
			s.Version = m.Version
		}
		if m.GitBranch != "" && s.GitBranch == "" {
			s.GitBranch = m.GitBranch
		}
		if m.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, m.Timestamp); err == nil {
				if s.StartedAt.IsZero() || t.Before(s.StartedAt) {
					s.StartedAt = t
				}
			}
		}
		if m.Type == "user" && !m.IsMeta && m.UserType == "external" && s.Preview == "" {
			s.Preview = firstText(m.Message)
		}
		if m.Type == "user" || m.Type == "assistant" {
			s.Messages++
		}
	}
	if s.StartedAt.IsZero() {
		s.StartedAt = s.UpdatedAt
	}
	return s, nil
}

func firstText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var body msgBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}
	// content may be a string or an array of {type,text}
	var asStr string
	if err := json.Unmarshal(body.Content, &asStr); err == nil {
		return clean(asStr)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body.Content, &parts); err == nil {
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				return clean(p.Text)
			}
		}
	}
	return ""
}

func clean(s string) string {
	s = strings.TrimSpace(s)
	// strip leading command/system tags users don't want to see in the preview
	for _, pfx := range []string{"<local-command-", "<command-name>", "<command-message>"} {
		if strings.HasPrefix(s, pfx) {
			if i := strings.Index(s, ">"); i >= 0 && i+1 < len(s) {
				s = strings.TrimSpace(s[i+1:])
			}
		}
	}
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
