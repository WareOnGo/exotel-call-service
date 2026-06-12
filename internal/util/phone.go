package util

import "strings"

// NormalizePhone reduces a phone number to a stable match key.
//
// Exotel returns numbers in varied shapes ("0XXXXXXXXXX", "+91XXXXXXXXXX",
// "91XXXXXXXXXX", "XXXXXXXXXX"). For matching we strip everything non-digit
// and keep the last 10 digits (the subscriber number for Indian numbers).
// This makes "+91 XXXXX XXXXX" and "0XXXXXXXXXX" collide on the same key.
//
// NOTE: if you handle non-Indian / variable-length numbers, replace this with
// libphonenumber (github.com/nyaruka/phonenumbers) and store E.164.
func NormalizePhone(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if len(digits) > 10 {
		return digits[len(digits)-10:]
	}
	return digits
}

// ToE164India formats a number as Exotel's Connect applet expects in the dial
// response: "+91" + the 10-digit subscriber number. Returns "" if the input
// can't be reduced to 10 digits (caller should skip it).
func ToE164India(raw string) string {
	d := NormalizePhone(raw)
	if len(d) != 10 {
		return ""
	}
	return "+91" + d
}
