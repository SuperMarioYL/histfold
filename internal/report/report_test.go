package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SuperMarioYL/histfold/internal/fold"
)

var stamp = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

func sampleSets() []fold.Changeset {
	c1 := fold.Commit{
		Hash: "aaa111bbb222", Author: "Leo",
		Message: "wip on login", Time: stamp,
		Files: []fold.FileStat{{Path: "internal/login.go", Added: 30, Deleted: 5}},
	}
	c2 := fold.Commit{
		Hash: "ddd333eee444", Author: "Leo",
		Message: "finish login", Time: stamp.Add(10 * time.Minute),
		Files: []fold.FileStat{{Path: "internal/login.go", Added: 4, Deleted: 3}},
	}
	return []fold.Changeset{
		{
			ID:           "f00dcafe1234",
			Intent:       "Fix the login redirect bug",
			Agent:        "Claude",
			Model:        "claude-sonnet-4-5",
			Commits:      []fold.Commit{c1, c2},
			Files:        []fold.FileStat{{Path: "internal/login.go", Added: 34, Deleted: 8}},
			SessionRef:   "/logs/s1.jsonl",
			ReviewStatus: fold.ReviewOnMain,
			Confidence:   fold.ConfSession,
		},
		{
			ID:           "deadbeef5678",
			Intent:       "Tidy docs",
			Commits:      []fold.Commit{c1},
			Files:        []fold.FileStat{{Path: "docs.md", Added: 1, Deleted: 0}},
			ReviewStatus: fold.ReviewUnknown,
			Confidence:   fold.ConfTimeGap,
		},
	}
}

func TestTerminal(t *testing.T) {
	var buf bytes.Buffer
	if err := Terminal(&buf, sampleSets()); err != nil {
		t.Fatalf("Terminal() error = %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"3 commits → 2 changesets",
		"INTENT",
		"Fix the login redirect bug",
		"Claude",
		"claude-sonnet-4-5",
		"on-main",
		"90%",
		"20%",
		"2 files touched, +35/-8",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal output missing %q:\n%s", want, out)
		}
	}
}

func TestTerminalEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Terminal(&buf, nil); err != nil {
		t.Fatalf("Terminal() error = %v", err)
	}
	if !strings.Contains(buf.String(), "0 commits → 0 changesets") {
		t.Errorf("empty output = %q", buf.String())
	}
}

func TestTruncateMultibyte(t *testing.T) {
	long := strings.Repeat("汉", 60)
	got := truncate(long, 48)
	if want := 48; len([]rune(got)) != want {
		t.Errorf("truncate kept %d runes, want %d", len([]rune(got)), want)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated string should end with an ellipsis: %q", got)
	}
	if truncate("short", 48) != "short" {
		t.Error("short strings must pass through unchanged")
	}
}

func TestHTML(t *testing.T) {
	var buf bytes.Buffer
	if err := HTML(&buf, sampleSets(), Meta{Repo: "demo-repo"}); err != nil {
		t.Fatalf("HTML() error = %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Fix the login redirect bug",
		"Tidy docs",
		"aaa111bb",
		"ddd333ee",
		"internal/login.go",
		"/logs/s1.jsonl",
		"demo-repo",
		"Expand all",
		"data-toggle",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q", want)
		}
	}
	// Self-containment: no external scripts, styles or fonts.
	for _, banned := range []string{"<script src", "src=\"http", "href=\"http", "@import", "url(http"} {
		if strings.Contains(out, banned) {
			t.Errorf("html output is not self-contained: found %q", banned)
		}
	}
	// Intent text containing HTML must be escaped.
	sets := sampleSets()
	sets[0].Intent = "Fix <script>alert(1)</script>"
	buf.Reset()
	if err := HTML(&buf, sets, Meta{}); err != nil {
		t.Fatalf("HTML() error = %v", err)
	}
	if strings.Contains(buf.String(), "<script>alert(1)</script>") {
		t.Error("html output must escape intent text")
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, sampleSets()); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	var back []fold.Changeset
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(back) != 2 {
		t.Fatalf("round-tripped %d changesets, want 2", len(back))
	}
	if back[0].Intent != "Fix the login redirect bug" || back[0].Agent != "Claude" {
		t.Errorf("round-trip lost fields: %#v", back[0])
	}
	if back[0].Commits[0].Hash != "aaa111bbb222" {
		t.Errorf("commit hash not round-tripped: %#v", back[0].Commits[0])
	}
}
