package frontend

import (
	"bytes"
	"context"
	"encoding/xml"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-router/middleware"
)

// SitemapFunc lists a module's public paths in a locale, relative to the page
// the module is mounted on, for example "/hello-world".
type SitemapFunc func(ctx context.Context, locale string) ([]string, error)

type sitemapURL struct {
	Location   string            `xml:"loc"`
	Alternates []sitemapLanguage `xml:"xhtml:link"`
}

type sitemapLanguage struct {
	Rel      string `xml:"rel,attr"`
	Language string `xml:"hreflang,attr"`
	Href     string `xml:"href,attr"`
}

// Sitemap limits: a sitemap file may list at most 50,000 URLs, and a
// sitemap is built at most once per sitemapTTL while what it lists stays
// the same.
const (
	maxSitemapURLs = 50000
	sitemapTTL     = 10 * time.Second
)

// sitemapCache is the sitemap built last, for the origin and the version
// it was built for. The origin can come from the request, so only one is
// kept.
type sitemapCache struct {
	mu      sync.Mutex
	base    string
	version uint64
	body    []byte
	expires time.Time
}

// Sitemap serves /sitemap.xml: every published page in every language, with
// language alternates, plus the paths modules report below their pages. A
// module that fails is logged and left out, so one module cannot break the
// sitemap. It lists at most 50,000 URLs, the most a sitemap file may hold.
//
// version is the version of what the sitemap lists, such as the app's
// output cache version, which moves on with every change in this process
// of the published pages and the modules' content. The sitemap is built
// again as soon as it moved on, so a page published in this process is in
// the next sitemap, also one the output cache keeps for the new version.
// Otherwise it is built at most once every ten seconds. It lists the
// published routes as the page service keeps them in memory (see
// pages.Service.SetCacheTTL, webthings.Config.CacheTTL), so changes made by
// other processes show up within that cache time plus ten seconds. A nil
// version only counts the time.
func (f *Frontend) Sitemap(baseURL string, modules map[string]SitemapFunc, version func() uint64) http.Handler {
	var cache sitemapCache

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := origin(r, baseURL)

		var current uint64

		if version != nil {
			current = version()
		}

		body, err := cache.get(base, current, func() ([]byte, error) {
			return f.sitemap(r.Context(), base, modules)
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "sitemap", "error", err)
			http.Error(w, "internal_error", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write(body)
	})
}

// get returns the sitemap of base at version, building it when the one
// kept is for another origin or version, or older than sitemapTTL.
// Requests meanwhile wait for it rather than building their own. The
// version is read before the sitemap is built, so a change during the
// build makes the next request build it again.
func (c *sitemapCache) get(base string, version uint64, build func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.base == base && c.version == version && time.Now().Before(c.expires) {
		return c.body, nil
	}

	body, err := build()
	if err != nil {
		return nil, err
	}

	c.base = base
	c.version = version
	c.body = body
	c.expires = time.Now().Add(sitemapTTL)

	return body, nil
}

// sitemap builds the sitemap document of base.
func (f *Frontend) sitemap(ctx context.Context, base string, modules map[string]SitemapFunc) ([]byte, error) {
	urls, err := f.sitemapURLs(ctx, base, modules)
	if err != nil {
		return nil, err
	}

	document := struct {
		XMLName xml.Name     `xml:"urlset"`
		Space   string       `xml:"xmlns,attr"`
		XHTML   string       `xml:"xmlns:xhtml,attr"`
		URLs    []sitemapURL `xml:"url"`
	}{
		Space: "http://www.sitemaps.org/schemas/sitemap/0.9",
		XHTML: "http://www.w3.org/1999/xhtml",
		URLs:  urls,
	}

	var body bytes.Buffer

	body.WriteString(xml.Header)

	encoder := xml.NewEncoder(&body)
	encoder.Indent("", "  ")

	if err := encoder.Encode(document); err != nil {
		return nil, err
	}

	return body.Bytes(), nil
}

func (f *Frontend) sitemapURLs(ctx context.Context, base string, modules map[string]SitemapFunc) ([]sitemapURL, error) {
	snapshot, err := f.site.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	settings := snapshot.Settings()

	var urls []sitemapURL

	for _, locale := range settings.Locales {
		routes, err := f.pages.Routes(ctx, locale)
		if err != nil {
			return nil, err
		}

		for _, route := range routes {
			entry := sitemapURL{Location: base + link(locale, route.Path, "")}

			alternates, err := f.pages.Alternates(ctx, route.PageID)
			if err != nil {
				return nil, err
			}

			// In the order of the site's languages, so the file only changes
			// when the site does.
			for _, code := range settings.Locales {
				path, ok := alternates[code]
				if !ok {
					continue
				}

				entry.Alternates = append(entry.Alternates, sitemapLanguage{
					Rel:      "alternate",
					Language: code,
					Href:     base + link(code, path, ""),
				})
			}

			// The default language's address is also for visitors of
			// other languages.
			if path, ok := alternates[settings.DefaultLocale]; ok && len(entry.Alternates) > 1 {
				entry.Alternates = append(entry.Alternates, sitemapLanguage{
					Rel:      "alternate",
					Language: "x-default",
					Href:     base + link(settings.DefaultLocale, path, ""),
				})
			}

			urls = append(urls, entry)

			list, ok := modules[route.Module]
			if route.Module == "" || !ok {
				continue
			}

			paths, err := list(ctx, locale)
			if err != nil {
				slog.ErrorContext(ctx, "sitemap module", "module", route.Module, "locale", locale, "error", err)

				continue
			}

			for _, path := range paths {
				urls = append(urls, sitemapURL{Location: base + link(locale, route.Path, path)})
			}
		}
	}

	if len(urls) > maxSitemapURLs {
		slog.WarnContext(ctx, "sitemap too long: left out the last URLs", "urls", len(urls), "limit", maxSitemapURLs)

		urls = urls[:maxSitemapURLs]
	}

	return urls, nil
}

// Robots serves /robots.txt, which names the sitemap at sitemapPath, such as
// the path of the app's route named sitemap. It does not name the admin,
// whose path a site may choose to keep to itself: admin pages answer with
// X-Robots-Tag: noindex, which keeps them out of search results also when
// other sites link to them, as a Disallow line cannot.
func Robots(baseURL, sitemapPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("User-agent: *\nAllow: /\nSitemap: " + origin(r, baseURL) + sitemapPath + "\n"))
	})
}

// origin is the configured public URL, or the one the request arrived on
// (see AbsoluteURL).
func origin(r *http.Request, baseURL string) string {
	if baseURL != "" {
		return strings.TrimSuffix(baseURL, "/")
	}

	return middleware.Origin(r)
}
