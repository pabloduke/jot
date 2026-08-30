package jot

import (
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

func pathURL(id string) string {
	parts := strings.Split(filepath.ToSlash(id), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// highlight escapes text and wraps query terms in <mark>.
func highlight(text string, terms []string) string {
	escaped := html.EscapeString(text)
	if len(terms) == 0 {
		return escaped
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range dedupe(terms) {
		if len(t) < 2 {
			continue
		}
		quoted = append(quoted, regexp.QuoteMeta(html.EscapeString(t)))
	}
	if len(quoted) == 0 {
		return escaped
	}
	re, err := regexp.Compile(`(?i)\b(` + strings.Join(quoted, "|") + `)`)
	if err != nil {
		return escaped
	}
	return re.ReplaceAllString(escaped, "<mark>$1</mark>")
}

var inlinePattern = regexp.MustCompile(
	"`([^`]+)`" + // code
		`|\[\[([^\]\|\n]+)(?:\|([^\]\n]*))?\]\]` + // wiki link
		`|\[\^([^\]\s]+)\]` + // footnote
		`|\[([^]\n]+)\]\(([^)[:space:]]+)\)` + // markdown link
		`|\*\*([^*\n]+)\*\*` + // bold
		`|\*([^*\n]+)\*` + // italic
		`|_([^_\n]+)_`) // italic

// renderInline formats one line of Markdown. titles maps concept ids to page
// titles so that [[wiki links]] can render a human label.
func renderInline(text, currentID string, titles map[string]string) string {
	var out strings.Builder
	last := 0
	for _, loc := range inlinePattern.FindAllStringSubmatchIndex(text, -1) {
		out.WriteString(html.EscapeString(text[last:loc[0]]))
		switch {
		case loc[2] >= 0: // code
			out.WriteString("<code>" + html.EscapeString(text[loc[2]:loc[3]]) + "</code>")
		case loc[4] >= 0: // wiki link
			target := text[loc[4]:loc[5]]
			href, external := wikiHref(target, currentID)
			label := target
			if loc[6] >= 0 && text[loc[6]:loc[7]] != "" {
				label = text[loc[6]:loc[7]]
			} else if !external {
				// Prefer the target page's own title over the raw slug.
				if title, ok := titles[conceptIDFromHref(href)]; ok && title != "" {
					label = title
				}
			}
			fmt.Fprintf(&out, `<a href="%s">%s</a>`, html.EscapeString(href), html.EscapeString(label))
		case loc[8] >= 0: // footnote
			id := text[loc[8]:loc[9]]
			fmt.Fprintf(&out, `<sup><a href="#source-%s">%s</a></sup>`, html.EscapeString(id), html.EscapeString(id))
		case loc[10] >= 0: // markdown link
			label := html.EscapeString(text[loc[10]:loc[11]])
			href, external := wikiHref(text[loc[12]:loc[13]], currentID)
			if external {
				fmt.Fprintf(&out, `<a href="%s" target="_blank" rel="noreferrer">%s</a>`, html.EscapeString(href), label)
			} else {
				fmt.Fprintf(&out, `<a href="%s">%s</a>`, html.EscapeString(href), label)
			}
		case loc[14] >= 0: // bold
			out.WriteString("<strong>" + html.EscapeString(text[loc[14]:loc[15]]) + "</strong>")
		case loc[16] >= 0: // italic with *
			out.WriteString("<em>" + html.EscapeString(text[loc[16]:loc[17]]) + "</em>")
		case loc[18] >= 0: // italic with _
			out.WriteString("<em>" + html.EscapeString(text[loc[18]:loc[19]]) + "</em>")
		}
		last = loc[1]
	}
	out.WriteString(html.EscapeString(text[last:]))
	return out.String()
}

// conceptIDFromHref recovers the concept id from a rendered wiki href.
func conceptIDFromHref(href string) string {
	id := strings.TrimPrefix(href, "/wiki/")
	if before, _, ok := strings.Cut(id, "#"); ok {
		id = before
	}
	unescaped, err := url.PathUnescape(id)
	if err != nil {
		return id
	}
	return unescaped
}

func wikiHref(target, currentID string) (string, bool) {
	if u, err := url.Parse(target); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return target, true
	}
	fragment := ""
	if before, after, ok := strings.Cut(target, "#"); ok {
		target, fragment = before, "#"+url.PathEscape(after)
	}
	target = strings.TrimSuffix(target, ".md")
	var id string
	if strings.HasPrefix(target, "/") {
		id = strings.TrimPrefix(target, "/")
	} else {
		id = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(currentID), filepath.FromSlash(target))))
	}
	if safe, err := safeID(id); err == nil {
		return "/wiki/" + pathURL(safe) + fragment, false
	}
	return "#", false
}

