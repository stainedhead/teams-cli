package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
)

func TestConstructorsMapToExitCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want output.ExitCode
	}{
		{"usage", NewUsage("m", "h"), output.ExitUsage},
		{"validation", NewValidation("m", "h"), output.ExitValidation},
		{"policy", NewPolicyDenied("m", "h"), output.ExitPolicyDenied},
		{"conflict", NewConflict("m", "h"), output.ExitConflict},
		{"notfound", NewNotFound("m", "h"), output.ExitNotFound},
		{"notsent plain", NotSent(errors.New("dial")), output.ExitGeneral},
		{"notsent keeps cause category", NotSent(NewConflict("c", "")), output.ExitConflict},
		{"wrapped", fmt.Errorf("ctx: %w", NewValidation("m", "")), output.ExitValidation},
		{"decision default", Decision{Reason: "no"}.Err(), output.ExitPolicyDenied},
		{"decision rate", Decision{Category: output.CategoryRateLimited, Reason: "slow"}.Err(), output.ExitRateLimited},
	}
	for _, c := range cases {
		if got := output.ExitOf(c.err); got != c.want {
			t.Errorf("%s: exit %d, want %d", c.name, got, c.want)
		}
	}
}

func TestHintAndNotSent(t *testing.T) {
	e := NewUsage("bad", "try X")
	var h output.Hinter
	if !errors.As(error(e), &h) || h.Hint() != "try X" {
		t.Fatalf("hint missing")
	}
	if NotSent(nil) != nil {
		t.Fatal("NotSent(nil) must be nil")
	}
	if !IsNotSent(fmt.Errorf("x: %w", NotSent(errors.New("e")))) || IsNotSent(errors.New("e")) {
		t.Fatal("IsNotSent wrong")
	}
	if (Decision{Allowed: true}).Err() != nil {
		t.Fatal("allowed decision must yield nil")
	}
}
