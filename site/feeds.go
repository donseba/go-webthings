package site

// A feed lists DefaultFeedSize of the newest items unless the settings say
// otherwise (Settings.FeedSize), and never more than MaxFeedSize.
const (
	DefaultFeedSize = 20
	MaxFeedSize     = 100
)

// FeedLimit is how many of the newest items each feed of the site lists,
// such as the posts in a news page's Atom and RSS feeds: FeedSize, or
// DefaultFeedSize when it is not set.
func (s Settings) FeedLimit() int {
	if s.FeedSize <= 0 {
		return DefaultFeedSize
	}

	return min(s.FeedSize, MaxFeedSize)
}
