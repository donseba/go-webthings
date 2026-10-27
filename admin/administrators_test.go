package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
)

// TestAdministratorsOnly checks that the middleware lets administrators
// through and refuses everyone else, also without a signed-in user.
func TestAdministratorsOnly(t *testing.T) {
	handler := admin.AdministratorsOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		name   string
		user   *auth.User
		status int
	}{
		{"administrator", &auth.User{Role: auth.Admin}, http.StatusNoContent},
		{"editor", &auth.User{Role: auth.Editor}, http.StatusForbidden},
		{"nobody", nil, http.StatusForbidden},
	} {
		request := httptest.NewRequest(http.MethodPost, "/admin/site/security", nil)
		if tc.user != nil {
			request = request.WithContext(auth.WithUser(request.Context(), *tc.user))
		}

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != tc.status {
			t.Fatalf("%s: %d, want %d", tc.name, response.Code, tc.status)
		}
	}
}
