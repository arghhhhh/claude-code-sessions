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
	Name       string
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
	UUID        string          `json:"uuid"`
	Summary     string          `json:"summary"`
	LeafUUID    string          `json:"leafUuid"`
	CustomTitle string          `json:"customTitle"`
	Message     json.RawMessage `json:"message"`
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
	// summaries maps a leaf message uuid -> conversation name. Claude writes
	// summary entries (the titles its /resume picker shows) keyed by the leaf
	// uuid of the summarized branch, often into a *different* jsonl file than
	// the session they name — so collect them across all files first.
	summaries := map[string]string{}
	// titles maps a sessionId -> the explicit name set via /rename
	// (a "custom-title" entry). These take precedence over summaries.
	titles := map[string]string{}
	// uuidsByIdx holds each session's message uuids (in file order) so we can
	// match them against summaries in a second pass.
	var uuidsByIdx [][]string
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
			s, uuids, sums, tls, err := parseSession(full)
			if err != nil {
				continue
			}
			if s.Project == "" {
				s.Project = decodeCWD(e.Name())
			}
			for k, v := range sums {
				summaries[k] = v
			}
			for k, v := range tls {
				titles[k] = v
			}
			out = append(out, s)
			uuidsByIdx = append(uuidsByIdx, uuids)
		}
	}
	// Assign each session its name. Precedence: an explicit /rename title wins;
	// otherwise use a summary whose leaf uuid appears in this session (keeping
	// the last, most recent match).
	for i := range out {
		for _, u := range uuidsByIdx[i] {
			if name, ok := summaries[u]; ok {
				out[i].Name = name
			}
		}
		if t, ok := titles[out[i].ID]; ok {
			out[i].Name = t
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// parseSession reads one jsonl file and returns its Session, the ordered list
// of message uuids it contains, any summary entries found in it (leafUuid ->
// cleaned summary text), and any /rename titles (sessionId -> cleaned title).
func parseSession(path string) (Session, []string, map[string]string, map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, nil, nil, nil, err
	}
	defer f.Close()

	info, _ := f.Stat()
	s := Session{
		Path:      path,
		ID:        strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		UpdatedAt: info.ModTime(),
	}
	var uuids []string
	summaries := map[string]string{}
	titles := map[string]string{}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var m rawMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.Type == "summary" && m.LeafUUID != "" && m.Summary != "" {
			summaries[m.LeafUUID] = clean(m.Summary)
			continue
		}
		if m.Type == "custom-title" && m.SessionID != "" && m.CustomTitle != "" {
			titles[m.SessionID] = clean(m.CustomTitle)
			continue
		}
		if m.UUID != "" {
			uuids = append(uuids, m.UUID)
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
	return s, uuids, summaries, titles, nil
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
	// Slash-command messages are stored as an XML-ish blob, e.g.
	//   <command-name>/voice</command-name>
	//   <command-message>voice</command-message>
	//   <command-args>some args</command-args>
	// Render them as "/voice some args" instead of leaking raw tags.
	if strings.HasPrefix(s, "<command-name>") {
		name := strings.TrimSpace(tagContent(s, "command-name"))
		if args := strings.TrimSpace(tagContent(s, "command-args")); args != "" {
			name += " " + args
		}
		s = name
	} else {
		// strip a single leading tag users don't want to see in the preview
		for _, pfx := range []string{"<local-command-", "<command-message>"} {
			if strings.HasPrefix(s, pfx) {
				if i := strings.Index(s, ">"); i >= 0 && i+1 < len(s) {
					s = strings.TrimSpace(s[i+1:])
				}
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

// tagContent returns the text between <tag> and </tag>, or "" if absent.
func tagContent(s, tag string) string {
	open, close := "<"+tag+">", "</"+tag+">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	i += len(open)
	j := strings.Index(s[i:], close)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}
