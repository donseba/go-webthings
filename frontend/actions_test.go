package frontend_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/frontend"

	"github.com/donseba/go-router/middleware"
)

func post(back string) *http.Request {
	form := url.Values{"back": {back}}
	request := httptest.NewRequest(http.MethodPost, "/_/comments/post", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return request
}

func TestBack(t *testing.T) {
	cases := []struct {
		back  string
		extra url.Values
		want  string
	}{
		{"/en/news/hello", nil, "/en/news/hello?comment=sent"},
		{"/en/news/hello#comments", nil, "/en/news/hello?comment=sent#comments"},
		{"/en/news/hello?page=2#comments", url.Values{"code": {"AB12"}}, "/en/news/hello?code=AB12&comment=sent&page=2#comments"},
		{"//evil.example/x", nil, "/?comment=sent"},
		{"https://evil.example/", nil, "/?comment=sent"},
		{"/\\evil.example", nil, "/?comment=sent"},
	}

	for _, c := range cases {
		response := httptest.NewRecorder()
		frontend.BackWith(response, post(c.back), "comment", "sent", c.extra)

		if got := response.Header().Get("Location"); got != c.want {
			t.Errorf("%q: got %q, want %q", c.back, got, c.want)
		}
	}
}

// TestBackFallsBackToTheReferer checks that a post whose form cannot be
// read, such as one over the body limit, returns to the page it came from
// when the Referer is an address on this site, and to the home page when it
// is not.
func TestBackFallsBackToTheReferer(t *testing.T) {
	cases := []struct {
		referer, location, path string
	}{
		{"http://example.com/en/contact?page=2", "/en/contact?comment=invalid&page=2", "/en/contact"},
		{"http://EXAMPLE.com/nl/", "/nl/?comment=invalid", "/nl/"},
		{"http://evil.example/en/contact", "/?comment=invalid", ""},
		{"javascript:alert(1)", "/?comment=invalid", ""},
		{"", "/?comment=invalid", ""},
	}

	for _, c := range cases {
		oversized := url.Values{"back": {"/en/elsewhere"}, "text": {strings.Repeat("x", 100)}}
		request := httptest.NewRequest(http.MethodPost, "/_/comments/post", strings.NewReader(oversized.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Referer", c.referer)

		response := httptest.NewRecorder()
		request.Body = http.MaxBytesReader(response, request.Body, 64)
		frontend.Back(response, request, "comment", "invalid")

		if got := response.Header().Get("Location"); got != c.location {
			t.Errorf("%q: got %q, want %q", c.referer, got, c.location)
		}

		if got := frontend.BackPath(request); got != c.path {
			t.Errorf("%q: back path %q, want %q", c.referer, got, c.path)
		}
	}

	// A readable back field wins over the Referer.
	readable := post("/en/news/hello")
	readable.Header.Set("Referer", "http://example.com/en/other")

	response := httptest.NewRecorder()
	frontend.Back(response, readable, "comment", "sent")

	if got := response.Header().Get("Location"); got != "/en/news/hello?comment=sent" {
		t.Fatalf("back field: %q", got)
	}
}

func TestThrottle(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	limited := frontend.OnLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		frontend.Back(w, r, "comment", "throttled")
	}))

	handler := frontend.Throttle(2, time.Minute, ok, limited)

	var codes []int

	for range 3 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, post("/en/#form"))
		codes = append(codes, response.Code)

		if response.Code == http.StatusSeeOther && response.Header().Get("Location") != "/en/?comment=throttled#form" {
			t.Fatalf("limited answer: %s", response.Header().Get("Location"))
		}
	}

	if codes[0] != http.StatusNoContent || codes[1] != http.StatusNoContent || codes[2] != http.StatusSeeOther {
		t.Fatalf("codes: %v", codes)
	}

	// Without OnLimit the answer is 429.
	bare := frontend.Throttle(1, time.Minute, ok)
	bare.ServeHTTP(httptest.NewRecorder(), post("/"))

	response := httptest.NewRecorder()
	bare.ServeHTTP(response, post("/"))

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("bare limit: %d", response.Code)
	}
}

