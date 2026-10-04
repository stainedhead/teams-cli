package policyfile

import (
	"bytes"
	"os"
	"testing"
)

// TestUserDocsSampleMatchesGolden keeps the sample policy published in
// user-docs byte-equal to the golden copy that the loader tests parse.
func TestUserDocsSampleMatchesGolden(t *testing.T) {
	golden, err := os.ReadFile("testdata/teams.policy.sample.yaml")
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile("../../../user-docs/teams.policy.sample.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(golden, published) {
		t.Fatal("user-docs/teams.policy.sample.yaml differs from testdata/teams.policy.sample.yaml")
	}
}
