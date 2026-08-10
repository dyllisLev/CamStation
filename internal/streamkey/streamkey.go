package streamkey

import (
	"regexp"
	"unicode/utf8"
)

const MaxBytes = 128

var validPattern = regexp.MustCompile(`^[\p{L}\p{N}_](?:[\p{L}\p{N}_-]*[\p{L}\p{N}_])?$`)

// Valid reports whether value is safe to use as a persistent camera key,
// URL path value, and go2rtc stream-name prefix.
func Valid(value string) bool {
	return value != "" && len(value) <= MaxBytes && utf8.ValidString(value) && validPattern.MatchString(value)
}
