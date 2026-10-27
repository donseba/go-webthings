package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// totpPeriod is the length of a step of time-based codes, in seconds.
	totpPeriod = 30
	// totpModulus keeps the 6 digits authenticator apps show.
	totpModulus = 1_000_000
	// secretSize is the length of a secret in bytes: the 160 bits RFC 4226
	// recommends for HMAC-SHA1.
	secretSize = 20
)

// secretEncoding is base32 (RFC 4648) without padding, as authenticator
// apps read secrets.
var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// TOTP returns the code an authenticator app shows for secret at time at
// (RFC 6238), with the settings every app uses: the HOTP value (RFC 4226,
// HMAC-SHA1) of the number of 30-second steps since the Unix epoch, as 6
// digits.
func TOTP(secret []byte, at time.Time) string {
	return hotp(secret, totpStep(at))
}

// totpStep is the number of the 30-second step at time at.
func totpStep(at time.Time) int64 {
	return at.Unix() / totpPeriod
}

// hotp is the HOTP value of secret for counter (RFC 4226, section 5.3),
// as 6 digits. Authenticator apps use HMAC-SHA1, which stays sound as an
// HMAC.
func hotp(secret []byte, counter int64) string {
	var message [8]byte

	binary.BigEndian.PutUint64(message[:], uint64(counter))

	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)

	// Dynamic truncation: four bytes from the offset that the last
	// nibble gives, without the sign bit.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%06d", value%totpModulus)
}

// matchTOTP returns the step whose code is code, for the step of now and
// one step either side, as long as it comes after last, the step whose code
// was used last: a code works once, and never after a later one.
func matchTOTP(secret []byte, code string, last int64, now time.Time) (int64, bool) {
	current := totpStep(now)

	for step := current - 1; step <= current+1; step++ {
		if step <= last {
			continue
		}

		if subtle.ConstantTimeCompare([]byte(hotp(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}

	return 0, false
}

// SecretText is secret as people type it into an authenticator app: base32
// (RFC 4648) without padding, such as GEZDGNBVGY3TQOJQ.
func SecretText(secret []byte) string {
	return secretEncoding.EncodeToString(secret)
}

// KeyURI is the otpauth address a QR code gives an authenticator app (the
// Key URI Format of Google Authenticator, which other apps read too): the
// issuer and account name the app shows, the secret, and the settings of
// TOTP. issuer is the site, such as its title; account the e-mail address.
func KeyURI(issuer, account string, secret []byte) string {
	label := labelText(issuer) + ":" + labelText(account)

	return "otpauth://totp/" + label +
		"?secret=" + SecretText(secret) +
		"&issuer=" + queryText(issuer) +
		"&algorithm=SHA1&digits=6&period=30"
}

// labelText escapes text for the label of KeyURI, its path: spaces as %20,
// which apps show as spaces, and colons, which separate the issuer from the
// account.
func labelText(text string) string {
	return strings.ReplaceAll(url.PathEscape(text), ":", "%3A")
}

// queryText escapes text for a parameter of KeyURI, with spaces as %20
// rather than +, which not every app reads as a space.
func queryText(text string) string {
	return strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}
