package sessions

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session is the normalized model of one agent session: everything the fold
// engine needs to correlate commits with the work recorded in a session log.
// Paths holds the files the session touched, made repo-relative when the log
// recorded them inside the repo, so commit streams and sessions can be
// compared on equal footing.
type Session struct {
	ID          string    // session identifier from the log
	Path        string    // path of the session log on disk; "" for synthetic sessions
	Agent       string    // canonical agent name, e.g. "Claude"
	Model       string    // model identifier the agent reported
	FirstPrompt string    // first real user prompt — the task intent
	Start       time.Time // first timestamp in the log
	End         time.Time // last timestamp in the log
	Paths       []string  // files the session touched, repo-relative when possible
}

// ClaudeProjectsDir returns the directory where Claude Code stores per-repo
// session logs (~/.claude/projects).
func ClaudeProjectsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// ClaudeProjectSlug encodes an absolute path the way Claude Code names its
// per-repo project directories: every character that is not an ASCII letter,
// digit or hyphen becomes a hyphen ("/Users/me/repo" -> "-Users-me-repo").
func ClaudeProjectSlug(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// DiscoverClaudeSessions loads the Claude Code session logs recorded for the
// repository at repoRoot. Missing or unparseable logs are not an error: repos
// without session logs still fold on trailers and time gaps, so discovery
// returns as much as it can and an empty result rather than a failure.
func DiscoverClaudeSessions(repoRoot string) ([]Session, error) {
	dir, err := ClaudeProjectsDir()
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	// Claude Code slugs the working directory it saw; a symlinked repo path
	// can differ from the resolved one, so try both.
	roots := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
		roots = append(roots, resolved)
	}
	byID := make(map[string]Session)
	for _, r := range roots {
		if err := loadClaudeDir(filepath.Join(dir, ClaudeProjectSlug(r)), r, byID); err != nil {
			return nil, err
		}
	}
	out := make([]Session, 0, len(byID))
	for _, s := range byID {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// loadClaudeDir parses every top-level *.jsonl log in projectDir against
// repoRoot, filling byID (deduplicated by session ID). Session logs live at
// the top level; subdirectories hold tool artifacts and are ignored.
func loadClaudeDir(projectDir, repoRoot string, byID map[string]Session) error {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // no logs for this repo — nothing to fold on
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		s, err := LoadClaudeSession(filepath.Join(projectDir, e.Name()), repoRoot)
		if err != nil || s.ID == "" || s.Start.IsZero() {
			continue // unusable log — skip it
		}
		if _, dup := byID[s.ID]; !dup {
			byID[s.ID] = s
		}
	}
	return nil
}

// LoadClaudeSession parses one Claude Code JSONL session log into a Session.
// repoRoot relativizes touched paths: files under the repo become
// repo-relative, anything else is dropped because it cannot match this
// repo's commits.
func LoadClaudeSession(logPath, repoRoot string) (Session, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()
	return parseClaudeSession(f, logPath, repoRoot)
}

// parseClaudeSession reads one JSONL log. The format is append-ordered JSON
// per line with many auxiliary record types (queue operations, summaries,
// attachments, sidechain turns); only the user prompts, the assistant model,
// the touched file paths and the timestamp span are extracted, and every
// malformed line is skipped rather than failing the session.
func parseClaudeSession(r io.Reader, logPath, repoRoot string) (Session, error) {
	scan := &sessionScan{seenPath: make(map[string]struct{})}
	scan.s.Path = logPath
	scan.s.Agent = "Claude"
	reader := bufio.NewReader(r)
	decoded := 0
	for {
		line, err := reader.ReadString('\n')
		if rec, ok := decodeRecord(line); ok {
			decoded++
			scan.apply(rec, repoRoot)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return Session{}, err
		}
	}
	if decoded == 0 {
		return Session{}, errors.New("sessions: no usable records in log")
	}
	if scan.s.Start.IsZero() {
		return Session{}, errors.New("sessions: no timestamps in log")
	}
	if scan.s.FirstPrompt == "" && scan.command != "" {
		// A slash-command-driven session has no typed prompt; the command
		// invocation is still the user's intent.
		scan.s.FirstPrompt = scan.command
	}
	scan.s.Paths = scan.paths
	return scan.s, nil
}

// sessionScan accumulates the extracted session fields across log records.
type sessionScan struct {
	s        Session
	paths    []string
	seenPath map[string]struct{}
	command  string // first slash-command invocation, used when no typed prompt exists
}

type claudeRecord struct {
	Type        string         `json:"type"`
	Timestamp   string         `json:"timestamp"`
	SessionID   string         `json:"sessionId"`
	IsMeta      bool           `json:"isMeta"`
	IsSidechain bool           `json:"isSidechain"`
	Message     *claudeMessage `json:"message"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

type claudeBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Input json.RawMessage `json:"input"`
}

type claudeToolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
}

func decodeRecord(line string) (claudeRecord, bool) {
	var rec claudeRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return claudeRecord{}, false
	}
	return rec, true
}

// apply folds one record into the scan: identifier, timestamp span, model,
// first real user prompt, and touched paths from tool calls. Sidechain
// (subagent) and meta records carry no user intent but their file edits
// still count as work done by the session.
func (sc *sessionScan) apply(rec claudeRecord, repoRoot string) {
	s := &sc.s
	if s.ID == "" && rec.SessionID != "" {
		s.ID = rec.SessionID
	}
	if rec.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
			if s.Start.IsZero() || t.Before(s.Start) {
				s.Start = t
			}
			if t.After(s.End) {
				s.End = t
			}
		}
	}
	msg := rec.Message
	if msg == nil {
		return
	}
	if s.Model == "" && msg.Model != "" {
		s.Model = msg.Model
	}
	text, blocks, isString := messageContent(msg.Content)
	switch {
	case isString:
		if msg.Role != "user" || rec.IsMeta || rec.IsSidechain || s.FirstPrompt != "" {
			return
		}
		if isPrompt(text) {
			s.FirstPrompt = strings.TrimSpace(text)
		} else if intent := commandIntent(text); intent != "" && sc.command == "" {
			sc.command = intent
		}
	case msg.Role == "assistant":
		for _, b := range blocks {
			if b.Type != "tool_use" || len(b.Input) == 0 {
				continue
			}
			var input claudeToolInput
			if err := json.Unmarshal(b.Input, &input); err != nil {
				continue
			}
			for _, p := range []string{input.FilePath, input.NotebookPath} {
				if rel, ok := relativize(p, repoRoot); ok {
					if _, dup := sc.seenPath[rel]; !dup {
						sc.seenPath[rel] = struct{}{}
						sc.paths = append(sc.paths, rel)
					}
				}
			}
		}
	case msg.Role == "user" && !rec.IsMeta && !rec.IsSidechain && s.FirstPrompt == "":
		var prompt strings.Builder
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				prompt.WriteString(b.Text)
				prompt.WriteByte('\n')
			}
		}
		if t := strings.TrimSpace(prompt.String()); isPrompt(t) {
			s.FirstPrompt = t
		}
	}
}

// commandIntent extracts "/name args" from a Claude Code command-invocation
// message, whose shape is a command-message line followed by a command-name
// line and an optional command-args line.
func commandIntent(text string) string {
	name := xmlInner(text, "command-name")
	if name == "" {
		return ""
	}
	intent := strings.TrimSpace(name)
	if args := xmlInner(text, "command-args"); args != "" {
		intent += " " + strings.Join(strings.Fields(args), " ")
	}
	return intent
}

// xmlInner returns the text between the first <tag>...</tag> pair.
func xmlInner(text, tag string) string {
	open, close := "<"+tag+">", "</"+tag+">"
	i := strings.Index(text, open)
	if i < 0 {
		return ""
	}
	rest := text[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// messageContent handles the two shapes of message.content: a plain string
// or an array of typed blocks.
func messageContent(raw json.RawMessage) (text string, blocks []claudeBlock, isString bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil, true
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return "", blocks, false
	}
	return "", nil, false
}

// isPrompt filters the known non-prompt user messages Claude Code
// interleaves into logs: command invocations and their output, injected
// caveats and reminders, IDE notifications, background-task notifications,
// and continuation summaries.
func isPrompt(text string) bool {
	if text == "" {
		return false
	}
	for _, prefix := range []string{
		"<command-",
		"<local-command",
		"<task-notification>",
		"<ide_opened_file>",
		"<system-reminder>",
		"Caveat:",
		"This session is being continued",
		"Base directory for this skill:",
	} {
		if strings.HasPrefix(text, prefix) {
			return false
		}
	}
	return true
}

// relativize makes an absolute tool path repo-relative. Paths outside the
// repo (or unusable ones) are dropped: they cannot match this repo's commits.
func relativize(path, repoRoot string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		return filepath.Clean(path), true
	}
	if repoRoot == "" {
		return "", false
	}
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}
