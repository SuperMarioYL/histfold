package fold

import (
	"reflect"
	"testing"
	"time"

	"github.com/SuperMarioYL/histfold/internal/sessions"
)

var base = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }

func commit(hash string, minutes int, msg string, paths ...string) Commit {
	c := Commit{Hash: hash, Author: "Leo", Message: msg, Time: at(minutes), RefState: RefOnMain}
	for _, p := range paths {
		c.Files = append(c.Files, FileStat{Path: p, Added: 10, Deleted: 2})
	}
	return c
}

func session(id, prompt string, from, to int, paths ...string) sessions.Session {
	s := sessions.Session{
		ID:          id,
		Path:        "/logs/" + id + ".jsonl",
		Agent:       "Claude",
		Model:       "claude-sonnet-4-5",
		FirstPrompt: prompt,
		Start:       at(from),
		End:         at(to),
	}
	s.Paths = paths
	return s
}

func findBySession(sets []Changeset, ref string) *Changeset {
	for i := range sets {
		if sets[i].SessionRef == ref {
			return &sets[i]
		}
	}
	return nil
}

func TestFoldSessionAttribution(t *testing.T) {
	commits := []Commit{
		commit("a1", 10, "wip", "internal/login.go"),
		commit("a2", 20, "still wip", "internal/login.go"),
		// Inside session B's window and touching session B's files.
		commit("b1", 90, "wip b", "internal/api.go"),
		// Far outside any session: human work.
		commit("h1", 200, "Fix typo in readme", "README.md"),
	}
	sess := []sessions.Session{
		session("s1", "Fix the login redirect bug", 5, 30, "internal/login.go", "internal/auth.go"),
		session("s2", "Add the api client", 85, 95, "internal/api.go"),
	}

	sets := Fold(commits, sess)
	if len(sets) != 3 {
		t.Fatalf("Fold() = %d changesets, want 3: %#v", len(sets), sets)
	}

	s1 := findBySession(sets, "/logs/s1.jsonl")
	if s1 == nil {
		t.Fatalf("no changeset for session s1: %#v", sets)
	}
	if s1.Intent != "Fix the login redirect bug" {
		t.Errorf("Intent = %q, want the session's first prompt", s1.Intent)
	}
	if s1.Agent != "Claude" || s1.Model != "claude-sonnet-4-5" {
		t.Errorf("Agent/Model = %q/%q, want Claude/claude-sonnet-4-5", s1.Agent, s1.Model)
	}
	if s1.Confidence != ConfSession {
		t.Errorf("Confidence = %v, want %v (paths overlap)", s1.Confidence, ConfSession)
	}
	if len(s1.Commits) != 2 || s1.Commits[0].Hash != "a1" || s1.Commits[1].Hash != "a2" {
		t.Errorf("Commits = %#v, want a1 then a2 in time order", s1.Commits)
	}
	if !reflect.DeepEqual(s1.Files, []FileStat{{Path: "internal/login.go", Added: 20, Deleted: 4}}) {
		t.Errorf("Files = %#v, want union with summed counts", s1.Files)
	}
	if s1.ReviewStatus != ReviewOnMain {
		t.Errorf("ReviewStatus = %q, want on-main", s1.ReviewStatus)
	}
	if s1.SessionRef != "/logs/s1.jsonl" {
		t.Errorf("SessionRef = %q, want the session log path", s1.SessionRef)
	}
}

func TestFoldSessionTieBreakOnPathOverlap(t *testing.T) {
	// Two overlapping windows contain c1; only s2 shares its files.
	commits := []Commit{commit("c1", 20, "wip", "internal/api.go")}
	sessions := []sessions.Session{
		session("s1", "broad task", 0, 60, "internal/ui.go"),
		session("s2", "api task", 10, 50, "internal/api.go"),
	}
	sets := Fold(commits, sessions)
	if len(sets) != 1 || sets[0].Intent != "api task" {
		t.Fatalf("Fold() = %#v, want one changeset attributed to s2", sets)
	}
	if sets[0].Confidence != ConfSession {
		t.Errorf("Confidence = %v, want %v", sets[0].Confidence, ConfSession)
	}
}

func TestFoldSessionTimeOnly(t *testing.T) {
	commits := []Commit{commit("c1", 20, "wip", "internal/other.go")}
	sessions := []sessions.Session{session("s1", "the task", 10, 30, "internal/login.go")}
	sets := Fold(commits, sessions)
	if len(sets) != 1 {
		t.Fatalf("Fold() = %d changesets, want 1", len(sets))
	}
	if sets[0].Confidence != ConfSessionTime {
		t.Errorf("Confidence = %v, want %v (time match, disjoint paths)", sets[0].Confidence, ConfSessionTime)
	}
}

