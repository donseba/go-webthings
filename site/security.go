package site

// The values of Settings.Framing: which sites may show the site's pages in a
// frame, such as an iframe. Pages that only the site itself may frame cannot
// be laid over another site to trick visitors into clicking (clickjacking).
const (
	// FramingSameOrigin lets only the site itself frame its pages: the
	// default.
	FramingSameOrigin = "same-origin"

	// FramingAny lets every site frame the pages, for example to embed them.
	FramingAny = "any"

	// FramingNone lets no site frame the pages, not even the site itself.
	FramingNone = "none"
)

// FramingPolicy is which sites may frame the site's pages: Framing, or
// FramingSameOrigin when it is empty or unknown.
func (s Settings) FramingPolicy() string {
	switch s.Framing {
	case FramingAny, FramingNone:
		return s.Framing
	default:
		return FramingSameOrigin
	}
}

// cleanSecurity checks Framing: empty, the default, or one of the Framing
// values.
func cleanSecurity(settings *Settings) error {
	switch settings.Framing {
	case "", FramingSameOrigin, FramingAny, FramingNone:
		return nil
	default:
		return ErrInvalid
	}
}
