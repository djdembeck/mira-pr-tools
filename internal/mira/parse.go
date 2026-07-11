package mira

import (
	"regexp"
	"strings"
)

// SeverityMap maps the leading emoji of a Mira comment's second line to a
// Severity.
var SeverityMap = map[string]Severity{
	"⛔":  SeverityBlocker,
	"🛑":  SeverityBlocker,
	"⚠️":  SeverityWarning,
	"💡":  SeveritySuggestion,
	"💬":  SeverityNitpick,
}

// MiraAuthors is the list of author-login substrings that identify Mira bots.
var MiraAuthors = []string{"miracodeai-bot", "miracodeai", "bot-mira"}

// IsMiraComment reports whether the author login looks like a Mira bot.
func IsMiraComment(author string) bool {
	lower := strings.ToLower(author)
	for _, a := range MiraAuthors {
		if strings.Contains(lower, a) {
			return true
		}
	}
	return false
}

var reCategory = regexp.MustCompile(`^\*\*(.+?)\*\*`)

// ParseCategory extracts the category from the first `**bold**` run of the
// first line. Returns "unknown" when no match is found.
func ParseCategory(body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		return "unknown"
	}
	first := strings.TrimSpace(lines[0])
	m := reCategory.FindStringSubmatch(first)
	if len(m) < 2 {
		return "unknown"
	}
	if v := strings.TrimSpace(m[1]); v != "" {
		return v
	}
	return "unknown"
}

// ParseSeverity reads the emoji prefix of the second line and maps it to a
// Severity. Returns SeveritySuggestion as the default fallback.
func ParseSeverity(body string) Severity {
	lines := strings.Split(body, "\n")
	if len(lines) < 2 {
		return SeveritySuggestion
	}
	sevLine := strings.TrimSpace(lines[1])
	for emoji, sev := range SeverityMap {
		if strings.HasPrefix(sevLine, emoji) {
			return sev
		}
	}
	return SeveritySuggestion
}

// ParseTitle returns the first **bold** line after the category+severity lines
// that is not a "Fix" or "Note" marker. Returns "Untitled" when none is found.
func ParseTitle(body string) string {
	lines := strings.Split(body, "\n")
	for i := 2; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") &&
			!strings.HasPrefix(line, "**Fix") && !strings.HasPrefix(line, "**Note") {
			return strings.TrimSpace(line[2 : len(line)-2])
		}
	}
	return "Untitled"
}

var reSuggestion = regexp.MustCompile("```suggestion\n([\\s\\S]*?)```")

// ParseSuggestion extracts the inner text of a ```suggestion fenced block.
// Returns nil when absent.
func ParseSuggestion(body string) *string {
	m := reSuggestion.FindStringSubmatch(body)
	if len(m) < 2 {
		return nil
	}
	v := strings.TrimSpace(m[1])
	return &v
}

var (
	reAgentBlock = regexp.MustCompile(`<details>\s*<summary>Prompt for AI Agents</summary>([\s\S]*?)</details>`)
	reCodeBlock  = regexp.MustCompile("```\n?([\\s\\S]*?)```")
)

// ParseAgentPrompt extracts the "Prompt for AI Agents" details block, preferring
// the inner fenced code block's content. Returns nil when absent.
func ParseAgentPrompt(body string) *string {
	block := reAgentBlock.FindStringSubmatch(body)
	if len(block) < 2 {
		return nil
	}
	inner := block[1]
	code := reCodeBlock.FindStringSubmatch(inner)
	var v string
	if len(code) >= 2 {
		v = code[1]
	} else {
		v = inner
	}
	v = strings.TrimSpace(v)
	return &v
}

var (
	reStripSuggestion = regexp.MustCompile("```suggestion[\\s\\S]*?```")
	reStripAgent      = regexp.MustCompile(`<details>\s*<summary>Prompt for AI Agents</summary>[\s\S]*?</details>`)
	reStripFooter     = regexp.MustCompile(`(?s)> Not useful\?.*$`)
	reStripSeparator  = regexp.MustCompile(`(?m)^---$`)
)

// ParseBody strips suggestion blocks, agent-prompt details blocks, the "Not
// useful?" footer, and standalone separators from the raw body, returning the
// trimmed remainder.
func ParseBody(body string) string {
	cleaned := reStripSuggestion.ReplaceAllString(body, "")
	cleaned = reStripAgent.ReplaceAllString(cleaned, "")
	cleaned = reStripFooter.ReplaceAllString(cleaned, "")
	cleaned = reStripSeparator.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

// firstNonNilInt returns the first non-nil pointer, or nil.
func firstNonNilInt(a, b *int) *int {
	if a != nil {
		return a
	}
	return b
}

// ParseMiraComment assembles a ParsedComment from a RawComment, deriving all
// parsed fields from the body and applying path/line fallbacks.
func ParseMiraComment(comment RawComment) ParsedComment {
	file := "unknown"
	if comment.Path != nil && *comment.Path != "" {
		file = *comment.Path
	}

	startLine := firstNonNilInt(comment.StartLine, comment.Line)
	endLine := firstNonNilInt(comment.Line, comment.StartLine)

	lineStart := 0
	if startLine != nil {
		lineStart = *startLine
	}
	lineEnd := 0
	if endLine != nil {
		lineEnd = *endLine
	}

	return ParsedComment{
		ID:            comment.ID,
		File:          file,
		LineStart:     lineStart,
		LineEnd:       lineEnd,
		Category:      ParseCategory(comment.Body),
		Severity:      ParseSeverity(comment.Body),
		Title:         ParseTitle(comment.Body),
		Body:          ParseBody(comment.Body),
		Suggestion:    ParseSuggestion(comment.Body),
		AgentPrompt:   ParseAgentPrompt(comment.Body),
		DiffHunk:      comment.DiffHunk,
		IsResolved:    comment.IsResolved,
		CreatedAt:     comment.CreatedAt,
		ThreadReplies: comment.ThreadReplies,
	}
}

// FilterMiraRootComments keeps only Mira-authored root comments (no reply
// parent).
func FilterMiraRootComments(comments []RawComment) []RawComment {
	out := make([]RawComment, 0, len(comments))
	for _, c := range comments {
		if IsMiraComment(c.Author) && c.ReplyToID == nil {
			out = append(out, c)
		}
	}
	return out
}
