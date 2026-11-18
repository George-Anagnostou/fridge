package lookup

import (
	"errors"
	"strings"
	"unicode"
)

var ErrInvalidBarcode = errors.New("invalid barcode")

// NormalizeBarcode strips non-digits, left-pads 1–7 digits to 8 and
// 9–12 digits to 13, and leaves 8 or 13+ digits unchanged. Empty input
// (or input with no digits) is rejected.
func NormalizeBarcode(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits == "" {
		return "", ErrInvalidBarcode
	}
	n := len(digits)
	switch {
	case n >= 1 && n <= 7:
		return strings.Repeat("0", 8-n) + digits, nil
	case n >= 9 && n <= 12:
		return strings.Repeat("0", 13-n) + digits, nil
	default:
		// 8 or 13+
		return digits, nil
	}
}
