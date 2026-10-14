// Package sessions adapts agent provenance sources — commit trailers,
// Co-Authored-By lines, and (later) agent session logs — into the signals
// the fold engine correlates.
package sessions

import (
	"regexp"
	"strings"
	"unicode"
)

// Trailer is a single token/value trailer parsed from a commit message,
// following git's trailer convention: "Token: value" lines at the end of
// the message.
type Trailer struct {
	Token string
	Value string
}

// Identity is a person or agent named by a Co-Authored-By line.
type Identity struct {
	Name    string // display name, "" when the line carried only an email
	Email   string // email address, "" when the line carried only a name
	IsAgent bool   // true when the identity matches a known coding-agent signature
}

// How Provenance.Agent was determined. Source is "" when no agent was found.
const (
	SourceTrailer      = "trailer"        // explicit agent trailer (Agent:, Agent-Name:, Generated-By:, ...)
	SourceCoAuthoredBy = "co-authored-by" // Co-Authored-By line naming a known agent
	SourceFooter       = "footer"         // "Generated with/by <agent>" footer line
)

// Provenance is the agent attribution extracted from one commit message.
// Agent is the canonical agent name and Model the stated model identifier,
// both "" when the message carries no agent signal.
type Provenance struct {
	Agent     string
	Model     string
	Source    string
	CoAuthors []Identity // every Co-Authored-By identity, in message order
	Trailers  []Trailer  // the parsed trailer block, in message order
}

var (
	coAuthoredByRe = regexp.MustCompile(`(?i)^\s*co-authored-by\s*:\s*(.+)$`)
	footerRe       = regexp.MustCompile(`(?i)^[^\p{L}\p{N}]*generated (?:with|by)[ \t]*(?::[ \t]*)?(.+)$`)
	urlRe          = regexp.MustCompile(`(?i)https?://\S+`)
	identityRe     = regexp.MustCompile(`^\s*(.*?)\s*<\s*(.*?)\s*>\s*$`)
	parenRe        = regexp.MustCompile(`\(([^)]+)\)`)
)

// ParseTrailers returns the trailer block of a commit message under the
// conservative subset of git's trailer rules: the block is a contiguous run
// of "Token: value" lines at the end of the message that either makes up the
// whole message or is separated from the body by a blank line. It returns
// nil when no such block exists.
func ParseTrailers(message string) []Trailer {
	lines := strings.Split(message, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	start := end
	for start > 0 && parseTrailerLine(lines[start-1]) != nil {
		start--
	}
	if start == end {
		return nil // no trailer lines at the end of the message
	}
	if start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		return nil // not separated from the body by a blank line
	}
	trailers := make([]Trailer, 0, end-start)
	for _, line := range lines[start:end] {
		trailers = append(trailers, *parseTrailerLine(line))
	}
	return trailers
}

// parseTrailerLine splits one line into a Trailer when it has the
// "Token: value" shape with an alphanumeric/hyphen token, and returns nil
// otherwise.
func parseTrailerLine(line string) *Trailer {
	colon := strings.Index(line, ":")
	if colon <= 0 {
		return nil
	}
	token := strings.TrimSpace(line[:colon])
	if token == "" {
		return nil
	}
	for _, r := range token {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return nil
	}
	return &Trailer{Token: token, Value: strings.TrimSpace(line[colon+1:])}
}

// DetectProvenance extracts coding-agent attribution from a raw commit
// message. Co-Authored-By lines are scanned across the whole message because
// tools do not always emit a well-formed trailer block. An explicit agent
// trailer wins over a co-author line, which wins over a generated-with
// footer; unknown identities are reported as humans rather than guessed at.
func DetectProvenance(message string) Provenance {
	prov := Provenance{Trailers: ParseTrailers(message)}

	// Explicit agent and model trailers carry the strongest signal.
	for _, t := range prov.Trailers {
		switch strings.ToLower(t.Token) {
		case "agent", "agent-name":
			if name := canonicalAgentName(t.Value); name != "" {
				prov.Agent, prov.Source = name, SourceTrailer
			} else if t.Value != "" {
				// A deliberate Agent declaration names the agent even when
				// it is not in the known-signature catalog.
				prov.Agent, prov.Source = t.Value, SourceTrailer
			}
		case "agent-model", "co-authored-model":
			if prov.Model == "" && t.Value != "" {
				prov.Model = t.Value
			}
		case "generated-by", "assisted-by":
			// These may credit humans too, so only known signatures count.
			if name := canonicalAgentName(t.Value); name != "" {
				prov.Agent, prov.Source = name, SourceTrailer
			}
		}
	}

	// Co-Authored-By: collect every identity, then let the first known
	// agent provide attribution when no trailer already did.
	for _, line := range strings.Split(message, "\n") {
		m := coAuthoredByRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := parseIdentity(m[1])
		id.IsAgent = matchAgent(id) != ""
		prov.CoAuthors = append(prov.CoAuthors, id)
	}
	if prov.Agent == "" {
		for _, id := range prov.CoAuthors {
			if name := matchAgent(id); name != "" {
				prov.Agent, prov.Source = name, SourceCoAuthoredBy
				if prov.Model == "" {
					prov.Model = parenModel(id.Name)
				}
				break
			}
		}
	}

	// Generated-with footers (e.g. Claude Code's) are the weakest signal.
	if prov.Agent == "" {
		for _, line := range strings.Split(message, "\n") {
			m := footerRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			text := urlRe.ReplaceAllString(m[1], " ")
			if name := canonicalAgentName(text); name != "" {
				prov.Agent, prov.Source = name, SourceFooter
				break
			}
		}
	}

	return prov
}

