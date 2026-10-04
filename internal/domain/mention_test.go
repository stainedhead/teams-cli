package domain

import (
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
)

func mentionPolicy() Policy {
	p := testPolicy()
	p.Destinations["user:jane"] = Destination{Alias: "user:jane", Kind: KindUser, AADID: "AAAAAAAA-1111-1111-1111-111111111111", DisplayName: "Jane <b>Doe</b> & Co"}
	p.Destinations["user:bob"] = Destination{Alias: "user:bob", Kind: KindUser, AADID: idOther, DisplayName: "Bob"}
	p.Destinations["user:nodn"] = Destination{Alias: "user:nodn", Kind: KindUser, AADID: idOther}
	p.Destinations["user:noid"] = Destination{Alias: "user:noid", Kind: KindUser, DisplayName: "X"}
	p.Destinations["user:unallowed"] = Destination{Alias: "user:unallowed", Kind: KindUser, AADID: idOther, DisplayName: "U"}
	p.Send.Mentions = MentionPolicy{Allow: []Alias{"user:jane", "user:bob", "user:nodn", "user:noid", "channel:b", "user:gone"}, BlockBroadcast: true, Max: 2}
	return p
}

func TestBuildMentionsNone(t *testing.T) {
	p := mentionPolicy()
	p.Send.Prefix = "[agent] "
	m, err := BuildMentions(p, nil, "a < b & c")
	if err != nil || m.HTML || len(m.Mentions) != 0 || m.Text != "[agent] a < b & c" {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestBuildMentionsHTML(t *testing.T) {
	p := mentionPolicy()
	m, err := BuildMentions(p, []Alias{"user:jane", "user:bob", "user:jane"}, "hi <at id=\"9\">x</at> & \"q\"\nline")
	if err != nil {
		t.Fatal(err)
	}
	if !m.HTML || len(m.Mentions) != 2 {
		t.Fatalf("%+v", m)
	}
	if m.Mentions[0] != (OutMention{ID: 0, AADID: "aaaaaaaa-1111-1111-1111-111111111111", DisplayName: "Jane <b>Doe</b> & Co"}) || m.Mentions[1].ID != 1 {
		t.Fatalf("%+v", m.Mentions)
	}
	if strings.Count(m.Text, "<at ") != 2 {
		t.Fatalf("injected <at> survived: %s", m.Text)
	}
	want := `<at id="0">Jane &lt;b&gt;Doe&lt;/b&gt; &amp; Co</at> <at id="1">Bob</at> hi &lt;at id=&#34;9&#34;&gt;x&lt;/at&gt; &amp; &#34;q&#34;<br>line`
	if m.Text != want {
		t.Fatalf("\n got %s\nwant %s", m.Text, want)
	}
}

func TestBuildMentionsRejections(t *testing.T) {
	p := mentionPolicy()
	cases := []struct {
		name string
		in   []Alias
		cat  output.Category
	}{
		{"channel alias", []Alias{"channel:b"}, output.CategoryPolicyDenied},
		{"chat alias", []Alias{"chat:a"}, output.CategoryPolicyDenied},
		{"broadcast-like", []Alias{"channel:everyone"}, output.CategoryPolicyDenied},
		{"unlisted user", []Alias{"user:stranger"}, output.CategoryPolicyDenied},
		{"allowed but not a destination", []Alias{"user:gone"}, output.CategoryPolicyDenied},
		{"destination not in allow", []Alias{"user:unallowed"}, output.CategoryPolicyDenied},
		{"no display name", []Alias{"user:nodn"}, output.CategoryPolicyDenied},
		{"no aad id", []Alias{"user:noid"}, output.CategoryPolicyDenied},
		{"over max", []Alias{"user:jane", "user:bob", "user:nodn"}, output.CategoryPolicyDenied},
		{"malformed", []Alias{"19:abc@thread.v2"}, output.CategoryUsage},
	}
	for _, c := range cases {
		_, err := BuildMentions(p, c.in, "t")
		if err == nil || output.CategoryOf(err) != c.cat {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// default max 5 when unset; duplicates do not count
	p.Send.Mentions.Max = 0
	for i := 0; i < 5; i++ {
		a := Alias("user:u" + string(rune('a'+i)))
		p.Destinations[a] = Destination{Alias: a, Kind: KindUser, AADID: idOther, DisplayName: "n"}
		p.Send.Mentions.Allow = append(p.Send.Mentions.Allow, a)
	}
	five := []Alias{"user:ua", "user:ub", "user:uc", "user:ud", "user:ue"}
	if _, err := BuildMentions(p, append(five, "user:ua"), "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildMentions(p, append(five, "user:jane"), "t"); err == nil {
		t.Fatal("6 mentions accepted")
	}
}
