package auth

import (
	"strings"
	"testing"
)

// TestPasswordCost guards the production cost: changing it would make every
// stored password fail to verify.
func TestPasswordCost(t *testing.T) {
	if got := productionCost.prefix(); got != "argon2id$3$65536$2$" {
		t.Fatalf("production cost changed: %s", got)
	}

	// The tests in this package run with the cheap cost (see main_test.go).
	hash, err := hashPassword("secret passphrase")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(hash, testCost.prefix()) || !verifyPassword("secret passphrase", hash) {
		t.Fatalf("cheap hash: %s", hash)
	}

	if verifyPassword("wrong passphrase", hash) {
		t.Fatal("wrong password verified")
	}

	// A hash made with another cost never verifies, so the cost cannot be
	// lowered through a stored value.
	other := productionCost.prefix() + strings.TrimPrefix(hash, testCost.prefix())
	if verifyPassword("secret passphrase", other) {
		t.Fatal("hash with another cost verified")
	}
}

// TestPasswordSpacesCount checks that a password is taken exactly as typed,
// as NIST SP 800-63B asks: spaces at the start and end are part of it, so
// " geheim123 " and "geheim123" are different passwords.
func TestPasswordSpacesCount(t *testing.T) {
	spaced, err := hashPassword(" geheim123 ")
	if err != nil {
		t.Fatal(err)
	}

	plain, err := hashPassword("geheim123")
	if err != nil {
		t.Fatal(err)
	}

	if !verifyPassword(" geheim123 ", spaced) || !verifyPassword("geheim123", plain) {
		t.Fatal("a password does not match its own hash")
	}

	if verifyPassword("geheim123", spaced) {
		t.Error(`"geheim123" matches the hash of " geheim123 "`)
	}

	if verifyPassword(" geheim123 ", plain) {
		t.Error(`" geheim123 " matches the hash of "geheim123"`)
	}
}