func TestClientIP(t *testing.T) {
	request := func(remote string, headers map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote

		for name, value := range headers {
			r.Header.Set(name, value)
		}

		return r
	}

	// Without trusted proxies the headers are ignored.
	spoofed := request("203.0.113.9:4000", map[string]string{"X-Forwarded-For": "198.51.100.1"})
	if got := frontend.ClientIP(spoofed); got != "203.0.113.9" {
		t.Fatalf("untrusted: %s", got)
	}

	// App installs go-router's RealIP middleware with Config.TrustedProxies.
	trust, err := middleware.TryRealIP(middleware.RealIPOptions{TrustedProxies: []string{"10.0.0.0/8", "127.0.0.1"}})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		remote  string
		headers map[string]string
		want    string
	}{
		{"10.1.2.3:5000", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		// A client cannot hide behind an address it puts in front itself.
		{"10.1.2.3:5000", map[string]string{"X-Forwarded-For": "1.1.1.1, 198.51.100.7, 10.0.0.5"}, "198.51.100.7"},
		{"127.0.0.1:5000", map[string]string{"X-Real-IP": "198.51.100.8"}, "198.51.100.8"},
		{"[::ffff:127.0.0.1]:5000", map[string]string{"X-Forwarded-For": "[2001:db8::1]:443"}, "2001:db8::1"},
		// Requests that do not come from a trusted proxy keep their address.
		{"203.0.113.9:4000", map[string]string{"X-Forwarded-For": "198.51.100.1"}, "203.0.113.9"},
		{"10.1.2.3:5000", map[string]string{"X-Forwarded-For": "not-an-ip"}, "10.1.2.3"},
	}

	for _, c := range cases {
		var got string

		trust(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = frontend.ClientIP(r)
		})).ServeHTTP(httptest.NewRecorder(), request(c.remote, c.headers))

		if got != c.want {
			t.Errorf("%s %v: got %s, want %s", c.remote, c.headers, got, c.want)
		}
	}
}

// TestAbsoluteURL checks that forwarding headers name the site's address
// only from trusted proxies.
func TestAbsoluteURL(t *testing.T) {
	trust, err := middleware.TryRealIP(middleware.RealIPOptions{TrustedProxies: []string{"192.0.2.0/24"}})
	if err != nil {
		t.Fatal(err)
	}

	visit := func(remote string, tls bool, headers map[string]string) string {
		request := httptest.NewRequest(http.MethodGet, "http://site.test/en/", nil)
		request.RemoteAddr = remote

		if tls {
			request = httptest.NewRequest(http.MethodGet, "https://site.test/en/", nil)
			request.RemoteAddr = remote
		}

		for name, value := range headers {
			request.Header.Set(name, value)
		}

		var got string

		trust(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = frontend.AbsoluteURL("", r, "/en/news")
		})).ServeHTTP(httptest.NewRecorder(), request)

		return got
	}

	forwarded := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "example.com"}

	cases := []struct {
		remote  string
		tls     bool
		headers map[string]string
		want    string
	}{
		{"203.0.113.5:1234", false, forwarded, "http://site.test/en/news"},
		{"203.0.113.5:1234", true, nil, "https://site.test/en/news"},
		{"192.0.2.1:1234", false, forwarded, "https://example.com/en/news"},
		{"192.0.2.1:1234", false, map[string]string{"X-Forwarded-Proto": "http, https"}, "https://site.test/en/news"},
		{
			"192.0.2.1:1234",
			false,
			map[string]string{"X-Forwarded-Proto": "gopher", "X-Forwarded-Host": "evil.test/path"},
			"http://site.test/en/news",
		},
		{"192.0.2.1:1234", false, nil, "http://site.test/en/news"},
	}

	for _, c := range cases {
		if got := visit(c.remote, c.tls, c.headers); got != c.want {
			t.Fatalf("%s %v %v: got %s, want %s", c.remote, c.tls, c.headers, got, c.want)
		}
	}

	if got := frontend.AbsoluteURL(
		"https://example.org/",
		httptest.NewRequest(http.MethodGet, "/", nil),
		"/a",
	); got != "https://example.org/a" {
		t.Fatalf("with a base: %s", got)
	}
}

// TestSecure checks that X-Forwarded-Proto marks a request secure only when
// a trusted proxy sent it.
func TestSecure(t *testing.T) {
	trust, err := middleware.TryRealIP(middleware.RealIPOptions{TrustedProxies: []string{"192.0.2.0/24"}})
	if err != nil {
		t.Fatal(err)
	}

	secure := func(remote, proto string) bool {
		request := httptest.NewRequest(http.MethodGet, "http://site.test/en/", nil)
		request.RemoteAddr = remote
		request.Header.Set("X-Forwarded-Proto", proto)

		var got bool

		trust(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = frontend.Secure(r)
		})).ServeHTTP(httptest.NewRecorder(), request)

		return got
	}

	if secure("203.0.113.5:1234", "https") {
		t.Error("an untrusted visitor's X-Forwarded-Proto counted")
	}

	if !secure("192.0.2.1:1234", "https") {
		t.Error("a trusted proxy's X-Forwarded-Proto did not count")
	}

	if secure("192.0.2.1:1234", "http") {
		t.Error("plain HTTP through a trusted proxy counted as secure")
	}
}

// TestHoneypot checks IsSpam telling a filled-in spam trap from an empty
// one.
func TestHoneypot(t *testing.T) {
	t.Parallel()

	if frontend.IsSpam(post("/en/")) {
		t.Fatal("a post without the trap is spam")
	}

	spam := url.Values{"back": {"/en/"}, frontend.Honeypot: {"https://spam.example"}}
	request := httptest.NewRequest(http.MethodPost, "/_/comments/post", strings.NewReader(spam.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if !frontend.IsSpam(request) {
		t.Fatal("a filled-in trap is not spam")
	}
}
