package fold

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/SuperMarioYL/histfold/internal/sessions"
)

// Confidence tiers, in decreasing signal strength. A changeset carries the
// confidence of the weakest evidence used to attribute it.
const (
	// ConfSession: the commit falls inside a session's activity window and
	// its touched paths overlap the session's.
	ConfSession = 0.9
	// ConfSessionTime: the commit falls inside a session's window but the
	// paths could not confirm it (unknown on either side, or disjoint).
	ConfSessionTime = 0.7
	// ConfTrailer: agent attribution comes from commit trailers only
	// (Co-Authored-By, Agent:, generated-with footers), no session matched.
	ConfTrailer = 0.5
	// ConfTimeGap: the changeset is a pure time-gap cluster with no agent
	// signal at all.
	ConfTimeGap = 0.2
)

// Folding policy constants. A commit belongs to a session when it happens
// within the session's recorded span, padded because the final commit often
// lands shortly after the last log line (and the first shortly before the
// first). Unmatched commits are clustered while they stay close in time and
// share one provenance class.
const (
	sessionWindowBefore = 5 * time.Minute
	sessionWindowAfter  = 15 * time.Minute
	gapThreshold        = 30 * time.Minute
)

// Fold correlates the raw commit stream with agent session logs and commit
// trailers, folding commits into task-attributed changesets.
//
// Three independent signals are used, strongest first:
//
//  1. Sessions: a commit inside a session's activity window is attributed to
//     that session (ConfSession with touched-path overlap, ConfSessionTime
//     otherwise). One session yields at most one changeset.
//  2. Trailers: commits whose message carries agent provenance are clustered
//     by time and agent (ConfTrailer).
//  3. Time gaps: whatever remains is clustered while gaps stay small
//     (ConfTimeGap).
//
// Commits may arrive in any order; every commit lands in exactly one
// changeset. The result is ordered chronologically by first commit.
func Fold(commits []Commit, sessions []sessions.Session) []Changeset {
	sorted := make([]Commit, len(commits))
	copy(sorted, commits)
	sortCommits(sorted)

	sets := make([]Changeset, 0)
	claimed := foldSessions(sorted, sessions, &sets)
	foldRemaining(sorted, claimed, &sets)

	sort.SliceStable(sets, func(i, j int) bool {
		ti, tj := firstTime(sets[i]), firstTime(sets[j])
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return sets[i].ID < sets[j].ID
	})
	return sets
}

// sessionMatch is one session's claim on a commit: how strongly (confidence),
// how specifically (window span), and via how many shared paths.
type sessionMatch struct {
	session int
	overlap int
	span    time.Duration
	conf    float64
}

// foldSessions attributes commits to sessions and appends one changeset per
// matched session. claimed[i] marks commits consumed this way.
func foldSessions(sorted []Commit, sessions []sessions.Session, sets *[]Changeset) []bool {
	claimed := make([]bool, len(sorted))
	assign := make([]sessionMatch, len(sorted))
	for i, c := range sorted {
		assign[i] = bestSession(c, sessions)
		if assign[i].session >= 0 {
			claimed[i] = true
		}
	}
	members := make([][]Commit, len(sessions))
	confs := make([][]float64, len(sessions))
	for i, c := range sorted {
		m := assign[i]
		if m.session < 0 {
			continue
		}
		members[m.session] = append(members[m.session], c)
		confs[m.session] = append(confs[m.session], m.conf)
	}
	for si, mem := range members {
		if len(mem) == 0 {
			continue
		}
		s := sessions[si]
		// A changeset is only as trustworthy as its weakest attribution.
		conf := confs[si][0]
		for _, c := range confs[si] {
			if c < conf {
				conf = c
			}
		}
		*sets = append(*sets, assembleChangeset(mem, s.FirstPrompt, s.Agent, s.Model, s.Path, conf))
	}
	return claimed
}

// bestSession picks the session with the strongest claim on a commit: most
// touched-path overlap first, then the narrowest window (the more specific
// claim), then log order for determinism.
func bestSession(c Commit, sessions []sessions.Session) sessionMatch {
	best := sessionMatch{session: -1}
	for si, s := range sessions {
		conf, overlap, ok := matchSession(c, s)
		if !ok {
			continue
		}
		span := s.End.Sub(s.Start)
		if best.session < 0 ||
			overlap > best.overlap ||
			(overlap == best.overlap && span < best.span) {
			best = sessionMatch{session: si, overlap: overlap, span: span, conf: conf}
		}
	}
	return best
}

// matchSession reports whether a commit falls inside a session's padded
// activity window, and with how much touched-path overlap.
func matchSession(c Commit, s sessions.Session) (conf float64, overlap int, ok bool) {
	if s.Start.IsZero() && s.End.IsZero() {
		return 0, 0, false
	}
	start := s.Start.Add(-sessionWindowBefore)
	end := s.End.Add(sessionWindowAfter)
	if c.Time.Before(start) || c.Time.After(end) {
		return 0, 0, false
	}
	overlap = pathOverlap(c.Files, s.Paths)
	if overlap > 0 {
		return ConfSession, overlap, true
	}
	return ConfSessionTime, 0, true
}

// pathOverlap counts the commit's files that also appear in the session's
// touched paths. Both sides are cleaned so absolute/relative spelling does
// not hide a match.
func pathOverlap(files []FileStat, sessionPaths []string) int {
	if len(files) == 0 || len(sessionPaths) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(sessionPaths))
	for _, p := range sessionPaths {
		set[filepath.Clean(p)] = struct{}{}
	}
	n := 0
	for _, f := range files {
		if _, ok := set[filepath.Clean(f.Path)]; ok {
			n++
		}
	}
	return n
}

// foldRemaining clusters the commits no session claimed: runs of commits
// that stay close in time and share one provenance class (same agent, or all
// human). A run whose class names an agent is trailer-attributed; the rest
// are pure time-gap clusters.
func foldRemaining(sorted []Commit, claimed []bool, sets *[]Changeset) {
	var run []Commit
	runClass := ""
	var last time.Time
	flush := func() {
		if len(run) > 0 {
			*sets = append(*sets, trailerChangeset(run))
			run = nil
			runClass = ""
		}
	}
	for i, c := range sorted {
		if claimed[i] {
			flush()
			continue
		}
		class := provenance(c).Agent
		if len(run) > 0 && (class != runClass || c.Time.Sub(last) > gapThreshold) {
			flush()
		}
		if len(run) == 0 {
			runClass = class
		}
		run = append(run, c)
		last = c.Time
	}
	flush()
}

// trailerChangeset assembles one run of session-unmatched commits. The agent
// attribution comes from the first member with provenance; a run with an
// agent is trailer-only (0.5), a run without any agent signal is a bare
// time-gap cluster (0.2).
func trailerChangeset(members []Commit) Changeset {
	agent, model := "", ""
	conf := ConfTimeGap
	for _, c := range members {
		if p := provenance(c); p.Agent != "" {
			agent, model = p.Agent, p.Model
			conf = ConfTrailer
			break
		}
	}
	// Empty intent: assembleChangeset falls back to the cluster-head subject.
	return assembleChangeset(members, "", agent, model, "", conf)
}

func firstTime(s Changeset) time.Time {
	if len(s.Commits) == 0 {
		return time.Time{}
	}
	return s.Commits[0].Time
}
