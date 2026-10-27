package admin

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/auth"

	form "github.com/donseba/go-form/v2"
)

// GroupsView is the list of groups with a form to add one.
type GroupsView struct {
	Groups []GroupRow
	Create form.RenderModel
	Errors form.FieldErrors
}

// GroupRow is one group in the list of groups, with its number of
// members.
type GroupRow struct {
	Name    string
	URL     string
	Members int
}

// MatrixView is one group's permission matrix. Form is the form around it,
// with the group's name; the matrix's checkboxes post "allow".
type MatrixView struct {
	Name    string
	Form    form.RenderModel
	Errors  form.FieldErrors
	Actions []string
	// Rows are all sections; Groups are the same rows under the headings of
	// the navigation, in its order.
	Rows   []MatrixRow
	Groups []MatrixGroup
	// Columns is the number of columns: the section and one per action.
	Columns int
	Delete  form.RenderModel
}

// MatrixGroup is the sections under one navigation heading; Name is empty
// for the sections listed without heading (Pages, Media).
type MatrixGroup struct {
	Name string
	Rows []MatrixRow
}

// MatrixRow is one section with a checkbox per action.
type MatrixRow struct {
	Title   string
	Section string
	Cells   []MatrixCell

	// group is the section's navigation group key; reviewable sections
	// offer the Review right (see Section.Review).
	group      string
	reviewable bool
}

// MatrixCell is one checkbox of the permission matrix: Value is posted as
// "<section>:<action>" when ticked, and Allowed ticks it.
type MatrixCell struct {
	Value   string
	Allowed bool
	// Unavailable cells have no checkbox: the action does not apply to the
	// section, such as Review in a section without revisions, or Export in
	// a section without an export.
	Unavailable bool
}

type groupInput struct {
	Name string `form:"input,text" label:"Group name" required:"true" maxLength:"100"`
}

// matrixInput is the name in a group's permission matrix, posted as "name".
type matrixInput struct {
	Name string `form:"input,text" label:"Group name" required:"true" maxLength:"100" name:"name"`
}

func (a *Admin) groupsSection() Section {
	return Section{
		ID:        "groups",
		Title:     "Groups",
		Icon:      "users",
		AdminOnly: true,
		Group:     GroupSystem,
		Routes: func(routes *Routes) {
			routes.Get("", View, a.groups).As("index")
			routes.Post("", Create, a.createGroup)
			routes.Get("/{group}", View, a.group).As("show")
			routes.Post("/{group}", Edit, a.saveGroup)
			routes.Post("/{group}/delete", Delete, a.deleteGroup).As("delete")
		},
	}
}

func (a *Admin) groups(w http.ResponseWriter, r *http.Request) {
	a.renderGroups(w, r, groupInput{}, nil, "", http.StatusOK)
}

func (a *Admin) renderGroups(
	w http.ResponseWriter,
	r *http.Request,
	input groupInput,
	errs form.FieldErrors,
	message string,
	status int,
) {
	groups, err := a.auth.Groups(r.Context())
	if err != nil {
		a.Fail(w, r, err)

		return
	}

	// The message shows as a toast; errors of single fields stay inline.
	r = WithError(r, message)

	view := GroupsView{
		Create: a.Form(r, input, a.URL("admin.groups.index"), "Add group"),
		Errors: errs,
	}

	search := SearchOf(r)

	for _, group := range groups {
		if !search.Matches(group.Name) {
			continue
		}

		view.Groups = append(view.Groups, GroupRow{
			Name:    group.Name,
			URL:     a.URL("admin.groups.show", "group", group.ID),
			Members: len(group.Members),
		})
	}

	a.Render(w, r, Page{Title: "Groups", Template: "templates/groups.gohtml", Data: view, Status: status})
}

