package graph

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

func itoa(n int) string { return strconv.Itoa(n) }

func chanPath(teamID, channelID string) string {
	return "/teams/" + seg(teamID) + "/channels/" + seg(channelID)
}

// ListChannelMessages returns top-level channel messages. With a stored
// deltaToken (an opaque delta link this adapter minted) it continues that
// delta; otherwise it starts a delta and falls back to a plain list when the
// delta route is refused. newDelta is empty when no delta link was reached
// (fallback, or the page cap was hit first).
//
// ASSUMPTION (UA-3, unverified against a real tenant): channel message delta
// works with delegated tokens; the fallback is list plus a client-side
// lastModified filter. Delta results are not truncated to limit, so no change
// is lost behind an advanced token; they are bounded by MaxPages.
func (c *Client) ListChannelMessages(ctx context.Context, teamID, channelID, deltaToken string, since time.Time, limit int) ([]domain.RawMessage, string, error) {
	if err := requireID("team", teamID); err != nil {
		return nil, "", err
	}
	if err := requireID("channel", channelID); err != nil {
		return nil, "", err
	}
	deltaURL := c.baseStr + chanPath(teamID, channelID) + "/messages/delta?$top=" + itoa(clampTop(limit))
	if deltaToken != "" && c.validDeltaLink(deltaToken, teamID, channelID) {
		deltaURL = deltaToken
	}
	msgs, newDelta, err := c.delta(ctx, deltaURL, teamID, channelID, since)
	if err == nil {
		return msgs, newDelta, nil
	}
	switch statusOf(err) {
	case 400, 404, 405, 501:
		msgs, err = c.listChannelFallback(ctx, teamID, channelID, since, limit)
		return msgs, "", err
	}
	return nil, "", err
}

// validDeltaLink accepts only a delta link on our own host whose path is this
// channel's delta route, so a tampered state file cannot redirect requests.
func (c *Client) validDeltaLink(link, teamID, channelID string) bool {
	if !c.sameOrigin(link) {
		return false
	}
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	want := c.base.Path + chanPath(teamID, channelID) + "/messages/delta"
	return strings.TrimSuffix(u.EscapedPath(), "/") == want || strings.TrimSuffix(u.Path, "/") == want
}

func (c *Client) delta(ctx context.Context, first, teamID, channelID string, since time.Time) ([]domain.RawMessage, string, error) {
	var out []domain.RawMessage
	newDelta := ""
	err := c.pages(ctx, first, "GET /teams/{id}/channels/{id}/messages/delta", func(p *page) bool {
		out = append(out, c.channelRaw(p, teamID, channelID, since)...)
		if p.Delta != "" && c.sameOrigin(p.Delta) {
			newDelta = p.Delta
		}
		return false
	})
	if err != nil {
		return nil, "", err
	}
	return out, newDelta, nil
}

func (c *Client) channelRaw(p *page, teamID, channelID string, since time.Time) []domain.RawMessage {
	var out []domain.RawMessage
	for _, d := range p.Value {
		m := d.raw(true)
		if !since.IsZero() && !m.Modified.After(since) {
			continue
		}
		m.TeamID, m.ChannelID = teamID, channelID
		out = append(out, m)
	}
	return out
}

func (c *Client) listChannelFallback(ctx context.Context, teamID, channelID string, since time.Time, limit int) ([]domain.RawMessage, error) {
	u := c.baseStr + chanPath(teamID, channelID) + "/messages?$top=" + itoa(clampTop(limit))
	var out []domain.RawMessage
	err := c.pages(ctx, u, "GET /teams/{id}/channels/{id}/messages", func(p *page) bool {
		out = append(out, c.channelRaw(p, teamID, channelID, since)...)
		return limit > 0 && len(out) >= limit
	})
	if err != nil {
		return nil, err
	}
	return capNewest(out, limit), nil
}
