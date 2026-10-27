package auth_test

import (
	"os"
	"testing"

	"github.com/donseba/go-webthings/internal/testhooks"
)

// TestMain hashes passwords cheaply: the production cost makes every sign-in
// in these tests take about 70ms. password_test.go checks the production cost.
func TestMain(m *testing.M) {
	testhooks.UseCheapPasswords()

	os.Exit(m.Run())
}
