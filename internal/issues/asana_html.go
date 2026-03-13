package issues

import (
	"html"
	"regexp"
	"strings"
)

// markdownToAsanaHTML converts a markdown string to Asana's supported HTML
// subset for rich-text comments (stories). Asana supports: <body>, <strong>,
// <em>, <code>, <pre>, <ul>, <ol>, <li>, <a href>.
//
// It also translates HTML-comment markers (<!-- erg:plan -->, <!-- erg:step=X -->)
// to visible text markers ([erg:plan], [erg:step=X]) since Asana rejects HTML
// comments inside html_text.
func markdownToAsanaHTML(md string) string {
	md = translateMarkersForAsana(md)

	lines := strings.Split(md, "\n")
	var out []string

	inCodeBlock := false
	inUL := false
	inOL := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Fenced code block toggle.
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inCodeBlock {
				out = append(out, "</pre>")
				inCodeBlock = false
			} else {
				if inUL {
					out = append(out, "</ul>")
					inUL = false
				}
				if inOL {
					out = append(out, "</ol>")
					inOL = false
				}
				out = append(out, "<pre>")
				inCodeBlock = true
			}
			continue
		}

		if inCodeBlock {
			out = append(out, html.EscapeString(line))
			continue
		}

		// Try parsing as list items once, reuse result for both
		// "still in list?" check and item extraction.
		ulText, isUL := parseUnorderedItem(line)
		olText, isOL := parseOrderedItem(line)

		// Close open lists if the line doesn't continue them.
		if inUL && !isUL {
			out = append(out, "</ul>")
			inUL = false
		}
		if inOL && !isOL {
			out = append(out, "</ol>")
			inOL = false
		}

		// ATX headers: ## Header -> <strong>Header</strong>
		if hdr, ok := parseHeader(line); ok {
			out = append(out, "<strong>"+convertInline(html.EscapeString(hdr))+"</strong>")
			continue
		}

		// Unordered list item: - item or * item
		if isUL {
			if !inUL {
				out = append(out, "<ul>")
				inUL = true
			}
			out = append(out, "<li>"+convertInline(html.EscapeString(ulText))+"</li>")
			continue
		}

		// Ordered list item: 1. item
		if isOL {
			if !inOL {
				out = append(out, "<ol>")
				inOL = true
			}
			out = append(out, "<li>"+convertInline(html.EscapeString(olText))+"</li>")
			continue
		}

		// Regular line: convert inline formatting.
		out = append(out, convertInline(html.EscapeString(line)))
	}

	// Close any open blocks.
	if inCodeBlock {
		out = append(out, "</pre>")
	}
	if inUL {
		out = append(out, "</ul>")
	}
	if inOL {
		out = append(out, "</ol>")
	}

	return "<body>" + strings.Join(out, "\n") + "</body>"
}

// --- Marker translation ---

var htmlCommentMarkerRe = regexp.MustCompile(`<!--\s*(erg:\S+?)\s*-->`)

// translateMarkersForAsana converts HTML-comment erg markers to visible text
// form suitable for Asana html_text. e.g. <!-- erg:plan --> -> [erg:plan]
func translateMarkersForAsana(text string) string {
	return htmlCommentMarkerRe.ReplaceAllString(text, "[$1]")
}

var textMarkerRe = regexp.MustCompile(`\[(erg:\S+?)\]`)

// translateMarkersFromAsana converts visible text erg markers back to
// HTML-comment form so the rest of the system can detect them.
// e.g. [erg:plan] -> <!-- erg:plan -->
func translateMarkersFromAsana(text string) string {
	return textMarkerRe.ReplaceAllString(text, "<!-- $1 -->")
}

// --- Header parsing ---

var headerRe = regexp.MustCompile(`^#{1,6}\s+(.+)$`)

func parseHeader(line string) (string, bool) {
	m := headerRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// --- List item parsing ---

var unorderedItemRe = regexp.MustCompile(`^\s*[-*]\s+(.+)$`)

func parseUnorderedItem(line string) (string, bool) {
	m := unorderedItemRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

var orderedItemRe = regexp.MustCompile(`^\s*\d+\.\s+(.+)$`)

func parseOrderedItem(line string) (string, bool) {
	m := orderedItemRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// --- Inline formatting ---
//
// Applied AFTER HTML-escaping, so we need to match on escaped text but
// produce unescaped HTML tags. Bold must be checked before italic since
// ** is a prefix of *.

// convertInline applies inline markdown formatting to an already HTML-escaped
// line. The order matters: bold (**) before italic (*), and links before
// other patterns to avoid mangling URLs.
func convertInline(escaped string) string {
	// Links: [text](url) — note: parens in the url part are HTML-escaped already
	escaped = linkRe.ReplaceAllString(escaped, `<a href="$2">$1</a>`)

	// Bold: **text**
	escaped = boldRe.ReplaceAllString(escaped, `<strong>$1</strong>`)

	// Italic: *text* (but not inside words or if preceded by *)
	escaped = italicRe.ReplaceAllString(escaped, `${1}<em>$2</em>${3}`)

	// Inline code: `text`
	escaped = inlineCodeRe.ReplaceAllString(escaped, `<code>$1</code>`)

	return escaped
}

var (
	linkRe       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	boldRe       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe     = regexp.MustCompile(`(^|[^*])\*([^*]+?)\*([^*]|$)`)
	inlineCodeRe = regexp.MustCompile("`([^`]+)`")
)
