package domain

import (
	"strings"
)

const maxAliasName = 64

// ParseAlias parses a policy alias of the form kind:name where kind is one of
// channel, chat, user and name matches [a-z0-9._-]{1,64}. Raw Teams ids, GUIDs
// and anything else are a usage error (exit 2, FR-3).
func ParseAlias(s string) (Alias, error) {
	kind, name, ok := strings.Cut(s, ":")
	if !ok {
		return "", NewUsage("invalid alias: expected kind:name", "use channel:<name>, chat:<name> or user:<name>; run `teams destinations list`")
	}
	switch Kind(kind) {
	case KindChannel, KindChat, KindUser:
	default:
		return "", NewUsage("invalid alias: unknown kind", "kind must be channel, chat or user; raw ids are not accepted")
	}
	if !validAliasName(name) {
		return "", NewUsage("invalid alias: bad name", "name must match [a-z0-9._-]{1,64}")
	}
	return Alias(s), nil
}

func validAliasName(n string) bool {
	if n == "" || len(n) > maxAliasName {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// Kind returns the alias kind (the part before the colon).
func (a Alias) Kind() Kind {
	k, _, _ := strings.Cut(string(a), ":")
	return Kind(k)
}

// Name returns the alias name (the part after the colon).
func (a Alias) Name() string {
	_, n, _ := strings.Cut(string(a), ":")
	return n
}
