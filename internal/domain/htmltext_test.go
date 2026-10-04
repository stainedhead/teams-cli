package domain

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHTMLToText(t *testing.T) {
	cases := []struct {
		name, in, text string
		links          []string
	}{
		{"plain", "hello", "hello", nil},
		{"p and br", "<p>one</p><p>two<br>three</p>", "one\n\ntwo\nthree", nil},
		{"at mention", `hi <at id="0">Jane Doe</at>, ok?`, "hi @Jane Doe, ok?", nil},
		{"list", "<ul><li>a</li><li>b</li></ul>", "- a\n- b", nil},
		{"ordered list", "<ol><li>x</li></ol>", "- x", nil},
		{"anchor", `see <a href="https://a.example/p?q=1&amp;r=2">the page</a>`, "see the page", []string{"https://a.example/p?q=1&r=2"}},
		{"anchor single quote", `<a href='https://b.example'>b</a>`, "b", []string{"https://b.example"}},
		{"anchor unquoted", `<a href=https://c.example>c</a>`, "c", []string{"https://c.example"}},
		{"anchor non-http ignored", `<a href="javascript:alert(1)">x</a>`, "x", nil},
		{"anchor no href", `<a name="n">x</a>`, "x", nil},
		{"plain url in text", "go to https://d.example/x, thanks", "go to https://d.example/x, thanks", []string{"https://d.example/x"}},
		{"dedupe", `<a href="https://e.example">https://e.example</a>`, "https://e.example", []string{"https://e.example"}},
		{"img ignored", `a<img src="https://tracker.example/p.png" alt="x">b`, "ab", nil},
		{"entities", "a &amp; b &lt;tag&gt; &quot;q&quot; &#39;s&#39; &nbsp;z", `a & b <tag> "q" 's' z`, nil},
		{"script stripped", "a<script>alert('x')<b>ignored</script>b", "ab", nil},
		{"style stripped", "a<STYLE type=text/css>p{}</STYLE>b", "ab", nil},
		{"unclosed script", "a<script>never closed", "a", nil},
		{"script close no gt", "a<script>x</script", "a", nil},
		{"comment", "a<!-- hidden <b> -->b", "ab", nil},
		{"unclosed comment", "a<!-- oops", "a", nil},
		{"nested blockquote", "<blockquote><p>q</p><blockquote>n</blockquote></blockquote>r", "q\n\nn\n\nr", nil},
		{"unbalanced", "<b>bold <i>both</b> tail</i></div>", "bold both tail", nil},
		{"stray lt", "1 < 2 and 3 <4 ok", "1 < 2 and 3 <4 ok", nil},
		{"lt at end", "x<", "x<", nil},
		{"unterminated tag", "x<b unterminated", "x<b unterminated", nil},
		{"quoted gt in attr", `<a href="https://q.example/?a>b" title='>'>t</a>`, "t", []string{"https://q.example/?a"}},
		{"whitespace collapse", "  a \n\t b  <p>  c  </p> ", "a b\nc", nil},
		{"many blank lines", "<p></p><p></p><p></p><p></p>x", "x", nil},
		{"escaped markup is text", "&lt;script&gt;x&lt;/script&gt;", "<script>x</script>", nil},
		{"empty", "", "", nil},
	}
	for _, c := range cases {
		text, links := HTMLToText(c.in)
		if text != c.text || !reflect.DeepEqual(links, c.links) {
			t.Errorf("%s:\n got %q %q\nwant %q %q", c.name, text, links, c.text, c.links)
		}
	}
}

func TestHTMLToTextBound(t *testing.T) {
	in := strings.Repeat("a", maxHTMLInput) + "<script>" + " https://late.example"
	text, links := HTMLToText(in)
	if len(text) != maxHTMLInput || links != nil {
		t.Fatalf("len=%d links=%v", len(text), links)
	}
	deep := strings.Repeat("<b>", 200000) + "x"
	if text, _ := HTMLToText(deep); text != "x" {
		t.Fatal("deep nesting")
	}
	long := strings.Repeat("<p>x", 100000)
	start := time.Now()
	HTMLToText(long)
	if time.Since(start) > 5*time.Second {
		t.Fatal("too slow")
	}
}

func FuzzHTMLToText(f *testing.F) {
	for _, s := range []string{"<p>a</p>", "<a href='x'>", "<!--", "<script>", "a<b", `<at id="1">`, "&amp;&#x41;"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		text, _ := HTMLToText(s)
		if strings.Contains(strings.ToLower(text), "<script") && !strings.Contains(strings.ToLower(s), "script") {
			t.Fatal("script appeared from nothing")
		}
	})
}
