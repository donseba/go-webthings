package frontend

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/donseba/go-webthings/site"

	"github.com/donseba/go-partial/exp/localization"
	"github.com/donseba/go-router/middleware"
)

// Social media in templates: the site's profiles (Site settings, Social
// media profiles) and share links for any page.
//
//	{{range profiles}}<a href="{{.URL}}" rel="me" class="profile-{{.Network}}">{{.Label}}</a>{{end}}
//	{{range share "" .Title}}<a href="{{.URL}}" class="share-{{.Network}}">{{.Label}}</a>{{end}}
//	{{shareBar "" .Title}}
//
// The layout has the profiles as {{range .Site.Profiles}} too. share returns
// links for facebook, x, linkedin, whatsapp, telegram, email and bluesky; the
// address may be absolute, a path such as "/en/news/hello", or empty for the
// current page; paths become absolute with Config.BaseURL or the address of
// the request. shareBar renders them with the site/share component.
//
// Labels are the networks' English names (site.NetworkName) translated with
// the site's translations (Translations): the msgid is the name, such as
// "E-mail" or "Facebook", so a site can also write "Share on Facebook" there.

// Profile is one of the site's social media profiles as templates see it.
type Profile struct {
	// Network is one of site.Networks, for example "instagram", for an icon.
	Network string
	// Label is the network's name in the page's language.
	Label string
	URL   string
}

// ShareLink is a link that shares a page on one network.
type ShareLink struct {
	// Network is facebook, x, linkedin, whatsapp, telegram, email or bluesky.
	Network string
	// Label is the network's name; the share function translates it.
	Label string
	URL   string
}

// ShareLinks returns the share links for an absolute page address and its
// title, in a fixed order, with English labels.
func ShareLinks(page, title string) []ShareLink {
	u := escapeQuery(page)
	t := escapeQuery(title)
	message := escapeQuery(strings.TrimSpace(title + " " + page))

	links := []ShareLink{
		{Network: "facebook", URL: "https://www.facebook.com/sharer/sharer.php?u=" + u},
		{Network: "x", URL: "https://x.com/intent/tweet?text=" + t + "&url=" + u},
		{Network: "linkedin", URL: "https://www.linkedin.com/sharing/share-offsite/?url=" + u},
		{Network: "whatsapp", URL: "https://wa.me/?text=" + message},
		{Network: "telegram", URL: "https://t.me/share/url?url=" + u + "&text=" + t},
		{Network: "email", URL: "mailto:?subject=" + t + "&body=" + u},
		{Network: "bluesky", URL: "https://bsky.app/intent/compose?text=" + message},
	}

	for i := range links {
		links[i].Label = site.NetworkName(links[i].Network)
	}

	return links
}

// escapeQuery encodes a query value with %20 for spaces, which mail clients
// need in mailto: links and every network accepts.
func escapeQuery(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// SetBaseURL sets the site's public address, for example
// "https://example.com", for absolute links such as share links; empty uses
// the address of each request.
func (f *Frontend) SetBaseURL(base string) {
	f.baseURL = base
}

// AbsoluteURL makes path absolute with base, or with the address of r when
// base is empty; for links in e-mail and share links. The address of r is
// its own TLS state and Host header; X-Forwarded-Proto and X-Forwarded-Host,
// or else Forwarded, count only from trusted proxies
// (webthings.Config.TrustedProxies), as go-router's middleware.Origin reads
// them. A Host header is what the client sent, so sites set base in
// production.
func AbsoluteURL(base string, r *http.Request, path string) string {
	if base != "" {
		return strings.TrimSuffix(base, "/") + path
	}

	return middleware.Origin(r) + path
}

// absolute makes page absolute with the site's address. Relative addresses,
// and an empty one, are read from current, the path of the page being
// rendered; r is its request. A query and a fragment are kept, so "#top" is
// the current page's address with #top and "?page=2" the current page with
// that query; "" is the current page without its query. Addresses with a
// scheme or a host ("https://…", "mailto:…", "//cdn.example.com/…") stay as
// they are, and so does page without a request or a base, or without a
// current page to be relative to.
func absolute(page, base string, r *http.Request, current string) string {
	target, err := url.Parse(page)
	if err != nil || target.IsAbs() || target.Host != "" {
		return page
	}

	switch {
	case current != "":
		target = (&url.URL{Path: current}).ResolveReference(target)
	case target.Path == "":
		// "", "?page=2" or "#top" need the page they belong to.
		return page
	}

	path := target.RequestURI()
	if !strings.HasPrefix(path, "/") || r == nil && base == "" {
		return page
	}

	if target.Fragment != "" {
		path += "#" + target.EscapedFragment()
	}

	if r == nil {
		return strings.TrimSuffix(base, "/") + path
	}

	return AbsoluteURL(base, r, path)
}

// share is {{range share "" .Title}}: the share links of a page, labelled in
// the page's language.
func share(ctx context.Context, page, title string) []ShareLink {
	current, ok := renderingFrom(ctx)
	if !ok {
		return ShareLinks(page, title)
	}

	path := current.path
	if path == "" && current.request != nil {
		path = current.request.URL.Path
	}

	links := ShareLinks(absolute(page, current.frontend.baseURL, current.request, path), title)

	tr := current.frontend.tr()
	loc := localization.New(current.locale)

	for i := range links {
		links[i].Label = tr.Tl(loc, links[i].Label)
	}

	return links
}

// profilesFunc is {{range profiles}}: the site's social media profiles,
// labelled in the page's language.
func profilesFunc(ctx context.Context) []Profile {
	current, ok := renderingFrom(ctx)
	if !ok {
		return nil
	}

	snapshot, err := current.frontend.site.Snapshot(ctx)
	if err != nil {
		return nil
	}

	return current.frontend.profiles(snapshot.Settings(), current.locale)
}

// profiles are the site's profiles with labels in locale.
func (f *Frontend) profiles(settings site.Settings, locale string) []Profile {
	tr := f.tr()
	loc := localization.New(locale)

	out := make([]Profile, 0, len(settings.Profiles))

	for _, profile := range settings.Profiles {
		label := tr.Tl(loc, site.NetworkName(profile.Network))

		out = append(out, Profile{Network: profile.Network, Label: label, URL: profile.URL})
	}

	return out
}
