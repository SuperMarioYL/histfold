package gitlog

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SuperMarioYL/histfold/internal/fold"
)

// scratchRepo creates a scratch repository with a fixed identity; it fails
// the test when git is unusable.
func scratchRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gt(t, dir, "init", "-q", "-b", "main")
	gt(t, dir, "config", "user.name", "Test Author")
	gt(t, dir, "config", "user.email", "test@example.com")
	return dir
}

// gt runs one git command in dir.
func gt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// writeFile drops a file into the repo and stages it.
func writeFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gt(t, dir, "add", "-A")
}

// commit commits the staged tree; trailer, when set, becomes the message's
// trailer block.
func commit(t *testing.T, dir, subject, trailer string) {
	t.Helper()
	if trailer != "" {
		gt(t, dir, "commit", "-q", "-m", subject, "-m", trailer)
		return
	}
	gt(t, dir, "commit", "-q", "-m", subject)
}

func TestLoad(t *testing.T) {
	dir := scratchRepo(t)
	writeFile(t, dir, "app.go", "package main\n")
	writeFile(t, dir, "README.md", "# demo\n")
	commit(t, dir, "add app", "Co-Authored-By: Claude <claude@anthropic.com>")
	writeFile(t, dir, "app.go", "package mainx\n")
	commit(t, dir, "fix app", "Agent: Codex\nAgent-Model: codex-max")

	commits, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}

	// git log prints newest first; check both entries.
	newest, oldest := commits[0], commits[1]
	if newest.Message != "fix app\n\nAgent: Codex\nAgent-Model: codex-max" {
		t.Errorf("newest message = %q", newest.Message)
	}
	if oldest.Message != "add app\n\nCo-Authored-By: Claude <claude@anthropic.com>" {
		t.Errorf("oldest message = %q", oldest.Message)
	}
	if len(oldest.Files) != 2 {
		t.Fatalf("oldest has %d files, want 2: %+v", len(oldest.Files), oldest.Files)
	}
	if len(newest.Files) != 1 || newest.Files[0].Path != "app.go" {
		t.Fatalf("newest files = %+v, want one app.go", newest.Files)
	}
	if newest.Files[0].Added != 1 || newest.Files[0].Deleted != 1 {
		t.Errorf("app.go counts = +%d/-%d, want +1/-1", newest.Files[0].Added, newest.Files[0].Deleted)
	}
	if len(oldest.Hash) != 40 {
		t.Errorf("hash = %q", oldest.Hash)
	}
	if oldest.Author != "Test Author" {
		t.Errorf("author = %q", oldest.Author)
	}
	if oldest.Time.IsZero() || oldest.Time.After(time.Now()) {
		t.Errorf("time = %v", oldest.Time)
	}
	// Both commits are reachable from main, the primary branch here.
	for _, c := range commits {
		if c.RefState != fold.RefOnMain {
			t.Errorf("commit %s RefState = %v, want RefOnMain", c.Hash[:8], c.RefState)
		}
	}
}

func TestLoadRenameCollapsesToNewPath(t *testing.T) {
	dir := scratchRepo(t)
	writeFile(t, dir, "app.go", "package main\n")
	commit(t, dir, "add app", "")
	// Rename plus a small edit stays one numstat line under -M.
	gt(t, dir, "mv", "app.go", "main.go")
	writeFile(t, dir, "main.go", "package main\n\n// moved\n")
	commit(t, dir, "move app", "")

	commits, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	files := commits[0].Files
	if len(files) != 1 {
		t.Fatalf("rename commit files = %+v, want one entry", files)
	}
	if files[0].Path != "main.go" {
		t.Errorf("rename path = %q, want main.go (post-image)", files[0].Path)
	}
	if files[0].Added != 2 || files[0].Deleted != 0 {
		t.Errorf("rename counts = +%d/-%d, want +2/-0", files[0].Added, files[0].Deleted)
	}
}

func TestLoadBranchRefState(t *testing.T) {
	dir := scratchRepo(t)
	writeFile(t, dir, "a.txt", "1\n")
	commit(t, dir, "on main", "")
	gt(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "b.txt", "2\n")
	commit(t, dir, "on branch", "")

	commits, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	// HEAD is the feature branch, so git log sees both commits; only the
	// first is reachable from main.
	if commits[1].RefState != fold.RefOnMain {
		t.Errorf("main commit RefState = %v, want RefOnMain", commits[1].RefState)
	}
	if commits[0].RefState != fold.RefBranch {
		t.Errorf("branch commit RefState = %v, want RefBranch", commits[0].RefState)
	}
}

func TestLoadEmptyRepo(t *testing.T) {
	dir := scratchRepo(t)
	if _, err := Load(dir); err == nil {
		t.Fatal("Load on an empty repository should fail")
	}
}

func TestRenamePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain/file.go", "plain/file.go"},
		{"old.txt => new.txt", "new.txt"},
		{"dir/{old => new}/file.go", "dir/new/file.go"},
		{"a{b => c}d", "acd"},
		{"{only => renamed}", "renamed"},
	}
	for _, c := range cases {
		if got := renamePath(c.in); got != c.want {
			t.Errorf("renamePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseNumstat(t *testing.T) {
	files := parseNumstat("\n5\t2\tregular.go\n-\t-\tbinary.png\n0\t0\told => new\n")
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3: %+v", len(files), files)
	}
	if files[1].Path != "binary.png" || files[1].Added != 0 || files[1].Deleted != 0 {
		t.Errorf("binary file parsed as %+v", files[1])
	}
	if files[2].Path != "new" {
		t.Errorf("rename parsed as %+v", files[2])
	}
	if got := parseNumstat("not a numstat line"); len(got) != 0 {
		t.Errorf("junk parsed as %+v", got)
	}
}