// Heading is one entry in a page's table of contents.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Slug  string `json:"slug"`
}

var slugNoise = regexp.MustCompile(`[^a-z0-9]+`)

func headingSlug(text string) string {
	s := slugNoise.ReplaceAllString(strings.ToLower(text), "-")
	return strings.Trim(s, "-")
}

// extractHeadings collects the level 2+ headings of a body, skipping fenced
// code so that shell comments never become table-of-contents entries.
func extractHeadings(d Document) []Heading {
	var out []Heading
	inCode := false
	seen := map[string]int{}
	for _, line := range strings.Split(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inCode = !inCode
			continue
		}
		if inCode || !strings.HasPrefix(trim, "#") {
			continue
		}
		level := len(trim) - len(strings.TrimLeft(trim, "#"))
		if level < 2 || level > 4 {
			continue
		}
		text := strings.TrimSpace(trim[level:])
		if text == "" {
			continue
		}
		slug := headingSlug(text)
		seen[slug]++
		if n := seen[slug]; n > 1 {
			slug = slug + "-" + itoa(n)
		}
		out = append(out, Heading{Level: level, Text: text, Slug: slug})
	}
	return out
}

var tableDividerPattern = regexp.MustCompile(`^\|?[\s:|-]+\|[\s:|-]*$`)

func isTableRow(line string) bool {
	trim := strings.TrimSpace(line)
	return strings.HasPrefix(trim, "|") && strings.Count(trim, "|") >= 2
}

