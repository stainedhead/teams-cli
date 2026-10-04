package policyfile

import "testing"

func TestDevOptionsMatchBuild(t *testing.T) {
	env := func(map[string]string) func(string) string {
		return func(k string) string {
			if k == "TEAMS_POLICY_INSECURE" {
				return "1"
			}
			return ""
		}
	}(nil)
	got := DevOptions(env)
	if DevBuild {
		if len(got) != 1 {
			t.Fatal("dev build must honor TEAMS_POLICY_INSECURE=1")
		}
		if len(DevOptions(func(string) string { return "" })) != 0 {
			t.Fatal("override must be off by default")
		}
		return
	}
	if len(got) != 0 {
		t.Fatal("release build must ignore TEAMS_POLICY_INSECURE (FR-21)")
	}
}