func TestFoldTrailerOnly(t *testing.T) {
	agentMsg := "Add retry helper\n\nCo-Authored-By: Claude <noreply@anthropic.com>"
	commits := []Commit{
		commit("r1", 0, agentMsg, "a.go"),
		commit("r2", 5, agentMsg, "b.go"),
		// Human commit between agent commits: different class, own changeset.
		commit("m1", 6, "Fix readme typo", "README.md"),
		commit("r3", 8, agentMsg, "c.go"),
	}
	sets := Fold(commits, nil)
	if len(sets) != 3 {
		t.Fatalf("Fold() = %d changesets, want 3: %#v", len(sets), sets)
	}
	// The two interleaved runs of trailer-carrying commits split around the
	// human commit.
	total := 0
	for _, s := range sets {
		if s.Agent != "Claude" {
			if s.Confidence != ConfTimeGap || s.Intent != "Fix readme typo" || s.Agent != "" {
				t.Errorf("human changeset = %#v, want bare time-gap cluster", s)
			}
			continue
		}
		if s.Confidence != ConfTrailer {
			t.Errorf("Confidence = %v, want %v (trailer-only)", s.Confidence, ConfTrailer)
		}
		if s.SessionRef != "" {
			t.Errorf("SessionRef = %q, want empty without a session", s.SessionRef)
		}
		if s.Intent != "Add retry helper" {
			t.Errorf("Intent = %q, want cluster-head subject", s.Intent)
		}
		total += len(s.Commits)
	}
	if total != 3 {
		t.Errorf("trailer changesets hold %d commits, want 3", total)
	}
}

func TestFoldGapSplitsSameClass(t *testing.T) {
	commits := []Commit{
		commit("h1", 0, "Fix a", "a.go"),
		commit("h2", 10, "Fix b", "b.go"),
		// Two hours later: a different task despite the same class.
		commit("h3", 130, "Fix c", "c.go"),
	}
	sets := Fold(commits, nil)
	if len(sets) != 2 {
		t.Fatalf("Fold() = %d changesets, want 2: %#v", len(sets), sets)
	}
	for _, s := range sets {
		if s.Confidence != ConfTimeGap {
			t.Errorf("Confidence = %v, want %v", s.Confidence, ConfTimeGap)
		}
		if s.ReviewStatus != ReviewOnMain {
			t.Errorf("ReviewStatus = %q, want on-main", s.ReviewStatus)
		}
	}
}

func TestFoldUnknownRefState(t *testing.T) {
	c := commit("h1", 0, "Fix a", "a.go")
	c.RefState = RefUnknown
	sets := Fold([]Commit{c}, nil)
	if sets[0].ReviewStatus != ReviewUnknown {
		t.Errorf("ReviewStatus = %q, want unknown", sets[0].ReviewStatus)
	}
}

func TestFoldMixedRefState(t *testing.T) {
	onMain := commit("h1", 0, "Fix a", "a.go")
	branch := commit("h2", 1, "Fix b", "b.go")
	branch.RefState = RefBranch
	sets := Fold([]Commit{onMain, branch}, nil)
	if sets[0].ReviewStatus != ReviewBranch {
		t.Errorf("ReviewStatus = %q, want branch when any member is unmerged", sets[0].ReviewStatus)
	}
}

func TestChangesetIDStable(t *testing.T) {
	a := commit("aaa", 0, "Fix a", "a.go")
	b := commit("bbb", 1, "Fix b", "b.go")
	first := Fold([]Commit{a, b}, nil)
	// Same members in reverse input order must yield the same ID.
	second := Fold([]Commit{b, a}, nil)
	if first[0].ID != second[0].ID {
		t.Errorf("IDs differ for the same member set: %q vs %q", first[0].ID, second[0].ID)
	}
	if len(first[0].ID) != 12 {
		t.Errorf("ID = %q, want 12 hex chars", first[0].ID)
	}
	c := commit("ccc", 2, "Fix c", "c.go")
	third := Fold([]Commit{a, c}, nil)
	if third[0].ID == first[0].ID {
		t.Error("different member sets must yield different IDs")
	}
}

func TestFoldEveryCommitLandsExactlyOnce(t *testing.T) {
	agentMsg := "Refactor\n\nCo-Authored-By: Claude <noreply@anthropic.com>"
	commits := []Commit{
		commit("c1", 5, "wip", "internal/login.go"),
		commit("c2", 15, "wip", "internal/login.go"),
		commit("c3", 100, agentMsg, "util.go"),
		commit("c4", 101, agentMsg, "util.go"),
		commit("c5", 103, "Tidy docs", "docs.md"),
		commit("c6", 400, "Alone", "solo.go"),
	}
	sessions := []sessions.Session{session("s1", "Fix the login flow", 0, 20, "internal/login.go")}

	sets := Fold(commits, sessions)
	seen := make(map[string]int)
	for _, s := range sets {
		for _, c := range s.Commits {
			seen[c.Hash]++
		}
	}
	if len(seen) != len(commits) {
		t.Fatalf("only %d of %d commits landed in a changeset", len(seen), len(commits))
	}
	for hash, n := range seen {
		if n != 1 {
			t.Errorf("commit %s appears %d times", hash, n)
		}
	}
}

func TestFoldEmpty(t *testing.T) {
	if sets := Fold(nil, nil); len(sets) != 0 {
		t.Errorf("Fold(nil, nil) = %#v, want empty", sets)
	}
	// A session with no commits produces no changeset.
	if sets := Fold(nil, []sessions.Session{session("s1", "task", 0, 10)}); len(sets) != 0 {
		t.Errorf("Fold(nil, sessions) = %#v, want empty", sets)
	}
}
