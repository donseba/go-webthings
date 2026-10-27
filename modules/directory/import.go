package directory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"

	form "github.com/donseba/go-form/v2"
)

// importColumns are the CSV columns the import reads, in documentation order.
var importColumns = []string{
	"name", "category", "summary", "website", "email", "phone", "street", "postcode", "city", "keywords",
}

var (
	errHeader = errors.New("directory_import_header")
	errCSV    = errors.New("directory_import_invalid_csv")
)

// ImportOptions change what Import does with the rows.
type ImportOptions struct {
	// Locale is the language of the summary, keywords and new category names.
	Locale string
	// Update changes entries whose slug already exists; otherwise such rows
	// are skipped.
	Update bool
	// Publish publishes the entries and categories the import creates.
	// Existing categories keep their state: an editor may hide a category on
	// purpose, so entries imported into an unpublished category stay off the
	// site until the category is published.
	Publish bool
}

// ImportResult tells what an import did.
type ImportResult struct {
	Created    int
	Updated    int
	Skipped    int
	Categories int
	Problems   []ImportProblem
}

// ImportProblem is a row that was skipped. Reason is an admin message key.
type ImportProblem struct {
	Row    int
	Reason string
}

// Import reads entries from CSV (see the package documentation for the
// columns) and creates or updates them. A row that cannot be saved is
// skipped and reported; other errors stop the import.
func (s *Service) Import(ctx context.Context, source io.Reader, options ImportOptions) (ImportResult, error) {
	rows, err := admin.ReadCSV(source)
	if err != nil {
		return ImportResult{}, fmt.Errorf("%w: %w", errCSV, err)
	}

	if len(rows) == 0 {
		return ImportResult{}, errHeader
	}

	columns := map[string]int{}

	for i, name := range rows[0].Values {
		columns[strings.ToLower(strings.TrimSpace(name))] = i
	}

	if _, ok := columns["name"]; !ok {
		return ImportResult{}, errHeader
	}

	if _, ok := columns["category"]; !ok {
		return ImportResult{}, errHeader
	}

	run, err := s.newImport(ctx, columns, options)
	if err != nil {
		return ImportResult{}, err
	}

	for _, row := range rows[1:] {
		if err := run.row(ctx, row.Line, row.Values); err != nil {
			return run.result, err
		}
	}

	return run.result, nil
}

// importRun is the state of one import: the known categories and entries.
type importRun struct {
	service    *Service
	options    ImportOptions
	columns    map[string]int
	categories []CategoryItem
	entries    map[string]EntryItem
	result     ImportResult
}

func (s *Service) newImport(ctx context.Context, columns map[string]int, options ImportOptions) (*importRun, error) {
	categories, err := s.categories.List(ctx, s.key)
	if err != nil {
		return nil, err
	}

	entries, err := s.entries.List(ctx, s.key)
	if err != nil {
		return nil, err
	}

	run := &importRun{
		service:    s,
		options:    options,
		columns:    columns,
		categories: categories,
		entries:    map[string]EntryItem{},
	}

	for _, item := range entries {
		run.entries[item.Fields.Slug] = item
	}

	return run, nil
}

// value returns a column of record, and whether the file has the column.
func (run *importRun) value(record []string, column string) (string, bool) {
	index, ok := run.columns[column]
	if !ok {
		return "", false
	}

	if index >= len(record) {
		return "", true
	}

	return strings.TrimSpace(record[index]), true
}

func (run *importRun) skip(row int, reason string) {
	run.result.Skipped++
	run.result.Problems = append(run.result.Problems, ImportProblem{Row: row, Reason: reason})
}

func (run *importRun) row(ctx context.Context, row int, record []string) error {
	if strings.TrimSpace(strings.Join(record, "")) == "" {
		return nil
	}

	name, _ := run.value(record, "name")
	categoryName, _ := run.value(record, "category")

	if name == "" {
		run.skip(row, "The name is missing.")

		return nil
	}

	if categoryName == "" {
		run.skip(row, "The category is missing.")

		return nil
	}

	slug := content.Slug(name)
	if slug == "" {
		run.skip(row, "A web address cannot be made from this name.")

		return nil
	}

	existing, exists := run.entries[slug]
	if exists && !run.options.Update {
		run.skip(row, "An entry with this name already exists.")

		return nil
	}

	category, err := run.category(ctx, categoryName)
	if err != nil {
		return run.failed(row, err)
	}

	draft := content.Draft[EntryFields, EntryText]{
		ID:           existing.ID,
		Fields:       existing.Fields,
		Translations: existing.Translations,
	}

	draft.Fields.Name = name
	draft.Fields.Slug = slug
	draft.Fields.Category = category
	run.fields(record, &draft.Fields)

	text, _ := existing.Text(run.options.Locale)

	if summary, ok := run.value(record, "summary"); ok {
		text.Summary = summary
	}

	if words, ok := run.value(record, "keywords"); ok {
		text.Keywords = words
	}

	draft.Translations = content.SetText(draft.Translations, run.options.Locale, text)

	saved, err := run.service.entries.Save(ctx, run.service.key, draft)
	if err != nil {
		return run.failed(row, err)
	}

	run.entries[slug] = saved

	if exists {
		run.result.Updated++

		return nil
	}

	run.result.Created++

	if run.options.Publish {
		return run.service.entries.SetPublished(ctx, run.service.key, saved.ID, true, run.options.Locale)
	}

	return nil
}

