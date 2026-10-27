// Package testhooks holds switches that only test helpers flip, to make
// expensive work cheap in tests. Production code never sets them, and sites
// cannot reach them: the package is internal and only sitetest imports it.
package testhooks

import "sync/atomic"

var cheapPasswords atomic.Bool

// UseCheapPasswords makes auth hash passwords with the smallest argon2id cost
// instead of the production cost of about 70ms and 64 MiB per hash. Every
// site a test starts hashes and checks its admin's password, so this saves
// seconds per test package. Call it before the first password is hashed:
// hashes made with one cost do not verify with the other.
func UseCheapPasswords() {
	cheapPasswords.Store(true)
}

// CheapPasswords reports whether UseCheapPasswords was called.
func CheapPasswords() bool {
	return cheapPasswords.Load()
}
