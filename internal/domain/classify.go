package domain

// Classify classifies a raw sender (FR-19). Stub: implemented by WS-A.
func Classify(p Policy, s RawSender) Sender { return Sender{} }

// SelectHandle applies the inbound handle rules (D4). Stub.
func SelectHandle(p Policy, d Destination, m RawMessage, agentID string) (InboundHandle, bool) {
	return "", false
}

// Normalize turns a raw message into an inbound item or a drop reason. Stub.
func Normalize(p Policy, d Destination, m RawMessage, agentID string) (InboundItem, NormalizeOutcome) {
	return InboundItem{}, NormalizeOutcome{}
}
