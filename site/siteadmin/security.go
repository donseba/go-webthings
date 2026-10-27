package siteadmin

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
)

// securityInput is the Security form: which sites may show the site's pages
// in a frame (see site.Settings.Framing), and the Content-Security-Policy
// with the outside sources it allows (see site.Settings.ContentSecurity).
type securityInput struct {
	Framing         string `form:"dropdown" label:"Other sites may embed pages" description:"Whether other sites may show the site's pages in a frame, such as an iframe. Allowing only this site keeps other sites from hiding a page under their own to trick visitors into clicking."                                                                                                                                                                   values:"same-origin:No, only this site;any:Yes, any site;none:No, not even this site"`
	ContentSecurity string `form:"dropdown" label:"Content-Security-Policy"     description:"Lets browsers run only the scripts and styles the site serves itself, which blocks most code injected into a page. Report only blocks nothing: browsers report what the policy would block, and the site writes it to its log. Check the log, then enforce. A theme with styles or scripts written into its pages needs changes first."                       values:"off:Off;report-only:Report only, block nothing;enforce:Enforce"`
	Sources         string `form:"textarea" label:"Outside sources"             description:"Other sites the pages may load from, one directive per line followed by addresses, such as frame-src https://www.youtube-nocookie.com for embedded videos. Directives: img-src, media-src, font-src, style-src, script-src, connect-src, frame-src, worker-src, manifest-src and form-action. The site's own files and its media storage are always allowed."                                                                                       maxLength:"4000"`
}

// securityForm fills the Security form from the settings.
func securityForm(current site.Settings) securityInput {
	return securityInput{
		Framing:         current.FramingPolicy(),
		ContentSecurity: current.ContentSecurityPolicy(),
		Sources:         current.ContentSecuritySources,
	}
}

// securityOverrides disable the policy when the server configuration sets
// it.
func (f Fixed) securityOverrides() form.Overrides {
	if f.ContentSecurityPolicy == "" {
		return nil
	}

	return form.Overrides{
		"ContentSecurity": {"disabled": "true", "description": fixedNote},
	}
}

// saveSecurity stores the Security form. Only administrators may change
// it, as it decides how the site's pages protect visitors (see
// admin.AdministratorsOnly on its route).
func (s section) saveSecurity(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Settings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	var in securityInput

	errs, err := s.admin.Bind(r, &in)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.render(w, r, current, state{security: &in, securityErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	if _, err := site.ParseCSPSources(in.Sources); err != nil {
		errs = admin.FieldError("Sources", s.admin.T(r,
			"Each line needs a directive, such as frame-src, and addresses, such as https://www.youtube-nocookie.com. "+
				"Inline code, eval and every site (*) cannot be allowed."))
		s.render(w, r, current, state{security: &in, securityErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	// The dropdowns only offer the Framing and ContentSecurity values,
	// which Validate checked. The policy the server configuration sets
	// leaves the setting as it is.
	current.Framing = in.Framing

	if s.fixed.ContentSecurityPolicy == "" {
		current.ContentSecurity = in.ContentSecurity
	}

	current.ContentSecuritySources = strings.TrimSpace(strings.ReplaceAll(in.Sources, "\r\n", "\n"))

	if err := s.settings.Save(r.Context(), current); err != nil {
		s.invalid(w, r, current, state{security: &in}, err)

		return
	}

	s.guide.done(w, r, s.admin.URL("admin.site.index", url.Values{"locale": {s.locale(r, current)}}), "")
}
