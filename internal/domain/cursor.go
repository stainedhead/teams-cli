package domain

import (
	"sort"
	"strings"
	"time"
)

const cursorPrefix = "c1:"

// ItemID builds an alias-qualified CLI item id: "<alias>/<graph-id>" (D8).
func ItemID(a Alias, graphID string) string { return string(a) + "/" + graphID }

// ThreadID builds an alias-qualified CLI thread id: "<alias>/<root>" (D8).
// For chats pass the literal root "chat".
func ThreadID(a Alias, root string) string { return string(a) + "/" + root }

// ParseItemID splits a CLI item or thread id into its alias and the Graph id
// that follows the first slash. Aliases cannot contain a slash, so the first
// slash is unambiguous. Malformed ids are a validation error (exit 9).
func ParseItemID(id string) (alias Alias, graphID string, err error) {
	head, rest, ok := strings.Cut(id, "/")
	if !ok || rest == "" {
		return "", "", NewValidation("malformed id: expected <alias>/<id>", "use an id exactly as returned by `teams inbox`")
	}
	a, perr := ParseAlias(head)
	if perr != nil {
		return "", "", NewValidation("malformed id: invalid alias part", "use an id exactly as returned by `teams inbox`")
	}
	if len(rest) > 512 || strings.ContainsFunc(rest, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return "", "", NewValidation("malformed id: invalid id part", "use an id exactly as returned by `teams inbox`")
	}
	return a, rest, nil
}

// EncodeCursor encodes a modified time as an opaque cursor "c1:<RFC 3339 UTC>".
func EncodeCursor(t time.Time) string {
	return cursorPrefix + t.UTC().Format(time.RFC3339Nano)
}

// DecodeCursor decodes a cursor produced by EncodeCursor. A malformed cursor
// is a usage error (a bad --since).
func DecodeCursor(s string) (time.Time, error) {
	rest, ok := strings.CutPrefix(s, cursorPrefix)
	if !ok {
		return time.Time{}, NewUsage("invalid cursor", "pass a cursor exactly as returned by `teams inbox`")
	}
	t, err := time.Parse(time.RFC3339Nano, rest)
	if err != nil {
		return time.Time{}, NewUsage("invalid cursor", "pass a cursor exactly as returned by `teams inbox`")
	}
	return t.UTC(), nil
}

// Since computes the effective lower bound for listing a destination: the
// override (a replayed cursor) or the watermark, never older than the
// max_lookback floor (D8).
func Since(c CursorState, now time.Time, lookback time.Duration, override *time.Time) time.Time {
	floor := now.Add(-lookback)
	base := c.Watermark
	if override != nil {
		base = *override
	}
	if base.Before(floor) {
		return floor
	}
	return base
}

// graphIDOf returns the Graph id of a CLI item id, or the input when it is
// not alias-qualified.
func graphIDOf(id string) string {
	if _, g, err := ParseItemID(id); err == nil {
		return g
	}
	return id
}

func (c CursorState) ackFor(graphID string) (AckEntry, bool) {
	for _, a := range c.Acked {
		if a.ID == graphID {
			return a, true
		}
	}
	return AckEntry{}, false
}

