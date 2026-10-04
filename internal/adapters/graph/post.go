package graph

import (
	"context"
	"html"
	"strings"

	"github.com/stainedhead/teams-cli/internal/domain"
)

type postBody struct {
	Body     bodyJSON      `json:"body"`
	Mentions []mentionJSON `json:"mentions,omitempty"`
}

type bodyJSON struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type mentionJSON struct {
	ID          int    `json:"id"`
	MentionText string `json:"mentionText"`
	Mentioned   struct {
		User struct {
			ID               string `json:"id"`
			DisplayName      string `json:"displayName"`
			UserIdentityType string `json:"userIdentityType"`
		} `json:"user"`
	} `json:"mentioned"`
}

// MarkerAttr is the HTML attribute that carries the idempotency marker
// (FR-15, UA-9: unverified that it survives in a real tenant).
const MarkerAttr = "data-teams-cli-key"

// buildPost maps an OutMessage to the Graph request body. Mentions require
// html (the <at id="n"> tags are built by the domain); a marker forces html
// and escapes plain text so the span can be appended safely.
func buildPost(m domain.OutMessage) (postBody, error) {
	if strings.TrimSpace(m.Text) == "" {
		return postBody{}, domain.NewValidation("message text is empty", "")
	}
	if len(m.Mentions) > 0 && !m.HTML {
		return postBody{}, domain.NewValidation("mentions require an html message", "")
	}
	content, ctype := m.Text, "text"
	if m.HTML {
		ctype = "html"
	}
	if m.MarkerKey != "" {
		if !m.HTML {
			content = strings.ReplaceAll(html.EscapeString(content), "\n", "<br>")
			ctype = "html"
		}
		content += `<span ` + MarkerAttr + `="` + html.EscapeString(m.MarkerKey) + `"></span>`
	}
	pb := postBody{Body: bodyJSON{ContentType: ctype, Content: content}}
	for _, x := range m.Mentions {
		var mj mentionJSON
		mj.ID, mj.MentionText = x.ID, x.DisplayName
		mj.Mentioned.User.ID, mj.Mentioned.User.DisplayName, mj.Mentioned.User.UserIdentityType = x.AADID, x.DisplayName, "aadUser"
		pb.Mentions = append(pb.Mentions, mj)
	}
	return pb, nil
}

// PostChat posts to a chat. ASSUMPTION (UA-1, unverified against a real
// tenant): POST /chats/{id}/messages with {body, mentions}. Failures that
// prove the server did not process the request are domain.NotSent.
func (c *Client) PostChat(ctx context.Context, chatID string, m domain.OutMessage) (domain.PostResult, error) {
	if err := requireID("chat", chatID); err != nil {
		return domain.PostResult{}, err
	}
	pb, err := buildPost(m)
	if err != nil {
		return domain.PostResult{}, err
	}
	return c.post(ctx, c.baseStr+"/chats/"+seg(chatID)+"/messages", "POST /chats/{id}/messages", pb, "")
}

// PostChannel posts a channel message, or a reply when threadID is set.
func (c *Client) PostChannel(ctx context.Context, teamID, channelID, threadID string, m domain.OutMessage) (domain.PostResult, error) {
	if err := requireID("team", teamID); err != nil {
		return domain.PostResult{}, err
	}
	if err := requireID("channel", channelID); err != nil {
		return domain.PostResult{}, err
	}
	pb, err := buildPost(m)
	if err != nil {
		return domain.PostResult{}, err
	}
	u := c.baseStr + chanPath(teamID, channelID) + "/messages"
	op := "POST /teams/{id}/channels/{id}/messages"
	if threadID != "" {
		if err := requireID("thread", threadID); err != nil {
			return domain.PostResult{}, err
		}
		u += "/" + seg(threadID) + "/replies"
		op += "/{id}/replies"
	}
	return c.post(ctx, u, op, pb, threadID)
}

func (c *Client) post(ctx context.Context, u, op string, pb postBody, threadID string) (domain.PostResult, error) {
	var out msgDTO
	if err := c.postJSON(ctx, u, op, pb, &out); err != nil {
		// A 2xx whose body cannot be read is ambiguous, not NotSent.
		return domain.PostResult{}, err
	}
	res := domain.PostResult{MessageID: out.ID, ThreadID: threadID, Created: parseTime(out.Created)}
	if threadID == "" && strings.Contains(op, "channels") {
		res.ThreadID = out.ID
	}
	return res, nil
}
