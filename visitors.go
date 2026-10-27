package webthings

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/frontend"
)

// visitorSecret names the instance secret VisitorID hashes with.
const visitorSecret = "visitors"

// VisitorID identifies the visitor of r without storing their address: an
// HMAC-SHA256 of the address (frontend.ClientIP), with the instance's
// "visitors" Secret as key, as 64 hexadecimal digits. Nobody can trace it
// back to the address by trying all addresses, and it differs between
// module instances.
//
// parts are hashed before the address, joined with "|", to narrow the
// identifier: with the date (time.DateOnly) it changes every day, so
// messages of one day can be grouped by sender without following anyone
// for longer. Modules that keep the identifier, such as a poll counting one
// vote per visitor, may store a prefix of it.
func (e Env) VisitorID(ctx context.Context, r *http.Request, parts ...string) (string, error) {
	key, err := e.Secret(ctx, visitorSecret)
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(strings.Join(slices.Concat(parts, []string{frontend.ClientIP(r)}), "|")))

	return hex.EncodeToString(mac.Sum(nil)), nil
}