// Undelivered returns the items still owed to the consumer, ordered by
// received time then id (D8): not acked at the same or a newer version
// (edited-after-ack items return with Edited set), not at or behind the
// watermark, and within the lookback window. Item modified times come from
// the item cursor, falling back to Received.
func (c CursorState) Undelivered(items []InboundItem, now time.Time, lookback time.Duration) []InboundItem {
	floor := now.Add(-lookback)
	var out []InboundItem
	for _, it := range items {
		mod, err := DecodeCursor(it.Cursor)
		if err != nil {
			mod = it.Received
		}
		if mod.Before(floor) {
			continue
		}
		if ack, ok := c.ackFor(graphIDOf(it.ID)); ok {
			if !mod.After(ack.Modified) {
				continue
			}
			it.Edited = true
		} else if !c.Watermark.IsZero() && !mod.After(c.Watermark) {
			continue
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Received.Equal(out[j].Received) {
			return out[i].Received.Before(out[j].Received)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Ack records acked versions and advances the watermark across the contiguous
// acked prefix of the delivery index (ordered by modified time; a group of
// equal times advances only when all of it is acked). It reports how many
// entries were newly acked and how many were already acked at that version or
// newer. Ids absent from the delivery index are the caller's concern. The
// result is independent of the order entries are applied in.
func (c CursorState) Ack(entries []AckEntry) (next CursorState, acked, already int) {
	next = c
	next.Acked = append([]AckEntry(nil), c.Acked...)
	next.Delivered = append([]DeliveryEntry(nil), c.Delivered...)
	for _, e := range entries {
		idx := -1
		for i, a := range next.Acked {
			if a.ID == e.ID {
				idx = i
				break
			}
		}
		switch {
		case idx < 0:
			next.Acked = append(next.Acked, e)
			acked++
		case e.Modified.After(next.Acked[idx].Modified):
			next.Acked[idx].Modified = e.Modified
			acked++
		default:
			already++
		}
	}
	sort.Slice(next.Acked, func(i, j int) bool {
		if !next.Acked[i].Modified.Equal(next.Acked[j].Modified) {
			return next.Acked[i].Modified.Before(next.Acked[j].Modified)
		}
		return next.Acked[i].ID < next.Acked[j].ID
	})
	next.Watermark = next.advance()
	return next, acked, already
}

func (c CursorState) advance() time.Time {
	pending := make([]DeliveryEntry, 0, len(c.Delivered))
	for _, d := range c.Delivered {
		if d.Modified.After(c.Watermark) {
			pending = append(pending, d)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Modified.Before(pending[j].Modified) })
	wm := c.Watermark
	for i := 0; i < len(pending); {
		j := i
		all := true
		for j < len(pending) && pending[j].Modified.Equal(pending[i].Modified) {
			a, ok := c.ackFor(pending[j].ID)
			if !ok || a.Modified.Before(pending[j].Modified) {
				all = false
			}
			j++
		}
		if !all {
			break
		}
		wm = pending[i].Modified
		i = j
	}
	return wm
}

// Record adds deliveries to the delivery index (one entry per message id,
// keeping the newest version), prunes entries delivered more than keep ago,
// and prunes acked entries that are older than keep and behind the watermark.
func (c CursorState) Record(d []DeliveryEntry, now time.Time, keep time.Duration) CursorState {
	byID := map[string]DeliveryEntry{}
	for _, e := range c.Delivered {
		byID[e.ID] = e
	}
	for _, e := range d {
		if old, ok := byID[e.ID]; ok && e.Modified.Before(old.Modified) {
			continue
		}
		byID[e.ID] = e
	}
	cutoff := now.Add(-keep)
	next := c
	next.Delivered = nil
	for _, e := range byID {
		if e.DeliveredAt.Before(cutoff) {
			continue
		}
		next.Delivered = append(next.Delivered, e)
	}
	sort.Slice(next.Delivered, func(i, j int) bool {
		if !next.Delivered[i].Modified.Equal(next.Delivered[j].Modified) {
			return next.Delivered[i].Modified.Before(next.Delivered[j].Modified)
		}
		return next.Delivered[i].ID < next.Delivered[j].ID
	})
	next.Acked = nil
	for _, a := range c.Acked {
		if a.Modified.Before(cutoff) && !a.Modified.After(c.Watermark) {
			continue
		}
		next.Acked = append(next.Acked, a)
	}
	next.Updated = now
	return next
}

// Known looks up a delivery entry by Graph message id.
func (c CursorState) Known(id string) (DeliveryEntry, bool) {
	for _, e := range c.Delivered {
		if e.ID == id {
			return e, true
		}
	}
	return DeliveryEntry{}, false
}
