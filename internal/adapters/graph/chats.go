package graph

import (
	"context"
	"github.com/stainedhead/agent-cli-core/output"
	"net/http"
	"strings"

	"github.com/stainedhead/teams-cli/internal/domain"
)

type chatDTO struct {
	ID      string `json:"id"`
	Members []struct {
		UserID string `json:"userId"`
	} `json:"members"`
}

type chatPage struct {
	Value []chatDTO `json:"value"`
	Next  string    `json:"@odata.nextLink"`
}

// ResolveUserChat finds the one-on-one chat with aadID by scanning the agent's
// chats (bounded by MaxChatScan). When none exists and create is true it
// creates one. With create false and no chat it returns a not-found
// domain error. ASSUMPTIONS (UA-6, UA-16, unverified against a real tenant):
// $expand=members works on /me/chats, and POST /chats returns the existing
// chat when one exists.
func (c *Client) ResolveUserChat(ctx context.Context, aadID string, create bool) (string, error) {
	if err := requireGUID(aadID); err != nil {
		return "", err
	}
	u := c.baseStr + "/me/chats?$filter=" + esc("chatType eq 'oneOnOne'") + "&$expand=members&$top=" + itoa(clampTop(c.maxScan))
	scanned := 0
	for i := 0; i < c.maxPages && u != "" && scanned < c.maxScan; i++ {
		var p chatPage
		if err := c.getJSON(ctx, u, "GET /me/chats", &p); err != nil {
			return "", err
		}
		for _, ch := range p.Value {
			if scanned++; scanned > c.maxScan {
				break
			}
			if chatHasUser(ch, aadID) {
				return ch.ID, nil
			}
		}
		u = p.Next
		if u != "" && !c.sameOrigin(u) {
			return "", &apiError{cat: output.CategoryValidation, msg: "graph GET /me/chats: refused next page link outside the graph host"}
		}
	}
	if !create {
		return "", domain.NewNotFound("no one-on-one chat with that user", "set create_chat: true for the destination to allow creating one")
	}
	return c.createChat(ctx, aadID)
}

func chatHasUser(ch chatDTO, aadID string) bool {
	for _, m := range ch.Members {
		if strings.EqualFold(m.UserID, aadID) {
			return true
		}
	}
	return false
}

func (c *Client) createChat(ctx context.Context, aadID string) (string, error) {
	me, err := c.Me(ctx)
	if err != nil {
		return "", err
	}
	if err := requireGUID(me.ID); err != nil {
		return "", err
	}
	member := func(id string) map[string]any {
		return map[string]any{
			"@odata.type":     "#microsoft.graph.aadUserConversationMember",
			"roles":           []string{"owner"},
			"user@odata.bind": c.baseStr + "/users('" + id + "')",
		}
	}
	body := map[string]any{"chatType": "oneOnOne", "members": []any{member(me.ID), member(aadID)}}
	var out chatDTO
	if err := c.postJSON(ctx, c.baseStr+"/chats", "POST /chats", body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", &apiError{cat: output.CategoryValidation, msg: "graph POST /chats: malformed response", status: http.StatusOK}
	}
	return out.ID, nil
}

// requireGUID checks an AAD object id before it is placed in an OData literal.
func requireGUID(id string) error {
	if len(id) != 36 {
		return domain.NewUsage("user id is not a valid id", "")
	}
	for i := 0; i < len(id); i++ {
		b := id[i]
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if b != '-' {
				return domain.NewUsage("user id is not a valid id", "")
			}
		case (b < '0' || b > '9') && (b < 'a' || b > 'f') && (b < 'A' || b > 'F'):
			return domain.NewUsage("user id is not a valid id", "")
		}
	}
	return nil
}
