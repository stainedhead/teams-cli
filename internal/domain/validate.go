package domain

// ValidateText validates outgoing text (FR-5). Stub: implemented by WS-A.
func ValidateText(p Policy, text string) error { return nil }

// ValidateKey validates an idempotency key (FR-14). Stub.
func ValidateKey(k string) error { return nil }

// PayloadHash hashes a payload for idempotency comparison. Stub.
func PayloadHash(alias Alias, thread string, o OutMessage) string { return "" }
