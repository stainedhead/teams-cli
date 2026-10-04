package domain

import (
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
)

func TestParseAlias(t *testing.T) {
	valid := []string{"channel:sdlc-alerts", "chat:ops.team_1", "user:jane", "user:a", "channel:" + strings.Repeat("a", 64), "chat:0-9._"}
	for _, in := range valid {
		a, err := ParseAlias(in)
		if err != nil || string(a) != in {
			t.Errorf("ParseAlias(%q) = %q, %v", in, a, err)
		}
	}
	invalid := []string{
		"", "channel:", ":name", "channel", "team:x", "Channel:x", "channel:Upper", "channel:" + strings.Repeat("a", 65),
		"19:abc@thread.v2", "19:abc123def@thread.tacv2", "123e4567-e89b-12d3-a456-426614174000",
		"chat:has space", "user:a/b", "user:a:b", "channel:ünï", "user:x\n", " user:x",
	}
	for _, in := range invalid {
		_, err := ParseAlias(in)
		if err == nil {
			t.Errorf("ParseAlias(%q) accepted", in)
			continue
		}
		if output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("ParseAlias(%q) category = %v", in, output.CategoryOf(err))
		}
		if strings.Contains(err.Error(), in) && in != "" {
			t.Errorf("error echoes input %q", in)
		}
	}
}

func TestAliasKindName(t *testing.T) {
	a := Alias("channel:sdlc-alerts")
	if a.Kind() != KindChannel || a.Name() != "sdlc-alerts" {
		t.Fatalf("got %q %q", a.Kind(), a.Name())
	}
	if Alias("x").Kind() != "x" || Alias("x").Name() != "" {
		t.Fatal("no-colon alias")
	}
}