func (a *Admin) createGroup(w http.ResponseWriter, r *http.Request) {
	var input groupInput

	errs, err := a.Bind(r, &input)
	if err != nil {
		a.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		a.renderGroups(w, r, input, errs, "", http.StatusUnprocessableEntity)

		return
	}

	group, err := a.auth.SaveGroup(r.Context(), auth.Group{Name: input.Name})
	if message := groupNameIssue(err); message != "" {
		a.renderGroups(w, r, input, FieldError("Name", a.T(r, message)), "", http.StatusUnprocessableEntity)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	a.Redirect(w, r, a.URL("admin.groups.show", "group", group.ID))
}

// matrixSections are the rows of the matrix: every section with its own
// rights, except the administrator-only ones.
func (a *Admin) matrixSections(r *http.Request) []MatrixRow {
	var rows []MatrixRow

	for _, section := range a.sections {
		if section.AdminOnly || section.PermissionID() != section.ID {
			continue
		}

		rows = append(rows, MatrixRow{
			Title:      a.T(r, section.Title),
			Section:    section.ID,
			group:      section.Group,
			reviewable: section.Review != nil,
		})
	}

	return rows
}

// matrixGroups puts rows under the headings of the navigation: the sections
// without heading first in their order, then the groups by translated name
// with their sections by title, GroupSystem last.
func (a *Admin) matrixGroups(r *http.Request, rows []MatrixRow) []MatrixGroup {
	order := a.collator()

	var (
		top   MatrixGroup
		keys  []string
		named = map[string]*MatrixGroup{}
	)

	for _, row := range rows {
		if row.group == "" {
			top.Rows = append(top.Rows, row)

			continue
		}

		group, ok := named[row.group]
		if !ok {
			group = &MatrixGroup{Name: a.T(r, row.group)}
			named[row.group] = group
			keys = append(keys, row.group)
		}

		group.Rows = append(group.Rows, row)
	}

	slices.SortFunc(keys, func(x, y string) int {
		if (x == GroupSystem) != (y == GroupSystem) {
			if x == GroupSystem {
				return 1
			}

			return -1
		}

		if c := order.CompareString(named[x].Name, named[y].Name); c != 0 {
			return c
		}

		return strings.Compare(x, y)
	})

	var groups []MatrixGroup

	if len(top.Rows) > 0 {
		groups = append(groups, top)
	}

	for _, key := range keys {
		group := named[key]
		slices.SortStableFunc(group.Rows, func(x, y MatrixRow) int {
			return order.CompareString(x.Title, y.Title)
		})

		groups = append(groups, *group)
	}

	return groups
}

func (a *Admin) group(w http.ResponseWriter, r *http.Request) {
	group, err := a.auth.Group(r.Context(), r.PathValue("group"))
	if errors.Is(err, auth.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	a.renderMatrix(w, r, group, nil, http.StatusOK)
}

// renderMatrix shows the matrix of group, with errs at the name.
func (a *Admin) renderMatrix(w http.ResponseWriter, r *http.Request, group auth.Group, errs form.FieldErrors, status int) {
	view := MatrixView{
		Name:   group.Name,
		Form:   a.Form(r, matrixInput{Name: group.Name}, a.URL("admin.groups.show", "group", group.ID), "Save"),
		Errors: errs,
		Delete: a.Confirm(
			r,
			a.Form(r, struct{}{}, a.URL("admin.groups.delete", "group", group.ID), "Delete group"),
			"Delete this group? This cannot be undone.",
		),
	}

	// The Export column shows only when some section has an export.
	actions := slices.DeleteFunc(slices.Clone(auth.Actions), func(action string) bool {
		return action == auth.Export && len(a.exports) == 0
	})

	for _, action := range actions {
		view.Actions = append(view.Actions, a.T(r, actionLabels[action]))
	}

	for _, row := range a.matrixSections(r) {
		for _, action := range actions {
			row.Cells = append(row.Cells, MatrixCell{
				Value:       row.Section + ":" + action,
				Allowed:     group.Permissions.Allows(row.Section, action),
				Unavailable: action == auth.Review && !row.reviewable || action == auth.Export && !a.exports[row.Section],
			})
		}

		view.Rows = append(view.Rows, row)
	}

	view.Groups = a.matrixGroups(r, view.Rows)
	view.Columns = len(view.Actions) + 1

	a.Render(w, r, Page{Title: "Groups", Label: group.Name, Template: "templates/group.gohtml", Data: view, Status: status})
}

var actionLabels = map[string]string{
	auth.Read:    "Read",
	auth.Create:  "Create",
	auth.Edit:    "Edit",
	auth.Publish: "Publish",
	auth.Delete:  "Delete",
	auth.Review:  "Review",
	auth.Export:  "Export",
}

func (a *Admin) saveGroup(w http.ResponseWriter, r *http.Request) {
	group, err := a.auth.Group(r.Context(), r.PathValue("group"))
	if errors.Is(err, auth.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	var input matrixInput

	if errs, err := a.MapForm(r, &input); err != nil || len(errs) > 0 {
		a.BadRequest(w, r)

		return
	}

	group.Name = input.Name
	group.Permissions = auth.Permissions{}

	for _, value := range r.PostForm["allow"] {
		section, action, ok := strings.Cut(value, ":")
		if !ok {
			continue
		}

		// Every other right needs the section's pages, so it implies read.
		if !group.Permissions.Allows(section, auth.Read) {
			group.Permissions[section] = append(group.Permissions[section], auth.Read)
		}

		if !group.Permissions.Allows(section, action) {
			group.Permissions[section] = append(group.Permissions[section], action)
		}
	}

	if errs := a.Validate(r, &input); len(errs) > 0 {
		a.renderMatrix(w, r, group, errs, http.StatusUnprocessableEntity)

		return
	}

	saved, err := a.auth.SaveGroup(r.Context(), group)
	if message := groupNameIssue(err); message != "" {
		a.renderMatrix(w, r, group, FieldError("name", a.T(r, message)), http.StatusUnprocessableEntity)

		return
	}

	if err != nil {
		a.Fail(w, r, err)

		return
	}

	a.Redirect(w, r, a.URL("admin.groups.show", "group", saved.ID))
}

// groupNameIssue explains a refused group name, as a translation key;
// empty for other errors.
func groupNameIssue(err error) string {
	switch {
	case errors.Is(err, auth.ErrDuplicate):
		return "A group with this name already exists."
	case errors.Is(err, auth.ErrGroupName):
		return "Give the group a name of at most 100 characters."
	}

	return ""
}

func (a *Admin) deleteGroup(w http.ResponseWriter, r *http.Request) {
	err := a.auth.DeleteGroup(r.Context(), r.PathValue("group"))
	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		a.Fail(w, r, err)

		return
	}

	// Groups are deleted for good, not moved to the trash.
	a.RedirectNotice(w, r, a.URL("admin.groups.index"), "Group deleted.")
}
