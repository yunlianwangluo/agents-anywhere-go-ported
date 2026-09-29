package auth

import "crypto/subtle"

func Valid(expected, supplied string) bool {
	if expected == "" || supplied == "" || len(expected) != len(supplied) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(supplied)) == 1
}
