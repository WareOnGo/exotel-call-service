package util

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := []struct{ in, want string }{
		{"09876543210", "9876543210"},       // leading trunk 0
		{"+919876543210", "9876543210"},     // E.164
		{"919876543210", "9876543210"},      // country code, no +
		{"9876543210", "9876543210"},        // bare 10-digit
		{"+91 98765 43210", "9876543210"},   // spaces
		{"(+91)-98765-43210", "9876543210"}, // punctuation
		{"", ""},                            // empty
		{"123", "123"},                      // short (left as-is)
		{"abc", ""},                         // no digits
	}
	for _, c := range cases {
		if got := NormalizePhone(c.in); got != c.want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToE164India(t *testing.T) {
	cases := []struct{ in, want string }{
		{"09876543210", "+919876543210"},
		{"9876543210", "+919876543210"},
		{"+919876543210", "+919876543210"},
		{"919876543210", "+919876543210"},
		{"123", ""}, // can't form 10 digits -> skipped
		{"", ""},    // empty
		{"abc", ""}, // no digits
	}
	for _, c := range cases {
		if got := ToE164India(c.in); got != c.want {
			t.Errorf("ToE164India(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Two inputs that differ only by formatting must map to the same key — this is
// what makes sticky routing work regardless of how Exotel formats the caller.
func TestNormalizePhoneStability(t *testing.T) {
	a := NormalizePhone("+918111122223")
	b := NormalizePhone("08111122223")
	if a != b {
		t.Fatalf("expected same key, got %q vs %q", a, b)
	}
}
