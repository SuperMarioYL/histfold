// Command histfold folds agent-era git history into reviewable task
// changesets. It reads `git log` and local Claude Code session logs, folds
// the raw commit stream with a pure correlation engine, and renders a
// terminal summary plus a self-contained HTML timeline. The repository and
// the session logs are read-only throughout.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/SuperMarioYL/histfold/internal/fold"
	"github.com/SuperMarioYL/histfold/internal/gitlog"
	"github.com/SuperMarioYL/histfold/internal/report"
	"github.com/SuperMarioYL/histfold/internal/sessions"
)

//go:embed VERSION
var versionFile string

var version = strings.TrimSpace(versionFile)

const usageText = `histfold folds agent-era git history into reviewable task changesets.

Usage:

  histfold <command> [flags]

Commands:

  fold      fold the commit stream into task changesets: prints a terminal
            summary and writes a self-contained histfold.html timeline
  commits   print the normalized commit stream with agent provenance
  version   print the histfold version

Run "histfold <command> -h" for the flags of a command.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "histfold: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	switch args[0] {
	case "fold":
		return runFold(args[1:])
	case "commits":
		return runCommits(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("histfold %s\n", version)
		return nil
	case "help", "--help", "-h":
		fmt.Print(usageText)
		return nil
	default:
		fmt.Fprintf(os.Stderr, "histfold: unknown command %q\n\n", args[0])
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
		return nil
	}
}

// runFold folds the current repository: git log in, session discovery in,
// changesets out as a terminal table (or JSON) plus a self-contained HTML
// timeline next to the repository's working copy.
func runFold(args []string) error {
	fs := flag.NewFlagSet("fold", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print changesets as JSON instead of the terminal table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("fold takes no arguments; got %q", fs.Arg(0))
	}

	repo, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	commits, err := gitlog.Load(".")
	if err != nil {
		return err
	}
	ss, err := sessions.DiscoverClaudeSessions(".")
	if err != nil {
		return fmt.Errorf("discovering Claude session logs: %w", err)
	}
	sets := fold.Fold(commits, ss)

	if *asJSON {
		if err := report.WriteJSON(os.Stdout, sets); err != nil {
			return err
		}
	} else if err := report.Terminal(os.Stdout, sets); err != nil {
		return err
	}

	html := filepath.Join(repo, "histfold.html")
	if err := writeTimeline(html, sets, filepath.Base(repo)); err != nil {
		return fmt.Errorf("writing %s: %w", html, err)
	}
	if !*asJSON {
		fmt.Printf("\nwrote %s — open it in a browser to browse the timeline\n", html)
	}
	return nil
}

// writeTimeline renders the foldable HTML timeline to path.
func writeTimeline(path string, sets []fold.Changeset, repo string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := report.HTML(f, sets, report.Meta{Repo: repo}); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// runCommits prints the normalized commit stream: hash, timestamp, author,
// touched files, and the agent provenance detected in each message.
func runCommits(args []string) error {
	fs := flag.NewFlagSet("commits", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("commits takes no arguments; got %q", fs.Arg(0))
	}
	commits, err := gitlog.Load(".")
	if err != nil {
		return err
	}
	return printCommits(os.Stdout, commits)
}

// printCommits renders the commit stream as a fixed-column table.
func printCommits(w io.Writer, commits []fold.Commit) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "HASH\tTIME\tAUTHOR\tFILES\t+/-\tAGENT\tVIA")
	for _, c := range commits {
		agent, via := "-", "-"
		if prov := sessions.DetectProvenance(c.Message); prov.Agent != "" {
			agent, via = prov.Agent, prov.Source
		}
		files, adds, dels := 0, 0, 0
		for _, f := range c.Files {
			files++
			adds += f.Added
			dels += f.Deleted
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t+%d/-%d\t%s\t%s\n",
			shortHash(c.Hash),
			c.Time.Format("2006-01-02 15:04"),
			c.Author,
			files,
			adds, dels,
			agent, via,
		)
	}
	return tw.Flush()
}

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}
