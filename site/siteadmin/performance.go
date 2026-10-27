package siteadmin

import (
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
)

// performanceInput is the Performance form: compression, how long
// browsers and caches keep public pages (see site.Settings.PageCache) and
// how long and in how much memory the site keeps them itself (see
// site.Settings.OutputCache). The times are seconds and the memory
// megabytes; the dropdowns offer a few sensible ones.
type performanceInput struct {
	Compression   bool `form:"checkbox" label:"Compress responses"                       description:"Send pages, stylesheets and scripts compressed (gzip) to browsers that accept it. Images and video are compressed already."`
	PageMaxAge    int  `form:"dropdown" label:"Browsers and caches keep pages for"       description:"How long visitors' browsers and caches may show a page without asking the site. With no time they ask every time, and an unchanged page costs little. Changes show after at most this time; signed-in editors always see the latest pages."                                                                                                                                                                                            values:"0:No time, ask every time;60:1 minute;300:5 minutes;900:15 minutes;3600:1 hour"`
	PageStale     int  `form:"dropdown" label:"Then show the kept page while refreshing" description:"After that time, how long a cache may still show the kept page while it fetches a fresh one, so visitors do not wait."                                                                                                                                                                                                                                                                                                                 values:"0:No time;60:1 minute;600:10 minutes;3600:1 hour;86400:1 day"`
	OutputCache   int  `form:"dropdown" label:"The site keeps pages in memory for"       description:"The site keeps the pages it sends visitors in memory and sends them again without making them anew. Changes made on this server show at once, in the admin and when content is published at its planned time. Changes made by another server of the site, and what changes with time alone, such as an event that has passed, show after at most this time. Signed-in editors, and visitors who sent a form, always get a fresh page." values:"0:Off;10:10 seconds;60:1 minute;300:5 minutes;900:15 minutes"`
	OutputCacheMB int  `form:"dropdown" label:"Memory for kept pages"                    description:"The most memory the kept pages take; the pages not asked for the longest are dropped first."                                                                                                                                                                                                                                                                                                                                           values:"8:8 MB;32:32 MB;64:64 MB;128:128 MB;256:256 MB"`
}

// performanceForm fills the Performance form from the settings.
func performanceForm(current site.Settings) performanceInput {
	maxAge, stale := current.PageCache()
	ttl, maxBytes := current.OutputCache()

	return performanceInput{
		Compression:   current.Compress(),
		PageMaxAge:    maxAge,
		PageStale:     stale,
		OutputCache:   int(ttl.Seconds()),
		OutputCacheMB: int(maxBytes >> 20),
	}
}

// performanceOverrides disable the output cache's time when the server
// configuration sets it, and offer that time when the dropdown has no
// such choice, such as 45s.
func (f Fixed) performanceOverrides() form.Overrides {
	if !f.OutputCache {
		return nil
	}

	field, _ := reflect.TypeFor[performanceInput]().FieldByName("OutputCache")
	values := field.Tag.Get("values")
	seconds := strconv.Itoa(int(f.OutputCacheTTL.Seconds()))

	offered := false

	for choice := range strings.SplitSeq(values, ";") {
		value, _, _ := strings.Cut(choice, ":")
		if value == seconds {
			offered = true
		}
	}

	if !offered {
		values += ";" + seconds + ":" + shortDuration(f.OutputCacheTTL)
	}

	return form.Overrides{
		"OutputCache": {"disabled": "true", "description": fixedNote, "values": values},
	}
}

// shortDuration writes d in whole seconds without zero parts, such as 45s,
// 2m or 1h30m.
func shortDuration(d time.Duration) string {
	written := d.Truncate(time.Second).String()

	if strings.HasSuffix(written, "m0s") {
		written = strings.TrimSuffix(written, "0s")
	}

	if strings.HasSuffix(written, "h0m") {
		written = strings.TrimSuffix(written, "0m")
	}

	return written
}

// savePerformance stores the Performance form.
func (s section) savePerformance(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Settings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	var in performanceInput

	errs, err := s.admin.Bind(r, &in)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.render(w, r, current, state{performance: &in, performanceErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	// Bind checked that each value is one the dropdowns offer.
	current.CompressionOff = !in.Compression
	current.PageMaxAge = in.PageMaxAge
	current.PageStale = in.PageStale
	current.OutputCacheMB = in.OutputCacheMB

	// The time the server configuration sets leaves the setting as it is.
	if !s.fixed.OutputCache {
		current.OutputCacheTTL = in.OutputCache
		current.OutputCacheOff = current.OutputCacheTTL == 0
	}

	if err := s.settings.Save(r.Context(), current); err != nil {
		s.invalid(w, r, current, state{performance: &in}, err)

		return
	}

	s.admin.Redirect(w, r, s.admin.URL("admin.site.index", url.Values{"locale": {s.locale(r, current)}}))
}
