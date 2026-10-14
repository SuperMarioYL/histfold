package sessions

import (
	"reflect"
	"testing"
)

func TestParseTrailers(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    []Trailer
	}{
		{
			name:    "block separated from body by blank line",
			message: "Fix the login redirect\n\nSigned-off-by: Leo <leo@example.com>\nCo-Authored-By: Claude <noreply@anthropic.com>",
			want: []Trailer{
				{Token: "Signed-off-by", Value: "Leo <leo@example.com>"},
				{Token: "Co-Authored-By", Value: "Claude <noreply@anthropic.com>"},
			},
		},
		{
			name:    "whole message is the block",
			message: "Signed-off-by: Leo <leo@example.com>",
			want:    []Trailer{{Token: "Signed-off-by", Value: "Leo <leo@example.com>"}},
		},
		{
			name:    "no blank line before block is rejected",
			message: "Fix the login redirect\nSigned-off-by: Leo <leo@example.com>",
			want:    nil,
		},
		{
			name:    "trailing blank lines are tolerated",
			message: "Fix the login redirect\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n\n",
			want:    []Trailer{{Token: "Co-Authored-By", Value: "Claude <noreply@anthropic.com>"}},
		},
		{
			name:    "non-trailer line after the block is rejected",
			message: "Fix the login redirect\n\nSigned-off-by: Leo <leo@example.com>\nthanks everyone",
			want:    nil,
		},
		{
			name:    "body line containing a colon is not a trailer",
			message: "Fix the login redirect\n\nSee https://example.com/docs for details",
			want:    nil,
		},
		{
			name:    "hyphenated tokens are accepted",
			message: "Checkpoint work\n\nEntire-Checkpoint: abc123",
			want:    []Trailer{{Token: "Entire-Checkpoint", Value: "abc123"}},
		},
		{
			name:    "empty message has no block",
			message: "",
			want:    nil,
		},
		{
			name:    "blank message has no block",
			message: "  \n\n",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTrailers(tt.message)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseTrailers() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDetectProvenance(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    Provenance
	}{
		{
			name: "claude code footer plus co-authored-by",
			message: "Fix the login redirect\n\n" +
				"\U0001F916 Generated with [Claude Code](https://claude.com/claude-code)\n\n" +
				"Co-Authored-By: Claude <noreply@anthropic.com>",
			want: Provenance{
				Agent:     "Claude",
				Source:    SourceCoAuthoredBy,
				CoAuthors: []Identity{{Name: "Claude", Email: "noreply@anthropic.com", IsAgent: true}},
				Trailers:  []Trailer{{Token: "Co-Authored-By", Value: "Claude <noreply@anthropic.com>"}},
			},
		},
		{
			name:    "copilot co-author without trailer block",
			message: "Add retry helper\nCo-authored-by: Copilot <198982749+Copilot@users.noreply.github.com>",
			want: Provenance{
				Agent:     "GitHub Copilot",
				Source:    SourceCoAuthoredBy,
				CoAuthors: []Identity{{Name: "Copilot", Email: "198982749+Copilot@users.noreply.github.com", IsAgent: true}},
			},
		},
		{
			name:    "aider model parenthetical",
			message: "Add csv export\n\nCo-authored-by: aider (gpt-4o) <noreply@aider.chat>",
			want: Provenance{
				Agent:  "Aider",
				Model:  "gpt-4o",
				Source: SourceCoAuthoredBy,
				CoAuthors: []Identity{
					{Name: "aider (gpt-4o)", Email: "noreply@aider.chat", IsAgent: true},
				},
				Trailers: []Trailer{
					{Token: "Co-authored-by", Value: "aider (gpt-4o) <noreply@aider.chat>"},
				},
			},
		},
		{
			name:    "explicit agent and model trailers",
			message: "Migrate auth to oauth2\n\nAgent: codex\nAgent-Model: o4-mini",
			want: Provenance{
				Agent:    "Codex",
				Model:    "o4-mini",
				Source:   SourceTrailer,
				Trailers: []Trailer{{Token: "Agent", Value: "codex"}, {Token: "Agent-Model", Value: "o4-mini"}},
			},
		},
		{
			name:    "unknown agent declaration is kept verbatim",
			message: "Run migrations\n\nAgent: terragrunt-bot",
			want: Provenance{
				Agent:    "terragrunt-bot",
				Source:   SourceTrailer,
				Trailers: []Trailer{{Token: "Agent", Value: "terragrunt-bot"}},
			},
		},
		{
			name:    "model trailer alongside co-authored-by",
			message: "Refactor parser\n\nCo-Authored-By: Claude <noreply@anthropic.com>\nCo-Authored-Model: claude-sonnet-4-5",
			want: Provenance{
				Agent:  "Claude",
				Model:  "claude-sonnet-4-5",
				Source: SourceCoAuthoredBy,
				CoAuthors: []Identity{
					{Name: "Claude", Email: "noreply@anthropic.com", IsAgent: true},
				},
				Trailers: []Trailer{
					{Token: "Co-Authored-By", Value: "Claude <noreply@anthropic.com>"},
					{Token: "Co-Authored-Model", Value: "claude-sonnet-4-5"},
				},
			},
		},
		{
			name:    "human co-author only",
			message: "Pair work on the parser\n\nCo-authored-by: Sam Lee <sam@example.com>",
			want: Provenance{
				CoAuthors: []Identity{{Name: "Sam Lee", Email: "sam@example.com"}},
				Trailers:  []Trailer{{Token: "Co-authored-by", Value: "Sam Lee <sam@example.com>"}},
			},
		},
		{
			name:    "human co-author then agent co-author",
			message: "Co-authored-by: Sam Lee <sam@example.com>\nCo-authored-by: Claude <noreply@anthropic.com>",
			want: Provenance{
				Agent:  "Claude",
				Source: SourceCoAuthoredBy,
				CoAuthors: []Identity{
					{Name: "Sam Lee", Email: "sam@example.com"},
					{Name: "Claude", Email: "noreply@anthropic.com", IsAgent: true},
				},
				Trailers: []Trailer{
					{Token: "Co-authored-by", Value: "Sam Lee <sam@example.com>"},
					{Token: "Co-authored-by", Value: "Claude <noreply@anthropic.com>"},
				},
			},
		},
		{
			name:    "footer only",
			message: "Fix flaky test\n\n\U0001F916 Generated with Codex",
			want: Provenance{
				Agent:  "Codex",
				Source: SourceFooter,
			},
		},
		{
			name:    "footer URL does not leak into agent detection",
			message: "Bump dependencies\n\nGenerated with [Something](https://claude.ai/collect)",
			want:    Provenance{},
		},
		{
			name:    "user-agent trailer is not an agent",
			message: "Document the api\n\nUser-Agent: curl/8.1",
			want: Provenance{
				Trailers: []Trailer{{Token: "User-Agent", Value: "curl/8.1"}},
			},
		},
		{
			name:    "uppercase co-authored-by token",
			message: "Draft the docs\n\nCO-AUTHORED-BY: Gemini <gemini-cli@google.com>",
			want: Provenance{
				Agent:     "Gemini",
				Source:    SourceCoAuthoredBy,
				CoAuthors: []Identity{{Name: "Gemini", Email: "gemini-cli@google.com", IsAgent: true}},
				Trailers:  []Trailer{{Token: "CO-AUTHORED-BY", Value: "Gemini <gemini-cli@google.com>"}},
			},
		},
		{
			name:    "human github noreply email is not an agent",
			message: "Tidy imports\n\nCo-authored-by: Leo <12345+leo@users.noreply.github.com>",
			want: Provenance{
				CoAuthors: []Identity{{Name: "Leo", Email: "12345+leo@users.noreply.github.com"}},
				Trailers:  []Trailer{{Token: "Co-authored-by", Value: "Leo <12345+leo@users.noreply.github.com>"}},
			},
		},
		{
			name:    "empty message",
			message: "",
			want:    Provenance{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectProvenance(tt.message)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DetectProvenance() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseIdentity(t *testing.T) {
	tests := []struct {
		value string
		want  Identity
	}{
		{value: "Claude <noreply@anthropic.com>", want: Identity{Name: "Claude", Email: "noreply@anthropic.com"}},
		{value: "<devin@cognition.ai>", want: Identity{Email: "devin@cognition.ai"}},
		{value: "Sam Lee", want: Identity{Name: "Sam Lee"}},
		{value: "  aider (gpt-4o) <noreply@aider.chat>  ", want: Identity{Name: "aider (gpt-4o)", Email: "noreply@aider.chat"}},
	}
	for _, tt := range tests {
		if got := parseIdentity(tt.value); got != tt.want {
			t.Errorf("parseIdentity(%q) = %#v, want %#v", tt.value, got, tt.want)
		}
	}
}
