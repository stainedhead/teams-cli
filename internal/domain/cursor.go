package domain

import "time"

// Undelivered filters items not yet acked or aged out (D8). Stub.
func (c CursorState) Undelivered(items []InboundItem, now time.Time, lookback time.Duration) []InboundItem {
	return nil
}

// Ack applies ack entries and advances the watermark. Stub.
func (c CursorState) Ack(entries []AckEntry) (next CursorState, acked, already int) {
	return c, 0, 0
}

// Record adds deliveries to the delivery index and prunes. Stub.
func (c CursorState) Record(d []DeliveryEntry, now time.Time, keep time.Duration) CursorState {
	return c
}

// Known looks up a delivery entry by message id. Stub.
func (c CursorState) Known(id string) (DeliveryEntry, bool) { return DeliveryEntry{}, false }

// ParseItemID splits a CLI item id into alias and Graph id (D8). Stub.
func ParseItemID(id string) (alias Alias, graphID string, err error) { return "", "", nil }

// ItemID builds a CLI item id. Stub.
func ItemID(a Alias, graphID string) string { return "" }

// ThreadID builds a CLI thread id. Stub.
func ThreadID(a Alias, root string) string { return "" }

// Since computes the effective lower bound for listing. Stub.
func Since(c CursorState, now time.Time, lookback time.Duration, override *time.Time) time.Time {
	return time.Time{}
}

// EncodeCursor encodes a cursor string. Stub.
func EncodeCursor(t time.Time) string { return "" }

// DecodeCursor decodes a cursor string. Stub.
func DecodeCursor(s string) (time.Time, error) { return time.Time{}, nil }
