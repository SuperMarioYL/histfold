package fold

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/SuperMarioYL/histfold/internal/sessions"
)

// FileStat is one touched path with the line counts aggregated across the
// member commits of a changeset.
type FileStat struct {
	Path    string
	Added   int
	Deleted int
}

// RefState records what the repository's local refs say about a commit. The
// ingest layer sets it by checking reachability from the repo's primary
// branch (e.g. `git merge-base --is-ancestor <hash> main`); folding never
// touches git itself.
type RefState int

const (
	RefUnknown RefState = iota // no ref information was supplied
	RefBranch                  // not reachable from the primary local branch
	RefOnMain                  // reachable from the primary local branch
)

// Commit is the normalized commit model the fold engine consumes. Callers
// map their parsed git stream onto it; Message must be the full commit
// message so trailer-based attribution stays possible.
type Commit struct {
	Hash     string
	Author   string
	Message  string
	Time     time.Time
	Files    []FileStat
	RefState RefState
}

// Changeset is one folded task unit: a group of raw commits attributed to a
// single task, carrying the intent, the agent that did the work, the union
// of touched paths, and how trustworthy the attribution is. Commits are
// never mutated — a changeset is strictly a view over them.
type Changeset struct {
	ID           string // stable hash of the member commit IDs
	Intent       string // first user prompt of the matched session; else cluster-head subject
	Agent        string // canonical agent name; "" when no agent signal was found
	Model        string // model identifier from session metadata or trailers
	Commits      []Commit
	Files        []FileStat // union of touched paths across member commits
	SessionRef   string     // path of the matched session log; "" when trailer-only
	ReviewStatus string     // ReviewOnMain | ReviewBranch | ReviewUnknown
	Confidence   float64
}

// Review status values, derived from local refs only.
const (
	ReviewOnMain  = "on-main"
	ReviewBranch  = "branch"
	ReviewUnknown = "unknown"
)

// assembleChangeset builds one changeset from member commits: it orders the
// members chronologically, derives the stable ID, unions the touched files,
// derives the review status, and falls back to the cluster-head subject when
// no better intent was supplied.
func assembleChangeset(members []Commit, intent, agent, model, sessionRef string, conf float64) Changeset {
	sortCommits(members)
	hashes := make([]string, len(members))
	for i, c := range members {
		hashes[i] = c.Hash
	}
	if intent == "" && len(members) > 0 {
		intent = subject(members[0].Message)
	}
	return Changeset{
		ID:           changesetID(hashes),
		Intent:       intent,
		Agent:        agent,
		Model:        model,
		Commits:      members,
		Files:        unionFiles(members),
		SessionRef:   sessionRef,
		ReviewStatus: reviewStatus(members),
		Confidence:   conf,
	}
}

// sortCommits orders members chronologically, breaking ties by hash so the
// order is deterministic.
func sortCommits(commits []Commit) {
	sort.SliceStable(commits, func(i, j int) bool {
		if !commits[i].Time.Equal(commits[j].Time) {
			return commits[i].Time.Before(commits[j].Time)
		}
		return commits[i].Hash < commits[j].Hash
	})
}

// changesetID hashes the member commit IDs into a short, stable identifier.
// The hashes are sorted first so the ID depends only on the member set.
func changesetID(hashes []string) string {
	sorted := make([]string, len(hashes))
	copy(sorted, hashes)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:])[:12]
}

// unionFiles merges the touched paths of all members, summing line counts
// per path and sorting the result by path for stable output.
func unionFiles(commits []Commit) []FileStat {
	agg := make(map[string]*FileStat)
	var order []string
	for _, c := range commits {
		for _, f := range c.Files {
			cur, ok := agg[f.Path]
			if !ok {
				cur = &FileStat{Path: f.Path}
				agg[f.Path] = cur
				order = append(order, f.Path)
			}
			cur.Added += f.Added
			cur.Deleted += f.Deleted
		}
	}
	sort.Strings(order)
	out := make([]FileStat, 0, len(order))
	for _, p := range order {
		out = append(out, *agg[p])
	}
	return out
}

// reviewStatus folds the members' ref states into one status: fully merged
// members mean on-main, any unmerged member means branch, and no ref
// information at all means unknown.
func reviewStatus(commits []Commit) string {
	known, onMain := 0, 0
	for _, c := range commits {
		switch c.RefState {
		case RefOnMain:
			known++
			onMain++
		case RefBranch:
			known++
		}
	}
	switch {
	case known == 0:
		return ReviewUnknown
	case known == onMain:
		return ReviewOnMain
	default:
		return ReviewBranch
	}
}

// subject returns the first non-empty line of a commit message.
func subject(message string) string {
	for _, line := range strings.Split(message, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// provenance is a tiny helper that keeps the sessions import in one place:
// every commit's attribution is recomputed from its message at fold time so
// the input model stays dumb and folding policy lives only here.
func provenance(c Commit) sessions.Provenance {
	return sessions.DetectProvenance(c.Message)
}
