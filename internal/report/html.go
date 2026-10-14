package report

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/SuperMarioYL/histfold/internal/assets"
	"github.com/SuperMarioYL/histfold/internal/fold"
)

// Meta carries optional context for the HTML report.
type Meta struct {
	// Repo is the repository name shown in the header; empty omits it.
	Repo string
}

// HTML writes the self-contained fold timeline: one collapsed card per
// changeset, expandable to its raw commits. Styles and script are embedded,
// so the output works offline in any browser with zero network requests.
func HTML(w io.Writer, sets []fold.Changeset, meta Meta) error {
	tpl, err := template.New("timeline").Parse(assets.Timeline)
	if err != nil {
		return err
	}
	return tpl.Execute(w, buildTimeline(sets, meta))
}

type timelinePage struct {
	Repo       string
	Generated  string
	Commits    int
	Changesets int
	Cards      []card
	ConfLegend string
}

type card struct {
	ID         string
	Intent     string
	Agent      string
	Model      string
	Review     string
	Confidence string
	ConfLevel  string
	SessionRef string
	When       string
	Commits    int
	Files      int
	Adds       int
	Dels       int
	FileRows   []fileRow
	CommitRows []commitRow
}

type fileRow struct {
	Path    string
	Added   int
	Deleted int
}

type commitRow struct {
	Hash    string
	Author  string
	When    string
	Subject string
	Adds    int
	Dels    int
}

// buildTimeline precomputes every display string so the template stays dumb.
func buildTimeline(sets []fold.Changeset, meta Meta) timelinePage {
	page := timelinePage{
		Repo:       meta.Repo,
		Generated:  time.Now().Format("2006-01-02 15:04"),
		Changesets: len(sets),
		Cards:      make([]card, 0, len(sets)),
		ConfLegend: fmt.Sprintf(
			"fold confidence: %.0f%% session+paths · %.0f%% session time · %.0f%% trailers · %.0f%% time gaps",
			fold.ConfSession*100, fold.ConfSessionTime*100, fold.ConfTrailer*100, fold.ConfTimeGap*100),
	}
	for _, s := range sets {
		c := card{
			ID:         s.ID,
			Intent:     truncate(collapse(s.Intent), 160),
			Agent:      s.Agent,
			Model:      s.Model,
			Review:     s.ReviewStatus,
			Confidence: fmt.Sprintf("%.0f%%", s.Confidence*100),
			ConfLevel:  confLevel(s.Confidence),
			SessionRef: s.SessionRef,
			Commits:    len(s.Commits),
			Files:      len(s.Files),
		}
		if len(s.Commits) > 0 {
			c.When = s.Commits[0].Time.Format("2006-01-02 15:04")
		}
		for _, f := range s.Files {
			c.Adds += f.Added
			c.Dels += f.Deleted
			c.FileRows = append(c.FileRows, fileRow{Path: f.Path, Added: f.Added, Deleted: f.Deleted})
		}
		for _, cm := range s.Commits {
			adds, dels := 0, 0
			for _, f := range cm.Files {
				adds += f.Added
				dels += f.Deleted
			}
			c.CommitRows = append(c.CommitRows, commitRow{
				Hash:    shortHash(cm.Hash),
				Author:  cm.Author,
				When:    cm.Time.Format("2006-01-02 15:04"),
				Subject: truncate(collapse(commitSubject(cm.Message)), 120),
				Adds:    adds,
				Dels:    dels,
			})
		}
		page.Commits += len(s.Commits)
		page.Cards = append(page.Cards, c)
	}
	return page
}

// confLevel buckets a confidence value for chip coloring.
func confLevel(c float64) string {
	switch {
	case c >= fold.ConfSession:
		return "high"
	case c >= fold.ConfTrailer:
		return "medium"
	default:
		return "low"
	}
}

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// commitSubject returns the first non-empty line of a commit message.
func commitSubject(message string) string {
	for _, line := range strings.Split(message, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}
