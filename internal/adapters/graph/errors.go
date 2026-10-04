package graph

import (
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/teams-cli/internal/domain"
)

// apiError is a Graph failure derived from an HTTP status. Its message only
// ever contains the operation template and the status (FR-31).
type apiError struct {
	cat    output.Category
	msg    string
	hint   string
	status int
}

func (e *apiError) Error() string             { return e.msg }
func (e *apiError) Category() output.Category { return e.cat }
func (e *apiError) Hint() string              { return e.hint }

// statusOf returns the HTTP status behind err, or 0.
func statusOf(err error) int {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status
	}
	var rl *httpx.RateLimitedError
	if errors.As(err, &rl) {
		return rl.Status
	}
	var fb *httpx.ForbiddenError
	if errors.As(err, &fb) {
		return http.StatusForbidden
	}
	var au *httpx.AuthError
	if errors.As(err, &au) {
		return http.StatusUnauthorized
	}
	return 0
}

// statusError maps a non-2xx status that httpx passed through. op is a path
// template such as "GET /chats/{id}/messages".
func statusError(status int, op string) error {
	msg := "graph " + op + " failed (HTTP " + strconv.Itoa(status) + ")"
	switch status {
	case http.StatusNotFound, http.StatusGone:
		return &apiError{cat: output.CategoryNotFound, msg: msg, status: status}
	case http.StatusBadRequest:
		return &apiError{cat: output.CategoryValidation, msg: msg, status: status}
	case http.StatusConflict, http.StatusPreconditionFailed:
		return &apiError{cat: output.CategoryConflict, msg: msg, status: status}
	}
	return &apiError{cat: output.CategoryGeneral, msg: msg, status: status}
}

// unwrapURLError strips the *url.Error http.Client adds (it carries the full
// URL) so typed httpx errors and their categories surface directly.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

// classifyWrite wraps domain.NotSent around failures that prove the server did
// not process the POST. Everything else (timeout, 5xx, dropped connection)
// stays ambiguous on purpose so the ledger entry remains pending.
func classifyWrite(err error) error {
	if err == nil {
		return nil
	}
	var ae *apiError
	if errors.As(err, &ae) {
		if ae.status >= 400 && ae.status < 500 && ae.status != http.StatusRequestTimeout {
			return domain.NotSent(err)
		}
		return err
	}
	var fh *httpx.ForbiddenHostError
	var au *httpx.AuthError
	var fb *httpx.ForbiddenError
	switch {
	case errors.As(err, &fh), errors.As(err, &au), errors.As(err, &fb):
		return domain.NotSent(err)
	}
	var rl *httpx.RateLimitedError
	if errors.As(err, &rl) {
		if rl.Status == http.StatusTooManyRequests {
			return domain.NotSent(err)
		}
		if rl.Status == 0 && neverSent(rl.Err) {
			return domain.NotSent(err)
		}
		return err
	}
	if neverSent(err) {
		return domain.NotSent(err)
	}
	return err
}

// neverSent reports transport failures that happen before any request byte
// leaves: DNS, dial and certificate verification errors.
func neverSent(err error) bool {
	var dns *net.DNSError
	var op *net.OpError
	var ua x509.UnknownAuthorityError
	var hn x509.HostnameError
	var ci x509.CertificateInvalidError
	switch {
	case err == nil:
		return false
	case errors.As(err, &dns), errors.As(err, &ua), errors.As(err, &hn), errors.As(err, &ci):
		return true
	case errors.As(err, &op):
		return op.Op == "dial"
	}
	return false
}
