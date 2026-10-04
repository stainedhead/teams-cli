package domain

import (
	"html"
	"regexp"
	"strings"
)

// maxHTMLInput bounds the HTML consumed by HTMLToText (UA-10).
const maxHTMLInput = 1 << 20

var urlRE = regexp.MustCompile(`(?i)https?://[^\s<>"'\x60]+`)

// extractURLs returns the distinct http(s) URLs in text in order of
// appearance, with trailing sentence punctuation and unbalanced closing
// brackets trimmed.
func extractURLs(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range urlRE.FindAllString(text, -1) {
		m = strings.TrimRight(m, ".,;:!?)]}")
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

var (
	hrefRE  = regexp.MustCompile(`(?is)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	spaceRE = regexp.MustCompile(`[ \t\r\n\f]+`)
	nlRE    = regexp.MustCompile(`\n{3,}`)
)

var srcWS = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "blockquote": true, "ul": true, "ol": true,
	"li": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"tr": true, "table": true, "pre": true, "hr": true,
}

// HTMLToText converts a Teams HTML body to plain text plus the http(s) links
// found in <a href> and in the text (UA-10). It is a lenient scanner, not a
// parser: unbalanced and nested tags are tolerated, <script>/<style> content
// and comments are dropped, <img> is ignored, <at>name</at> becomes @name and
// entities are decoded. Input beyond 1 MiB is ignored. Output is plain text:
// the caller treats it as untrusted data.
func HTMLToText(in string) (text string, links []string) {
	if len(in) > maxHTMLInput {
		in = in[:maxHTMLInput]
	}
	var b strings.Builder
	var hrefs []string
	i := 0
	for i < len(in) {
		lt := strings.IndexByte(in[i:], '<')
		if lt < 0 {
			b.WriteString(srcWS.Replace(in[i:]))
			break
		}
		b.WriteString(srcWS.Replace(in[i : i+lt]))
		i += lt
		if strings.HasPrefix(in[i:], "<!--") {
			if end := strings.Index(in[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
			} else {
				i = len(in)
			}
			continue
		}
		if i+1 >= len(in) || !isTagStart(in[i+1]) {
			b.WriteByte('<')
			i++
			continue
		}
		gt := tagEnd(in, i)
		if gt < 0 {
			b.WriteByte('<')
			i++
			continue
		}
		tag := in[i+1 : gt]
		i = gt + 1
		closing := strings.HasPrefix(tag, "/")
		name := tagName(strings.TrimPrefix(tag, "/"))
		switch {
		case !closing && (name == "script" || name == "style"):
			i = skipRawText(in, i, name)
		case name == "a" && !closing:
			if m := hrefRE.FindStringSubmatch(tag); m != nil {
				hrefs = append(hrefs, html.UnescapeString(m[1]+m[2]+m[3]))
			}
		case name == "at" && !closing:
			b.WriteByte('@')
		case name == "li" && closing:
		case name == "li":
			b.WriteString("\n- ")
		case blockTags[name]:
			b.WriteByte('\n')
		}
	}
	text = collapse(html.UnescapeString(b.String()))
	for _, h := range hrefs {
		links = appendUnique(links, extractURLs(h))
	}
	links = appendUnique(links, extractURLs(text))
	return text, links
}

func appendUnique(dst, src []string) []string {
	for _, s := range src {
		dup := false
		for _, d := range dst {
			if d == s {
				dup = true
				break
			}
		}
		if !dup {
			dst = append(dst, s)
		}
	}
	return dst
}

func isTagStart(c byte) bool {
	return c == '/' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// tagEnd finds the closing '>' of the tag starting at i, honoring quoted
// attribute values; -1 if the tag never closes.
func tagEnd(s string, i int) int {
	var quote byte
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return j
		}
	}
	return -1
}

func tagName(tag string) string {
	end := strings.IndexAny(tag, " \t\r\n\f/")
	if end >= 0 {
		tag = tag[:end]
	}
	return strings.ToLower(tag)
}

// skipRawText returns the index after the closing </name> at or after i, or
// len(s) when it never closes.
func skipRawText(s string, i int, name string) int {
	lower := strings.ToLower(s[i:])
	idx := strings.Index(lower, "</"+name)
	if idx < 0 {
		return len(s)
	}
	end := strings.IndexByte(lower[idx:], '>')
	if end < 0 {
		return len(s)
	}
	return i + idx + end + 1
}

// collapse applies HTML whitespace rules: runs of blanks become one space,
// block boundaries (already emitted as newlines) are kept, at most one blank
// line survives, and the result is trimmed.
func collapse(s string) string {
	s = strings.ReplaceAll(s, " ", " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRE.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(nlRE.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
