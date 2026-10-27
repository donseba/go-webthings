package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/donseba/go-webthings/internal/testhooks"

	"golang.org/x/crypto/argon2"
)

// argonCost is the argon2id cost of a password hash; it is written into the
// stored value as its prefix.
type argonCost struct {
	time    uint32
	memory  uint32
	threads uint8
}

// Fixed, versioned parameters make verification bounded even if a stored value
// is malformed.
var productionCost = argonCost{time: 3, memory: 64 * 1024, threads: 2}

// testCost is the cheapest argon2id cost, used only when a test helper asks
// for it through testhooks.
var testCost = argonCost{time: 1, memory: 8, threads: 1}

func passwordCost() argonCost {
	if testhooks.CheapPasswords() {
		return testCost
	}

	return productionCost
}

// prefix is how the cost starts a stored hash, for example
// "argon2id$3$65536$2$".
func (c argonCost) prefix() string {
	return "argon2id$" + strconv.FormatUint(uint64(c.time), 10) + "$" +
		strconv.FormatUint(uint64(c.memory), 10) + "$" +
		strconv.FormatUint(uint64(c.threads), 10) + "$"
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)

	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	cost := passwordCost()
	hash := argon2.IDKey([]byte(password), salt, cost.time, cost.memory, cost.threads, 32)

	return cost.prefix() + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func verifyPassword(password, encoded string) bool {
	cost := passwordCost()
	prefix := cost.prefix()

	if len(password) > 1024 || !strings.HasPrefix(encoded, prefix) {
		return false
	}

	parts := strings.Split(strings.TrimPrefix(encoded, prefix), "$")

	if len(parts) != 2 {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[0])

	if err != nil || len(salt) != 16 {
		return false
	}

	want, err := base64.RawStdEncoding.DecodeString(parts[1])

	if err != nil || len(want) != 32 {
		return false
	}

	got := argon2.IDKey([]byte(password), salt, cost.time, cost.memory, cost.threads, 32)

	return subtle.ConstantTimeCompare(got, want) == 1
}
