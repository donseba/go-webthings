package auth

import "net/http"

// CookieName is the admin session cookie.
const CookieName = "wt_admin"

// Token reads the session token from the request cookie.
func Token(r *http.Request) string {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}

	return cookie.Value
}

// SetCookie stores the session token. secure must be false only for plain
// HTTP. The cookie reaches the whole site, so public pages can show editors
// an edit link, wherever the admin is; it stays HttpOnly and SameSite=Lax,
// and the admin refuses cross-origin form posts (see
// http.CrossOriginProtection).
func SetCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie removes the session cookie.
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