func splitTableRow(line string) []string {
	trim := strings.TrimSpace(line)
	trim = strings.TrimPrefix(trim, "|")
	trim = strings.TrimSuffix(trim, "|")
	cells := strings.Split(trim, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

var orderedItemPattern = regexp.MustCompile(`^(\d+)[.)]\s+(.*)$`)

// leadingIndent measures a list item's indent, counting a tab as four columns.
func leadingIndent(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// renderMarkdown converts a concept body to HTML. It deliberately implements a
// small, predictable subset rather than pulling in a full CommonMark engine.
func renderMarkdown(d Document, titles map[string]string) string {
	lines := strings.Split(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\n")
	var out strings.Builder
	var paragraph []string
	inCode, authoritative := false, false

	// Lists are tracked as a stack of (tag, indent) so that indented items
	// nest instead of flattening.
	type listLevel struct {
		tag    string
		indent int
	}
	var lists []listLevel
	slugs := map[string]int{}

	badge := func() {
		if authoritative {
			out.WriteString(`<span class="badge">Authoritative</span>`)
			authoritative = false
		}
	}
	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		badge()
		out.WriteString("<p>" + renderInline(strings.Join(paragraph, " "), d.ID, titles) + "</p>")
		paragraph = nil
	}
	closeList := func() {
		for len(lists) > 0 {
			out.WriteString("</li></" + lists[len(lists)-1].tag + ">")
			lists = lists[:len(lists)-1]
		}
	}
	// openItem reconciles the list stack with one item's indent and marker,
	// then emits the opening <li>.
	openItem := func(tag string, indent int) {
		for len(lists) > 0 && indent < lists[len(lists)-1].indent {
			top := lists[len(lists)-1]
			out.WriteString("</li></" + top.tag + ">")
			lists = lists[:len(lists)-1]
		}
		if len(lists) == 0 {
			out.WriteString("<" + tag + ">")
			lists = append(lists, listLevel{tag: tag, indent: indent})
			out.WriteString("<li>")
			return
		}

		top := &lists[len(lists)-1]
		if indent > top.indent {
			// The parent <li> deliberately remains open so the nested list is
			// valid HTML content of that item.
			out.WriteString("<" + tag + ">")
			lists = append(lists, listLevel{tag: tag, indent: indent})
			out.WriteString("<li>")
			return
		}

		// A sibling closes the preceding item. Switching marker type at the
		// same indentation also closes and replaces the surrounding list.
		out.WriteString("</li>")
		if tag != top.tag {
			out.WriteString("</" + top.tag + "><" + tag + ">")
			top.tag = tag
		}
		out.WriteString("<li>")
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)

		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			flushParagraph()
			closeList()
			if !inCode {
				badge()
				out.WriteString("<pre><code>")
			} else {
				out.WriteString("</code></pre>")
			}
			inCode = !inCode
			continue
		}
		if inCode {
			out.WriteString(html.EscapeString(line) + "\n")
			continue
		}
		if trim == authorityMarker {
			flushParagraph()
			closeList()
			authoritative = true
			continue
		}
		if trim == "" {
			flushParagraph()
			closeList()
			continue
		}
		if trim == "---" || trim == "***" || trim == "___" {
			flushParagraph()
			closeList()
			out.WriteString("<hr>")
			continue
		}
		// Tables: a header row followed by a divider row.
		if isTableRow(line) && i+1 < len(lines) && tableDividerPattern.MatchString(strings.TrimSpace(lines[i+1])) {
			flushParagraph()
			closeList()
			badge()
			out.WriteString(`<div class="tablewrap"><table><thead><tr>`)
			for _, cell := range splitTableRow(line) {
				out.WriteString("<th>" + renderInline(cell, d.ID, titles) + "</th>")
			}
			out.WriteString("</tr></thead><tbody>")
			i++
			for i+1 < len(lines) && isTableRow(lines[i+1]) {
				i++
				out.WriteString("<tr>")
				for _, cell := range splitTableRow(lines[i]) {
					out.WriteString("<td>" + renderInline(cell, d.ID, titles) + "</td>")
				}
				out.WriteString("</tr>")
			}
			out.WriteString("</tbody></table></div>")
			continue
		}
		if strings.HasPrefix(trim, "#") {
			flushParagraph()
			closeList()
			level := len(trim) - len(strings.TrimLeft(trim, "#"))
			if level > 6 {
				level = 6
			}
			text := strings.TrimSpace(trim[level:])
			if level == 1 && strings.EqualFold(text, d.Title) {
				continue
			}
			badge()
			// Anchors must match extractHeadings so the TOC links resolve.
			slug := headingSlug(text)
			slugs[slug]++
			if n := slugs[slug]; n > 1 {
				slug = slug + "-" + itoa(n)
			}
			fmt.Fprintf(&out, `<h%d id="%s">%s</h%d>`, level, html.EscapeString(slug),
				renderInline(text, d.ID, titles), level)
			continue
		}
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
			flushParagraph()
			openItem("ul", leadingIndent(line))
			badge()
			out.WriteString(renderInline(strings.TrimSpace(trim[2:]), d.ID, titles))
			continue
		}
		if m := orderedItemPattern.FindStringSubmatch(trim); m != nil {
			flushParagraph()
			openItem("ol", leadingIndent(line))
			badge()
			out.WriteString(renderInline(m[2], d.ID, titles))
			continue
		}
		if strings.HasPrefix(trim, "> ") {
			flushParagraph()
			closeList()
			badge()
			out.WriteString("<blockquote>" + renderInline(strings.TrimSpace(trim[2:]), d.ID, titles) + "</blockquote>")
			continue
		}
		paragraph = append(paragraph, trim)
	}
	flushParagraph()
	closeList()
	if inCode {
		out.WriteString("</code></pre>")
	}
	return out.String()
}