// parseIdentity parses a Co-Authored-By value in git's "Name <email>" form;
// a bare name or a bare "<email>" is also accepted.
func parseIdentity(value string) Identity {
	value = strings.TrimSpace(value)
	if m := identityRe.FindStringSubmatch(value); m != nil {
		return Identity{Name: strings.TrimSpace(m[1]), Email: strings.TrimSpace(m[2])}
	}
	return Identity{Name: value}
}

// canonicalAgentName maps free text to a known agent's canonical name,
// returning "" when no known signature matches.
func canonicalAgentName(text string) string {
	return matchAgent(parseIdentity(text))
}

// parenModel extracts a model identifier from a parenthetical in an identity
// name, e.g. "aider (gpt-4o)" -> "gpt-4o".
func parenModel(name string) string {
	m := parenRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

type agentSignature struct {
	Canonical   string
	NameTokens  []string // matched against word tokens of an identity name
	Domains     []string // exact matches for the email domain
	EmailTokens []string // matched against word tokens of the email local part
}

// agentSignatures is the catalog of coding agents histfold recognizes from
// commit provenance. Domain matches assume agent tooling owns its vendor
// domain; humans at those domains are rare enough that the tradeoff is fine.
var agentSignatures = []agentSignature{
	{Canonical: "Claude", NameTokens: []string{"claude"}, Domains: []string{"anthropic.com", "claude.ai"}, EmailTokens: []string{"claude"}},
	{Canonical: "GitHub Copilot", NameTokens: []string{"copilot"}, EmailTokens: []string{"copilot"}},
	{Canonical: "Codex", NameTokens: []string{"codex"}, Domains: []string{"openai.com"}, EmailTokens: []string{"codex"}},
	{Canonical: "Gemini", NameTokens: []string{"gemini"}, EmailTokens: []string{"gemini"}},
	{Canonical: "Cursor", NameTokens: []string{"cursor"}, Domains: []string{"cursor.sh", "cursor.com", "anysphere.inc"}},
	{Canonical: "Devin", NameTokens: []string{"devin"}, Domains: []string{"cognition.ai", "devin.ai"}, EmailTokens: []string{"devin"}},
	{Canonical: "Aider", NameTokens: []string{"aider"}, Domains: []string{"aider.chat"}, EmailTokens: []string{"aider"}},
	{Canonical: "OpenHands", NameTokens: []string{"openhands"}, Domains: []string{"all-hands.dev"}, EmailTokens: []string{"openhands"}},
}

// matchAgent reports the canonical agent name for id, or "" when the
// identity does not match any known agent signature.
func matchAgent(id Identity) string {
	nameToks := tokenize(id.Name)
	for _, sig := range agentSignatures {
		if containsAny(nameToks, sig.NameTokens) {
			return sig.Canonical
		}
	}
	if id.Email == "" {
		return ""
	}
	local, domain, _ := strings.Cut(id.Email, "@")
	for _, sig := range agentSignatures {
		if containsAny([]string{domain}, sig.Domains) {
			return sig.Canonical
		}
	}
	localToks := tokenize(local)
	for _, sig := range agentSignatures {
		if containsAny(localToks, sig.EmailTokens) {
			return sig.Canonical
		}
	}
	return ""
}

// tokenize lowercases s and splits it into alphanumeric word tokens.
func tokenize(s string) []string {
	var toks []string
	var cur strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks
}

func containsAny(tokens, targets []string) bool {
	for _, t := range tokens {
		for _, target := range targets {
			if t == target {
				return true
			}
		}
	}
	return false
}
