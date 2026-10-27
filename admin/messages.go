package admin

import (
	"context"
	"sync"

	"github.com/donseba/go-partial/exp/localization"
	translator "github.com/donseba/go-translator/v2"
)

// messagesKey holds the admin of the current request in its context, so code
// that only has a context, such as an editor's Notes, can translate.
type messagesKey struct{}

// withMessages adds the admin to the request context for T and TN, and its
// localizer: the admin language and the site's timezone.
func (a *Admin) withMessages(ctx context.Context) context.Context {
	ctx = localization.WithLocalizer(ctx, a.localizer(ctx))

	return context.WithValue(ctx, messagesKey{}, a)
}

// Context returns ctx as the context of a request to this admin, for code
// that runs section hooks outside a request, such as Section.Recent in a
// test or background work: RouteURL, T and TN work with it.
func (a *Admin) Context(ctx context.Context) context.Context {
	return a.withMessages(ctx)
}

// RouteURL is Admin.URL for the admin request in ctx, for code that gets a
// context rather than the Admin, such as Section.Recent, Section.Review or a
// Trash: the address of the named route with its parameters filled in.
// Outside an admin request it returns "". Like Admin.URL, it panics on an
// unknown route or parameter.
func RouteURL(ctx context.Context, name string, args ...any) string {
	a, ok := ctx.Value(messagesKey{}).(*Admin)
	if !ok {
		return ""
	}

	return a.URL(name, args...)
}

// T translates an admin message in the language of the admin request in ctx,
// for module code that has a context but no request, such as an editor's
// Notes. Like Admin.T, a message without translation is shown as written,
// with its arguments. Outside an admin request the message is never
// translated.
func T(ctx context.Context, key string, args ...any) string {
	return translatorFor(ctx).Tl(localization.FromContext(ctx), key, args...)
}

// TN translates an admin message with a singular and a plural form, choosing
// the form for n by the plural rules of the admin language. args format the
// chosen form, so n is usually passed again:
//
//	admin.TN(ctx, "%d download", "%d downloads", count, count)
//
// A catalogue writes such a message with msgid, msgid_plural and one
// msgstr[i] per form. Without translation, or outside an admin request, the
// English rule picks singular for 1 and plural otherwise.
func TN(ctx context.Context, singular, plural string, n int, args ...any) string {
	return translatorFor(ctx).Tn(localization.FromContext(ctx), singular, plural, n, args...)
}

// translatorFor is the translator of the admin request in ctx. Outside one it
// is go-translator without catalogues, which shows every message as written.
func translatorFor(ctx context.Context) *translator.Translator {
	a, ok := ctx.Value(messagesKey{}).(*Admin)
	if !ok {
		return untranslated()
	}

	return a.tr
}

// untranslated is go-translator without catalogues: messages show as
// written, with their arguments.
var untranslated = sync.OnceValue(func() *translator.Translator {
	return translator.Untranslated()
})
