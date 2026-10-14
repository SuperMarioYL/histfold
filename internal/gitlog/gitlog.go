// Package gitlog reads a repository's commit stream by shelling out to the
// system git binary: one `git log --numstat` pass builds the normalized
// commits, one `git rev-list` pass marks what the primary branch contains.
// The repository is only ever read; nothing is written or modified.
package gitlog

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/SuperMarioYL/histfold/internal/fold"
)

// Field and record separators used in the git log format. These control
// characters do not appear in ordinary commit metadata, which keeps the
// stream parseable even when messages span many lines.
const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
)

// logFormat prints, per commit: record separator, hash, author, author date
// (strict ISO 8601), full message, then a field separator marking where the
// message ends and the numstat section begins.
const logFormat = recordSep + "%H" + fieldSep + "%an" + fieldSep + "%aI" + fieldSep + "%B" + fieldSep

// Load returns the normalized commit stream of the repository at repoRoot:
// every commit reachable from HEAD with its hash, author, full message,
// author timestamp, touched files (rename-collapsed onto the new path), and
// review status relative to the repository's primary branch.
func Load(repoRoot string) ([]fold.Commit, error) {
	out, err := git(repoRoot, "-c", "core.quotePath=false", "log",
		"--numstat", "-M", "--pretty=format:"+logFormat)
	if err != nil {
		return nil, fmt.Errorf("reading git history: %w", err)
	}
	commits := parse(out)
	markPrimaryBranch(repoRoot, commits)
	return commits, nil
}

// parse decodes the `git log --numstat` stream. Each record opens with the
// record separator, so the first split part is empty; malformed records are
// skipped rather than failing the whole history.
func parse(out string) []fold.Commit {
	records := strings.Split(out, recordSep)
	commits := make([]fold.Commit, 0, len(records))
	for _, rec := range records[1:] {
		rec = strings.TrimPrefix(rec, "\n")
		parts := strings.SplitN(rec, fieldSep, 5)
		if len(parts) < 5 || !isHash(parts[0]) {
			continue
		}
		t, err := time.Parse(time.RFC3339, parts[2])
		if err != nil {
			continue
		}
		commits = append(commits, fold.Commit{
			Hash:    parts[0],
			Author:  parts[1],
			Time:    t,
			Message: strings.TrimRight(parts[3], "\n"),
			Files:   parseNumstat(parts[4]),
		})
	}
	return commits
}

// isHash reports whether s looks like a full commit hash.
func isHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			continue
		}
		return false
	}
	return true
}

// parseNumstat decodes the `--numstat` section following one commit: lines
// of "added<TAB>deleted<TAB>path", with "-" for binary counts and git's
// rename notation for moved files. Anything that is not a numstat line is
// skipped, which keeps stray message text from corrupting the file list.
func parseNumstat(section string) []fold.FileStat {
	var files []fold.FileStat
	for _, line := range strings.Split(section, "\n") {
		added, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		deleted, path, ok := strings.Cut(rest, "\t")
		if !ok {
			continue
		}
		a, ok1 := parseCount(added)
		d, ok2 := parseCount(deleted)
		if !ok1 || !ok2 {
			continue
		}
		files = append(files, fold.FileStat{Path: renamePath(path), Added: a, Deleted: d})
	}
	return files
}

// parseCount reads one numstat count; "-" (binary files) counts as zero.
func parseCount(s string) (int, bool) {
	if s == "-" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// renamePath resolves git's rename notation to the file's post-image path:
// "old => new" is a plain rename, "{old => new}/suffix" rewrites one path
// component. The new path is what sessions and later commits refer to, so
// line counts are attributed there.
func renamePath(p string) string {
	if i := sepOutsideBraces(p); i >= 0 {
		return p[i+len(" => "):]
	}
	open := strings.IndexByte(p, '{')
	clos := strings.LastIndexByte(p, '}')
	arrow := strings.Index(p, " => ")
	if open < 0 || clos < open || arrow < open || arrow+len(" => ") > clos {
		return p
	}
	return p[:open] + p[arrow+len(" => "):clos] + p[clos+1:]
}

// sepOutsideBraces returns the index of the " => " separator at brace depth
// zero, or -1. A depth-zero separator means the whole path was renamed.
func sepOutsideBraces(p string) int {
	depth := 0
	for i := 0; i+4 <= len(p); i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ' ':
			if depth == 0 && p[i:i+4] == " => " {
				return i
			}
		}
	}
	return -1
}

// markPrimaryBranch sets each commit's RefState from the repository's
// primary branch: reachable means merged (on-main), everything else counts
// as living on a branch — the same answer `git merge-base --is-ancestor`
// gives per commit, computed with one `git rev-list` pass. Without a primary
// branch, or if git disagrees, ref information stays unknown: that is a soft
// failure, not a fold failure.
func markPrimaryBranch(repoRoot string, commits []fold.Commit) {
	branch, ok := primaryBranch(repoRoot)
	if !ok {
		return
	}
	out, err := git(repoRoot, "rev-list", branch)
	if err != nil {
		return
	}
	reachable := make(map[string]struct{})
	for _, h := range strings.Fields(out) {
		reachable[h] = struct{}{}
	}
	for i := range commits {
		if _, onMain := reachable[commits[i].Hash]; onMain {
			commits[i].RefState = fold.RefOnMain
		} else {
			commits[i].RefState = fold.RefBranch
		}
	}
}

// primaryBranch picks the repository's primary branch: main, then master,
// then the current branch when HEAD names one.
func primaryBranch(repoRoot string) (string, bool) {
	for _, ref := range []string{"main", "master"} {
		if _, err := git(repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+ref); err == nil {
			return ref, true
		}
	}
	out, err := git(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil {
		if name := strings.TrimSpace(out); name != "" && name != "HEAD" {
			return name, true
		}
	}
	return "", false
}

// git runs one git command in dir and returns its stdout. Stderr (trimmed)
// is the error text, so failures read like git's own diagnostics.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}
