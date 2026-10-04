package graph

import (
	"context"
	"strings"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

const markerScanSize = 20

// FindByMarker scans the last 20 messages of dest for an idempotency marker
// (FR-15). marker is the key hash placed in the data-teams-cli-key attribute.
// A 400 from Graph is inconclusive: (_, false, nil). ASSUMPTION (UA-9,
// unverified against a real tenant): the attribute survives in the stored body.
func (c *Client) FindByMarker(ctx context.Context, dest domain.Destination, marker string) (string, bool, error) {
	if marker == "" {
		return "", false, domain.NewUsage("marker is empty", "")
	}
	var msgs []domain.RawMessage
	var err error
	switch dest.Kind {
	case domain.KindChannel:
		msgs, err = c.listChannelFallback(ctx, dest.TeamID, dest.ChannelID, time.Time{}, markerScanSize)
	default:
		if dest.ChatID == "" {
			return "", false, nil // user chat not resolved: inconclusive
		}
		msgs, err = c.ListChatMessages(ctx, dest.ChatID, time.Time{}, markerScanSize)
	}
	if err != nil {
		if statusOf(err) == 400 {
			return "", false, nil
		}
		return "", false, err
	}
	for _, m := range msgs {
		if strings.Contains(m.BodyContent, MarkerAttr) && strings.Contains(m.BodyContent, marker) {
			return m.ID, true, nil
		}
	}
	return "", false, nil
}
