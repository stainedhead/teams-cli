package domain

import (
	"errors"

	"github.com/stainedhead/agent-cli-core/output"
)

// Error is the domain error; it implements output.CategoryError and
// output.Hinter so output.ExitOf maps it to an exit code.
type Error struct {
	Cat  output.Category
	Msg  string
	Hnt  string
	wrap error
}

// Error returns the message.
func (e *Error) Error() string { return e.Msg }

// Category returns the error category.
func (e *Error) Category() output.Category { return e.Cat }

// Hint returns the actionable hint, if any.
func (e *Error) Hint() string { return e.Hnt }

// Unwrap returns the wrapped cause, if any.
func (e *Error) Unwrap() error { return e.wrap }

func newErr(c output.Category, msg, hint string) *Error {
	return &Error{Cat: c, Msg: msg, Hnt: hint}
}

// NewUsage returns a usage error (exit 2).
func NewUsage(msg, hint string) *Error { return newErr(output.CategoryUsage, msg, hint) }

// NewValidation returns a validation error (exit 9).
func NewValidation(msg, hint string) *Error { return newErr(output.CategoryValidation, msg, hint) }

// NewPolicyDenied returns a policy denial (exit 6).
func NewPolicyDenied(msg, hint string) *Error { return newErr(output.CategoryPolicyDenied, msg, hint) }

// NewConflict returns a conflict error (exit 7).
func NewConflict(msg, hint string) *Error { return newErr(output.CategoryConflict, msg, hint) }

// NewNotFound returns a not-found error (exit 5).
func NewNotFound(msg, hint string) *Error { return newErr(output.CategoryNotFound, msg, hint) }

// notSent marks a write that provably was not processed by the server. It
// keeps the category of the underlying cause.
type notSent struct{ cause error }

func (n *notSent) Error() string { return n.cause.Error() }
func (n *notSent) Unwrap() error { return n.cause }
func (n *notSent) Category() output.Category {
	return output.CategoryOf(n.cause)
}

// NotSent wraps err as a proven-unprocessed write failure. The ledger entry
// may then be retried. A nil err returns nil.
func NotSent(err error) error {
	if err == nil {
		return nil
	}
	return &notSent{cause: err}
}

// IsNotSent reports whether err is, or wraps, a NotSent error.
func IsNotSent(err error) bool {
	var n *notSent
	return errors.As(err, &n)
}

// Err converts a denied Decision to a domain Error; an allowed one yields nil.
func (d Decision) Err() error {
	if d.Allowed {
		return nil
	}
	cat := d.Category
	if cat == "" || cat == output.CategoryOK {
		cat = output.CategoryPolicyDenied
	}
	return newErr(cat, d.Reason, "")
}
