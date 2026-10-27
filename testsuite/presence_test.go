package testsuite

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/sitetest"
)

// TestAdminPresence follows presence through the whole app: the stream
// passes compression and the access log unbuffered, a colleague who opens
// the same post in another language shows up in the editor's stream and
// header, and closing the app ends the stream.
func TestAdminPresence(t *testing.T) {
	t.Parallel()

	s := sitetest.Start(t, publishing.Module("news", "News"))

	post := s.Submit("/admin/news/new?locale=en", url.Values{
		"Title": {"Harbour festival"},
		"Slug":  {"harbour-festival"},
		"Date":  {"2026-10-01"},
		"Body":  {"Boats and music."},
	})

	s.Submit("/admin/accounts", url.Values{
		"Email":    {"colleague@example.test"},
		"Name":     {"Cora Colleague"},
		"Password": {"colleague long passphrase"},
		"Role":     {"admin"},
	})

	colleague := s.As("colleague@example.test", "colleague long passphrase")

	// The editor's page names the stream of the page presence follows,
	// without the language.
	_, page := s.Get(post + "?locale=en")
	if !strings.Contains(page, `data-partial-stream="/admin/presence?`+url.Values{"page": {post}}.Encode()+`"`) {
		t.Fatalf("the editor lacks its presence page:\n%s", page)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	query := url.Values{"tab": {"editor-tab-1"}, "page": {post + "?locale=en"}}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "/admin/presence?"+query.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Accept-Encoding", "gzip")

	response := s.Request(request)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" {
		t.Fatalf("stream: %d, encoding %q", response.StatusCode, response.Header.Get("Content-Encoding"))
	}

	lines := make(chan string, 64)

	go func() {
		defer close(lines)

		reader := bufio.NewReader(response.Body)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			lines <- line
		}
	}()

	// waitFor reads the stream until a line contains part.
	waitFor := func(part string) {
		t.Helper()

		timeout := time.After(5 * time.Second)

		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("the stream ended before %q", part)
				}

				if strings.Contains(line, part) {
					return
				}
			case <-timeout:
				t.Fatalf("no %q within 5 seconds", part)
			}
		}
	}

	waitFor(`"target":"#admin-presence"`)

	// The colleague opens the post in Dutch: the tab reports its stream,
	// as go-partial's exp/sse client does.
	status, _, _ := colleague.Send("/admin/presence", url.Values{
		"clientId": {"colleague-tab-1"},
		"reason":   {"beat"},
		"params":   {url.Values{"page": {post + "?locale=nl"}, "tab": {"colleague-tab-1"}}.Encode()},
	})
	if status != http.StatusNoContent {
		t.Fatalf("report: %d", status)
	}

	waitFor(`Cora Colleague is also on this page`)

	if _, page := s.Get(post + "?locale=nl"); !strings.Contains(page, `>CC</span>`) {
		t.Fatal("the editor's header lacks the colleague")
	}

	// Closing the app ends the stream, so a shutdown need not wait for it.
	if err := s.App.Close(); err != nil {
		t.Logf("close: %v", err)
	}

	timeout := time.After(5 * time.Second)

	for {
		select {
		case _, ok := <-lines:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("the stream outlived the app")
		}
	}
}
