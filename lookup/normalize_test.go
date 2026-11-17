package lookup

import (
	"testing"
)

func TestNormalizeBarcode(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"abc", "", false},
		{"1", "00000001", true},
		{"1234567", "01234567", true},
		{"12 345 67", "01234567", true},
		{"12345678", "12345678", true},
		{"123456789", "0000123456789", true},
		{"123456789012", "0123456789012", true},
		{"1234-5678-9012", "0123456789012", true},
		{"1234567890123", "1234567890123", true},
		{"12345678901234", "12345678901234", true},
		{"  004  ", "00000004", true},
	}
	for _, tc := range cases {
		got, err := NormalizeBarcode(tc.in)
		if tc.ok && err != nil {
			t.Errorf("NormalizeBarcode(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if !tc.ok && err == nil {
			t.Errorf("NormalizeBarcode(%q) = %q, want error", tc.in, got)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("NormalizeBarcode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
