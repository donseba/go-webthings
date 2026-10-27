package siteadmin

import (
	"net/http"
	"net/url"
	"slices"

	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
)

// reviewInput is the Review form: the sections whose revisions need a
// reviewer's approval before they are published (site.Settings.Review).
type reviewInput struct {
	Sections form.SortedMultiSelect[string] `form:"multicheckbox" label:"Sections that need a review" class:"grid sm:grid-cols-2 lg:grid-cols-4 gap-x-6"`
}

// reviewForm fills the Review form from the settings, or keeps posted when
// set; nil when no section can be reviewed.
func (s section) reviewForm(r *http.Request, current site.Settings, posted *reviewInput) *reviewInput {
	sections := s.admin.ReviewSections(r)
	if len(sections) == 0 {
		return nil
	}

	if posted != nil {
		return posted
	}

	return &reviewInput{Sections: form.NewSortedMultiSelect(s.reviewChoices(r)).Selected(current.Review...)}
}

// reviewChoices maps the reviewable sections to their titles.
func (s section) reviewChoices(r *http.Request) map[string]string {
	choices := map[string]string{}

	for _, section := range s.admin.ReviewSections(r) {
		choices[section.ID] = section.Title
	}

	return choices
}

// saveReview stores the Review form. Sections that cannot be reviewed are
// ignored; ticked sections of modules that are gone are dropped. Only
// administrators may switch the workflow, which decides who may publish
// without a second pair of eyes (see admin.AdministratorsOnly on its route).
func (s section) saveReview(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Settings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	choices := s.reviewChoices(r)
	in := reviewInput{Sections: form.NewSortedMultiSelect(choices)}

	if errs, err := s.admin.MapForm(r, &in); err != nil || len(errs) > 0 {
		s.admin.BadRequest(w, r)

		return
	}

	var reviewed []string

	for _, section := range in.Sections.Get() {
		if _, ok := choices[section]; ok {
			reviewed = append(reviewed, section)
		}
	}

	slices.Sort(reviewed)
	current.Review = reviewed

	if err := s.settings.Save(r.Context(), current); err != nil {
		s.invalid(w, r, current, state{review: &in}, err)

		return
	}

	s.admin.Redirect(w, r, s.admin.URL("admin.site.index", url.Values{"locale": {s.locale(r, current)}}))
}
