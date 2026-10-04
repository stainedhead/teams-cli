package graph

import (
	"encoding/json"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// msgDTO is the Graph chatMessage shape. Every nested object may be null.
type msgDTO struct {
	ID          string          `json:"id"`
	MessageType string          `json:"messageType"`
	ReplyToID   *string         `json:"replyToId"`
	Created     *string         `json:"createdDateTime"`
	Modified    *string         `json:"lastModifiedDateTime"`
	DeletedAt   *string         `json:"deletedDateTime"`
	Removed     json.RawMessage `json:"@removed"`
	From        *struct {
		User *struct {
			ID       string  `json:"id"`
			Name     string  `json:"displayName"`
			TenantID *string `json:"tenantId"`
		} `json:"user"`
		Application *struct {
			ID       string `json:"id"`
			Name     string `json:"displayName"`
			Identity string `json:"applicationIdentityType"`
		} `json:"application"`
	} `json:"from"`
	Body *struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	Mentions []struct {
		Text      string `json:"mentionText"`
		Mentioned *struct {
			User *struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"mentioned"`
	} `json:"mentions"`
}

func parseTime(p *string) time.Time {
	if p == nil || *p == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, *p)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// raw maps the DTO to a domain.RawMessage without applying any policy. Fields
// Graph sends as null become zero values. threadRoot says top-level channel
// messages are their own thread.
func (d msgDTO) raw(threadRoot bool) domain.RawMessage {
	m := domain.RawMessage{
		ID:          d.ID,
		MessageType: d.MessageType,
		Created:     parseTime(d.Created),
		Modified:    parseTime(d.Modified),
		Deleted:     d.DeletedAt != nil && *d.DeletedAt != "" || len(d.Removed) > 0 && string(d.Removed) != "null",
		FromKind:    domain.SenderUnknown,
	}
	if m.Modified.IsZero() {
		m.Modified = m.Created
	}
	if threadRoot {
		m.ThreadID = d.ID
		if d.ReplyToID != nil && *d.ReplyToID != "" {
			m.ThreadID = *d.ReplyToID
		}
	}
	if f := d.From; f != nil {
		switch {
		case f.User != nil:
			m.FromKind, m.FromUserID, m.FromName = domain.SenderUser, f.User.ID, f.User.Name
			m.FromTenantID = str(f.User.TenantID)
		case f.Application != nil:
			m.FromKind, m.FromUserID, m.FromName = domain.SenderApplication, f.Application.ID, f.Application.Name
			if f.Application.Identity == "bot" {
				m.FromKind = domain.SenderBot
			}
		}
	}
	if d.Body != nil {
		m.BodyType, m.BodyContent = d.Body.ContentType, d.Body.Content
	}
	for _, x := range d.Mentions {
		if x.Mentioned == nil || x.Mentioned.User == nil {
			continue
		}
		m.Mentions = append(m.Mentions, domain.RawMention{UserID: x.Mentioned.User.ID, Text: x.Text})
	}
	return m
}

// page is one collection response.
type page struct {
	Value []msgDTO `json:"value"`
	Next  string   `json:"@odata.nextLink"`
	Delta string   `json:"@odata.deltaLink"`
}
