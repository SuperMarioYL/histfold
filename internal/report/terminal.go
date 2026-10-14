// Package report renders folded changesets for people and for scripts: a
// terminal table, a self-contained HTML timeline, and JSON export. All three
// are pure views over the same []fold.Changeset.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/SuperMarioYL/histfold/internal/fold"
)

// WriteJSON emits changesets as pretty-printed JSON for scripts and
// pipelines (`histfold fold --json`). Commit messages are included in full.
func WriteJSON(w io.Writer, sets []fold.Changeset) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(sets)
}

// Terminal prints the folded summary: a headline counting commits and
// changesets, then one row per changeset with task intent, agent and model,
// files touched, review status, and fold confidence.
func Terminal(w io.Writer, sets []fold.Changeset) error {
	commits, files, adds, dels := 0, 0, 0, 0
	for _, s := range sets {
		commits += len(s.Commits)
		for _, f := range s.Files {
			files++
			adds += f.Added
			dels += f.Deleted
		}
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%d commits → %d changesets\n", commits, len(sets))
	if files > 0 {
		fmt.Fprintf(tw, "%d files touched, +%d/-%d\n", files, adds, dels)
	}
	fmt.Fprintln(tw)
	fmt.Fprintln(tw, "INTENT\tAGENT\tMODEL\tFILES\tREVIEW\tCONF\tCOMMITS")
	for _, s := range sets {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%.0f%%\t%d\n",
			truncate(collapse(s.Intent), 48),
			s.Agent,
			s.Model,
			len(s.Files),
			s.ReviewStatus,
			s.Confidence*100,
			len(s.Commits),
		)
	}
	fmt.Fprintln(tw)
	fmt.Fprintf(tw,
		"confidence: %.0f%% session+paths · %.0f%% session time · %.0f%% trailers · %.0f%% time gaps\n",
		fold.ConfSession*100, fold.ConfSessionTime*100, fold.ConfTrailer*100, fold.ConfTimeGap*100)
	return tw.Flush()
}

// collapse flattens any whitespace run to a single space, so multi-line
// intents stay on one table row.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate cuts a string to max runes, appending an ellipsis when it had to.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
