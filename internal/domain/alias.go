package domain

// ParseAlias parses a policy alias. Stub: implemented by WS-A.
func ParseAlias(s string) (Alias, error) { return Alias(s), nil }

// Kind returns the alias kind. Stub: implemented by WS-A.
func (a Alias) Kind() Kind { return "" }

// Name returns the alias name. Stub: implemented by WS-A.
func (a Alias) Name() string { return "" }
