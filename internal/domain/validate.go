package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxKeyLen = 128

// ValidateText validates outgoing text (FR-5): size in UTF-8 bytes including
// the policy prefix, non-empty after trimming, valid UTF-8, and no NUL, CR or
// other control characters (newline and tab are allowed). Errors are
// validation errors (exit 9) and never echo the text.
func ValidateText(p Policy, text string) error {
	if max := p.Send.MaxBytes; max > 0 && len(p.Send.Prefix)+len(text) > max {
		return NewValidation(fmt.Sprintf("message exceeds %d bytes", max), "shorten the message; the policy prefix counts toward the limit")
	}
	if strings.TrimSpace(text) == "" {
		return NewValidation("message is empty", "provide non-blank text")
	}
	if !utf8.ValidString(text) {
		return NewValidation("message is not valid UTF-8", "")
	}
	for _, r := range text {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return NewValidation("message contains control characters", "remove NUL, CR and other control characters")
		}
	}
	return nil
}

// ValidateKey validates an idempotency key: 1-128 chars of [A-Za-z0-9._:-].
func ValidateKey(k string) error {
	if k == "" || len(k) > maxKeyLen {
		return NewValidation("invalid idempotency key length", "use 1-128 characters")
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == ':', c == '-':
		default:
			return NewValidation("invalid idempotency key character", "allowed characters are A-Z a-z 0-9 . _ : -")
		}
	}
	return nil
}

// PayloadHash returns a stable hex SHA-256 over the destination, thread and
// rendered message (text, html flag, mentions). The marker key is excluded so
// the hash depends on the payload only. Fields are length-prefixed so no two
// payloads share an encoding.
func PayloadHash(alias Alias, thread string, o OutMessage) string {
	h := sha256.New()
	w := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	w(string(alias))
	w(thread)
	w(o.Text)
	if o.HTML {
		w("html")
	} else {
		w("text")
	}
	for _, m := range o.Mentions {
		w(fmt.Sprint(m.ID))
		w(strings.ToLower(m.AADID))
		w(m.DisplayName)
	}
	return hex.EncodeToString(h.Sum(nil))
}
