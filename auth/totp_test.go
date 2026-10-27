package auth_test

import (
	"testing"
	"time"

	"github.com/donseba/go-webthings/auth"
)

// rfcSecret is the secret of the test vectors of RFC 4226 and RFC 6238
// (SHA1).
var rfcSecret = []byte("12345678901234567890")

// TestHOTPVectors checks the codes of the 30-second steps 0 to 9 against
// the HOTP test vectors of RFC 4226, appendix D, whose counters they are.
func TestHOTPVectors(t *testing.T) {
	t.Parallel()

	want := []string{
		"755224", "287082", "359152", "969429", "338314",
		"254676", "287922", "162583", "399871", "520489",
	}

	for counter, code := range want {
		if got := auth.TOTP(rfcSecret, time.Unix(int64(counter)*30, 0)); got != code {
			t.Errorf("counter %d: %s, want %s", counter, got, code)
		}
	}
}

// TestTOTPVectors checks the SHA1 test vectors of RFC 6238, appendix B,
// truncated to the 6 digits authenticator apps show: 94287082 becomes
// 287082.
func TestTOTPVectors(t *testing.T) {
	t.Parallel()

	want := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}

	for seconds, code := range want {
		if got := auth.TOTP(rfcSecret, time.Unix(seconds, 0)); got != code {
			t.Errorf("%d: %s, want %s", seconds, got, code)
		}
	}
}

// TestKeyURI checks the otpauth address of the QR code and the secret as
// typed by hand.
func TestKeyURI(t *testing.T) {
	t.Parallel()

	if got := auth.SecretText(rfcSecret); got != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" {
		t.Fatalf("secret text: %s", got)
	}

	got := auth.KeyURI("My Site: Admin", "anna@example.com", rfcSecret)
	want := "otpauth://totp/My%20Site%3A%20Admin:anna@example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" +
		"&issuer=My%20Site%3A%20Admin&algorithm=SHA1&digits=6&period=30"

	if got != want {
		t.Fatalf("key URI:\n%s\nwant\n%s", got, want)
	}
}
