package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// DefaultBaseURL is the Graph v1.0 root.
const DefaultBaseURL = "https://graph.microsoft.com/v1.0"

const (
	maxJSONBytes   = 16 << 20 // bound on one JSON response
	defaultMaxPage = 5        // paging cap per call
	maxTop         = 50       // Graph's page size ceiling for message lists
	defaultScan    = 50       // default chat scan bound for ResolveUserChat
	maxIDLen       = 512
)

var _ usecase.Graph = (*Client)(nil)

// Config configures a Client.
type Config struct {
	// Refresher authorizes requests (an *auth.Authorizer in production, over
	// authtest.Fake in tests). The adapter never sees a token.
	Refresher httpx.TokenRefresher
	// BaseURL defaults to DefaultBaseURL; tests point it at graphtest.
	BaseURL string
	// HTTP carries retry tuning (MaxRetries, Clock, ...). Refresher,
	// AllowedHosts and VendorCode are set by New.
	HTTP httpx.Config
	// MaxPages bounds how many pages one call follows (default 5).
	MaxPages int
	// MaxChatScan bounds how many chats ResolveUserChat inspects (default 50).
	MaxChatScan int
}

// Client talks to Graph. It is safe for concurrent use.
type Client struct {
	hc       *http.Client
	base     *url.URL
	baseStr  string
	maxPages int
	maxScan  int
}

// New builds a Client; it fails on an unusable BaseURL.
func New(cfg Config) (*Client, error) {
	raw := strings.TrimRight(cfg.BaseURL, "/")
	if raw == "" {
		raw = DefaultBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, domain.NewUsage("graph base URL is not a valid http(s) URL", "")
	}
	hcfg := cfg.HTTP
	hcfg.Refresher = cfg.Refresher
	hcfg.AllowedHosts = []string{u.Host}
	hcfg.VendorCode = vendorCode
	c := &Client{hc: httpx.NewClient(hcfg), base: u, baseStr: raw, maxPages: cfg.MaxPages, maxScan: cfg.MaxChatScan}
	if c.maxPages <= 0 {
		c.maxPages = defaultMaxPage
	}
	if c.maxScan <= 0 {
		c.maxScan = defaultScan
	}
	return c, nil
}

// vendorCode picks a diagnostic from 403 response headers.
// ASSUMPTION (UA-11, unverified against a real tenant): Graph's real error
// code lives in the body, which httpx never offers; x-ms-error-code is a
// guess and request-id is a correlation id.
func vendorCode(h http.Header) string {
	for _, k := range []string{"X-Ms-Error-Code", "Request-Id", "Client-Request-Id"} {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	return ""
}

// do sends one request and returns the response for a 2xx status; the caller
// closes the body. op is a path template used in error messages.
func (c *Client) do(ctx context.Context, method, rawURL, op string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("graph %s: encode request: %w", op, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, fmt.Errorf("graph %s: build request", op)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodGet {
		req = httpx.MarkSafeToRetry(req)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, withStatus(unwrapURLError(err))
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	_ = resp.Body.Close()
	return nil, statusError(resp.StatusCode, op)
}

func decode(resp *http.Response, op string, out any) error {
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(out); err != nil {
		return &apiError{cat: output.CategoryValidation, msg: "graph " + op + ": malformed response", status: resp.StatusCode}
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, rawURL, op string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, rawURL, op, nil)
	if err != nil {
		return err
	}
	return decode(resp, op, out)
}

// postJSON sends a write. Failures are classified for the ledger.
func (c *Client) postJSON(ctx context.Context, rawURL, op string, body, out any) error {
	resp, err := c.do(ctx, http.MethodPost, rawURL, op, body)
	if err != nil {
		return classifyWrite(err)
	}
	return decode(resp, op, out)
}

// pages follows a collection from first, calling each for every page, for at
// most maxPages pages. Next links must stay on the configured host and path
// prefix; anything else is refused before a request is made. each returns true
// to stop early.
func (c *Client) pages(ctx context.Context, first, op string, each func(p *page) bool) error {
	next := first
	for i := 0; i < c.maxPages && next != ""; i++ {
		var p page
		if err := c.getJSON(ctx, next, op, &p); err != nil {
			return err
		}
		if each(&p) {
			return nil
		}
		next = p.Next
		if next != "" && !c.sameOrigin(next) {
			return &apiError{cat: output.CategoryValidation, msg: "graph " + op + ": refused next page link outside the graph host", status: 0}
		}
	}
	return nil
}

// sameOrigin reports whether link is on the base scheme, host and path prefix.
func (c *Client) sameOrigin(link string) bool {
	u, err := url.Parse(link)
	if err != nil || u.User != nil {
		return false
	}
	return u.Scheme == c.base.Scheme && u.Host == c.base.Host &&
		(c.base.Path == "" || strings.HasPrefix(u.Path, c.base.Path+"/"))
}

func seg(s string) string { return url.PathEscape(s) }

// esc escapes a query value (spaces as %20, never +).
func esc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// requireID validates an id before it becomes a path segment or an OData
// literal. Teams ids contain letters, digits and - _ = . : @ only.
func requireID(kind, id string) error {
	if strings.TrimSpace(id) == "" {
		return domain.NewValidation(kind+" id is empty", "")
	}
	if len(id) > maxIDLen || id == "." || id == ".." {
		return domain.NewUsage(kind+" id is not a valid id", "")
	}
	for i := 0; i < len(id); i++ {
		b := id[i]
		ok := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
			b == '-' || b == '_' || b == '=' || b == '.' || b == ':' || b == '@'
		if !ok {
			return domain.NewUsage(kind+" id is not a valid id", "")
		}
	}
	return nil
}

func clampTop(limit int) int {
	if limit <= 0 || limit > maxTop {
		return maxTop
	}
	return limit
}