// fields copies the contact and address columns the file has.
func (run *importRun) fields(record []string, fields *EntryFields) {
	targets := map[string]*string{
		"email":    &fields.Email,
		"phone":    &fields.Phone,
		"street":   &fields.Street,
		"postcode": &fields.Postcode,
		"city":     &fields.City,
	}

	for column, target := range targets {
		if value, ok := run.value(record, column); ok {
			*target = value
		}
	}

	if value, ok := run.value(record, "website"); ok {
		fields.Website = website(value)
	}
}

// failed skips a row whose values the module rejects and returns other errors.
func (run *importRun) failed(row int, err error) error {
	reason := issue(err)
	if reason == "" {
		return err
	}

	run.skip(row, reason)

	return nil
}

// category finds a category by name in the import's language, or by slug, and
// creates it when there is none.
func (run *importRun) category(ctx context.Context, name string) (string, error) {
	slug := content.Slug(name)

	for _, item := range run.categories {
		text, _ := item.Text(run.options.Locale)
		if strings.EqualFold(text.Name, name) || item.Fields.Slug == slug {
			return item.ID, nil
		}
	}

	created, err := run.service.categories.Save(ctx, run.service.key, content.Draft[CategoryFields, CategoryText]{
		Fields:       CategoryFields{Slug: slug},
		Translations: []content.Translation[CategoryText]{{Locale: run.options.Locale, Text: CategoryText{Name: name}}},
	})
	if err != nil {
		return "", err
	}

	if run.options.Publish {
		err := run.service.categories.SetPublished(ctx, run.service.key, created.ID, true, run.options.Locale)
		if err != nil {
			return "", err
		}
	}

	run.categories = append(run.categories, created)
	run.result.Categories++

	return created.ID, nil
}

// importer is the admin screen for CSV imports.
type importer struct {
	service *Service
	admin   *admin.Admin
	routes  *admin.Routes
	title   string
}

// ImportView is the data of the import screen. The template lays out Form
// field by field: Update and Publish only for editors who may use them.
type ImportView struct {
	Title      string
	BackURL    string
	Columns    string
	CanUpdate  bool
	CanPublish bool
	Form       form.RenderModel
	Errors     form.FieldErrors
	Result     *ImportResult
}

// importInput is the import form: a CSV file, or CSV pasted into the text
// field. The file's size is limited like the request (maxImportBytes).
type importInput struct {
	File    *multipart.FileHeader `form:"input,file" label:"CSV file"                           name:"file"    accept:".csv,.tsv,text/csv,text/tab-separated-values,text/plain" maxSize:"4MB"`
	CSV     string                `form:"textarea"   label:"Or paste CSV"                       name:"csv"                                                                                    rows:"8"`
	Update  bool                  `form:"checkbox"   label:"Update entries that already exist"  name:"update"`
	Publish bool                  `form:"checkbox"   label:"Publish new entries and categories" name:"publish"`
}

func (i importer) form(w http.ResponseWriter, r *http.Request) {
	i.render(w, r, importInput{}, nil, nil, "", http.StatusOK)
}

func (i importer) render(
	w http.ResponseWriter,
	r *http.Request,
	input importInput,
	errs form.FieldErrors,
	result *ImportResult,
	message string,
	status int,
) {
	// The message shows as a toast.
	r = admin.WithError(r, message)

	// A file is not sent back to the browser.
	input.File = nil

	i.admin.Render(w, r, admin.Page{
		Title:    "Import CSV",
		Template: "templates/admin/import.gohtml",
		Files:    files,
		Status:   status,
		Data: ImportView{
			Title:      i.title,
			BackURL:    i.routes.URL("index"),
			Columns:    strings.Join(importColumns, ", "),
			CanUpdate:  i.admin.Can(r, admin.Permission{Section: i.service.key, Action: admin.Edit}),
			CanPublish: i.admin.Can(r, admin.Permission{Section: i.service.key, Action: admin.Publish}),
			Form:       i.admin.Form(r, input, i.routes.URL("import"), "Import"),
			Errors:     errs,
			Result:     result,
		},
	})
}

// run imports an uploaded file, or CSV pasted into the text field.
func (i importer) run(w http.ResponseWriter, r *http.Request) {
	var input importInput

	errs, err := i.admin.Bind(r, &input)
	if err != nil {
		i.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		i.render(w, r, input, errs, nil, "", http.StatusUnprocessableEntity)

		return
	}

	source := io.Reader(strings.NewReader(input.CSV))

	if input.File != nil {
		file, err := input.File.Open()
		if err != nil {
			i.admin.Fail(w, r, err)

			return
		}

		defer func() { _ = file.Close() }()

		source = file
	}

	config, err := i.service.locales.Configuration(r.Context())
	if err != nil {
		i.admin.Fail(w, r, err)

		return
	}

	options := ImportOptions{
		Locale:  config.Default,
		Update:  input.Update && i.admin.Can(r, admin.Permission{Section: i.service.key, Action: admin.Edit}),
		Publish: input.Publish && i.admin.Can(r, admin.Permission{Section: i.service.key, Action: admin.Publish}),
	}

	result, err := i.service.Import(r.Context(), source, options)
	if errors.Is(err, errHeader) {
		message := i.admin.T(r, "The first row must name the columns, including name and category.")
		i.render(w, r, input, nil, nil, message, http.StatusUnprocessableEntity)

		return
	}

	if errors.Is(err, errCSV) {
		i.render(w, r, input, nil, nil, i.admin.T(r, "The file is not valid CSV."), http.StatusUnprocessableEntity)

		return
	}

	if err != nil {
		i.admin.Fail(w, r, err)

		return
	}

	i.render(w, r, input, nil, &result, "", http.StatusOK)
}
