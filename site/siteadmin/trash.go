package siteadmin

import (
	"net/http"
	"net/url"
)

// trashInput is the Trash form: after how many days in the admin's trash
// items are deleted for good (site.Settings.TrashDays). Empty is 0, which
// keeps them until someone deletes them. Its max is site.MaxTrashDays.
type trashInput struct {
	Days int `form:"input,number" label:"Delete items in the trash for good after (days)" min:"0" max:"3650" description:"Items that have been in the trash longer are deleted for good, as Delete selected for good does; the site checks every hour. They cannot be restored after that. Empty or 0 keeps them until someone deletes them for good."`
}

// saveTrash stores the Trash form.
func (s section) saveTrash(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Settings(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	var in trashInput

	errs, err := s.admin.Bind(r, &in)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.render(w, r, current, state{trash: &in, trashErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	// Bind checked the range, which site.Service.Save checks again.
	current.TrashDays = in.Days

	if err := s.settings.Save(r.Context(), current); err != nil {
		s.invalid(w, r, current, state{trash: &in}, err)

		return
	}

	s.admin.Redirect(w, r, s.admin.URL("admin.site.index", url.Values{"locale": {s.locale(r, current)}}))
}
