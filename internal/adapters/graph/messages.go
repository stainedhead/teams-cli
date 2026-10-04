package graph

import (
	"context"
	"sort"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// Me returns the agent's own profile. ASSUMPTION (UA-2, unverified against a
// real tenant): userPrincipalName matches the policy upn.
func (c *Client) Me(ctx context.Context) (domain.Profile, error) {
	var raw struct {
		ID   string `json:"id"`
		Name string `json:"displayName"`
		UPN  string `json:"userPrincipalName"`
	}
	if err := c.getJSON(ctx, c.baseStr+"/me?$select=id,displayName,userPrincipalName", "GET /me", &raw); err != nil {
		return domain.Profile{}, err
	}
	return domain.Profile{ID: raw.ID, DisplayName: raw.Name, UPN: raw.UPN}, nil
}

// GetChat probes GET /chats/{id}; it returns nil only on success. A chat the
// agent is not in yields a typed 403 or 404 error (UA-8).
func (c *Client) GetChat(ctx context.Context, chatID string) error {
	if err := requireID("chat", chatID); err != nil {
		return err
	}
	var v map[string]any
	return c.getJSON(ctx, c.baseStr+"/chats/"+seg(chatID), "GET /chats/{id}", &v)
}

func sinceFilter(since time.Time) string {
	if since.IsZero() {
		return ""
	}
	return "lastModifiedDateTime gt " + since.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// ListChatMessages lists chat messages modified after since, newest first, at
// most limit, following at most MaxPages pages. ASSUMPTION (UA-1, UA-7,
// unverified against a real tenant): $orderby=lastModifiedDateTime desc with
// $filter=lastModifiedDateTime gt T is accepted.
func (c *Client) ListChatMessages(ctx context.Context, chatID string, since time.Time, limit int) ([]domain.RawMessage, error) {
	if err := requireID("chat", chatID); err != nil {
		return nil, err
	}
	top := clampTop(limit)
	u := c.baseStr + "/chats/" + seg(chatID) + "/messages?$top=" + itoa(top) +
		"&$orderby=" + esc("lastModifiedDateTime desc")
	if f := sinceFilter(since); f != "" {
		u += "&$filter=" + esc(f)
	}
	var out []domain.RawMessage
	err := c.pages(ctx, u, "GET /chats/{id}/messages", func(p *page) bool {
		for _, d := range p.Value {
			m := d.raw(false)
			m.ChatID = chatID
			out = append(out, m)
		}
		return limit > 0 && len(out) >= limit
	})
	if err != nil {
		return nil, err
	}
	return capNewest(out, limit), nil
}

// ListReplies lists replies of a channel message, oldest first, at most limit.
func (c *Client) ListReplies(ctx context.Context, teamID, channelID, messageID string, limit int) ([]domain.RawMessage, error) {
	for _, x := range [][2]string{{"team", teamID}, {"channel", channelID}, {"message", messageID}} {
		if err := requireID(x[0], x[1]); err != nil {
			return nil, err
		}
	}
	u := c.baseStr + chanPath(teamID, channelID) + "/messages/" + seg(messageID) +
		"/replies?$top=" + itoa(clampTop(limit))
	var out []domain.RawMessage
	err := c.pages(ctx, u, "GET /teams/{id}/channels/{id}/messages/{id}/replies", func(p *page) bool {
		for _, d := range p.Value {
			m := d.raw(true)
			m.ThreadID = messageID
			m.TeamID, m.ChannelID = teamID, channelID
			out = append(out, m)
		}
		return limit > 0 && len(out) >= limit
	})
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// capNewest sorts newest-modified first and truncates to limit (limit <= 0 is
// unbounded).
func capNewest(in []domain.RawMessage, limit int) []domain.RawMessage {
	sort.SliceStable(in, func(i, j int) bool { return in[i].Modified.After(in[j].Modified) })
	if limit > 0 && len(in) > limit {
		in = in[:limit]
	}
	return in
}
