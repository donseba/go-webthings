package site

import "time"

// maxCacheSeconds is the longest PageMaxAge and PageStale accepted: a year.
const maxCacheSeconds = 365 * 24 * 60 * 60

// The output cache keeps pages for DefaultOutputCacheTTL in at most
// DefaultOutputCacheMB megabytes unless the settings say otherwise (see
// OutputCache); it never keeps them longer than MaxOutputCacheTTL or in
// more than MaxOutputCacheMB.
const (
	DefaultOutputCacheTTL = time.Minute
	DefaultOutputCacheMB  = 32
	MaxOutputCacheTTL     = time.Hour
	MaxOutputCacheMB      = 1024
)

// Compress reports whether responses are compressed (gzip) for browsers that
// accept it: the default, unless CompressionOff is set.
func (s Settings) Compress() bool {
	return !s.CompressionOff
}

// PageCache is how long, in seconds, browsers and caches may keep a public
// page for visitors (maxAge; 0, the default, has them ask the site every
// time) and how long after that they may still show it while fetching a
// fresh one (stale, for stale-while-revalidate).
func (s Settings) PageCache() (maxAge, stale int) {
	return max(s.PageMaxAge, 0), max(s.PageStale, 0)
}

// OutputCache is how long the output cache keeps a public page for
// visitors in memory and how many bytes it keeps at most: DefaultOutputCacheTTL
// and DefaultOutputCacheMB unless set; a ttl of zero when OutputCacheOff is
// set. Changes made in this process show at once; the time bounds how long
// a page shows what changed without a save, such as content published at
// its scheduled time, and changes made by other processes.
func (s Settings) OutputCache() (ttl time.Duration, maxBytes int64) {
	megabytes := s.OutputCacheMB
	if megabytes <= 0 {
		megabytes = DefaultOutputCacheMB
	}

	maxBytes = int64(megabytes) << 20

	switch {
	case s.OutputCacheOff:
		return 0, maxBytes
	case s.OutputCacheTTL <= 0:
		return DefaultOutputCacheTTL, maxBytes
	default:
		return time.Duration(s.OutputCacheTTL) * time.Second, maxBytes
	}
}

// cleanPerformance checks the cache times, whole seconds of at most a year,
// and the output cache's time and size.
func cleanPerformance(settings *Settings) error {
	for _, value := range []int{settings.PageMaxAge, settings.PageStale} {
		if value < 0 || value > maxCacheSeconds {
			return ErrInvalid
		}
	}

	if settings.OutputCacheTTL < 0 || time.Duration(settings.OutputCacheTTL)*time.Second > MaxOutputCacheTTL ||
		settings.OutputCacheMB < 0 || settings.OutputCacheMB > MaxOutputCacheMB {
		return ErrInvalid
	}

	return nil
}
