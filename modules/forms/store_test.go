package forms

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/media"

	form "github.com/donseba/go-form/v2"
)

func TestStore(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()

		if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
			t.Fatal(err)
		}

		if err := database.ApplySchema(ctx, "module:forms", schema()); err != nil {
			t.Fatal(err)
		}

		collection, err := content.NewCollection(database, spec())
		if err != nil {
			t.Fatal(err)
		}

		fields, labels, err := parseFields(exampleFields + "topic | select | Topic\n- a | A\n")
		if err != nil {
			t.Fatal(err)
		}

		saved, err := collection.Save(ctx, "contact", content.Draft[Definition, Text]{
			Fields: Definition{Fields: fields, Notify: "office@example.test"},
			Translations: []content.Translation[Text]{{
				Locale: "en",
				Text:   Text{Title: "Contact", Success: "Thanks", Labels: labels},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}

		item, err := collection.Get(ctx, "contact", saved.ID)
		if err != nil {
			t.Fatal(err)
		}

		text, _ := item.Text("en")
		if !reflect.DeepEqual(item.Fields.Fields, fields) || !reflect.DeepEqual(text.Labels, labels) ||
			item.Fields.Notify != "office@example.test" {
			t.Fatalf("round trip: %+v %+v", item.Fields, text.Labels)
		}

		service := &Service{key: "contact", db: database, forms: collection}

		for _, message := range []string{"first", "second", "third"} {
			if _, err := service.save(ctx, "en", Answers{{Name: "message", Label: "Message", Value: message}}); err != nil {
				t.Fatal(err)
			}
		}

		page, more, err := service.SubmissionsByStatus(ctx, "", 0, 2)
		if err != nil || len(page) != 2 || !more || page[0].Answers[0].Value != "third" {
			t.Fatalf("first page: %+v %v %v", page, more, err)
		}

		last, more, err := service.SubmissionsByStatus(ctx, "", 2, 2)
		if err != nil || len(last) != 1 || more || last[0].Answers[0].Value != "first" {
			t.Fatalf("last page: %+v %v %v", last, more, err)
		}

		if err := service.Archive(ctx, page[0].ID); err != nil {
			t.Fatal(err)
		}

		archived, _, err := service.SubmissionsByStatus(ctx, StatusArchived, 0, 10)
		if err != nil || len(archived) != 1 || archived[0].ID != page[0].ID || archived[0].Status != StatusArchived {
			t.Fatalf("archived: %+v %v", archived, err)
		}

		fresh, err := service.Count(ctx, StatusNew)
		if err != nil || fresh != 2 {
			t.Fatalf("new count: %d %v", fresh, err)
		}

		if err := service.Unarchive(ctx, page[0].ID); err != nil {
			t.Fatal(err)
		}

		if all, err := service.Count(ctx, ""); err != nil || all != 3 {
			t.Fatalf("count: %d %v", all, err)
		}

		if err := service.DeleteSubmission(ctx, last[0].ID); err != nil {
			t.Fatal(err)
		}

		if _, err := service.Submission(ctx, last[0].ID); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("deleted submission: %v", err)
		}
	})
}

// TestSubmissionTrash deletes submissions to the trash and restores and
// purges them through the section's trash.
func TestSubmissionTrash(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		visitor := context.Background()
		editor := db.NewID()
		ctx := db.WithActor(visitor, editor)

		if err := database.ApplySchema(ctx, "module:forms", schema()); err != nil {
			t.Fatal(err)
		}

		service := &Service{key: "contact", db: database}
		other := &Service{key: "quote", db: database}

		var ids []string

		for _, message := range []string{"first", "second", "third"} {
			id, err := service.save(visitor, "en", Answers{{Name: "message", Label: "Message", Value: message}})
			if err != nil {
				t.Fatal(err)
			}

			ids = append(ids, id)
		}

		if _, err := other.save(visitor, "en", Answers{{Name: "message", Label: "Message", Value: "elsewhere"}}); err != nil {
			t.Fatal(err)
		}

		// Visitors send forms: no one created the submission in the audit
		// columns; the editor who archives it changed it.
		audit := func(id string) (string, string) {
			var row struct {
				CreatedBy db.Ref `db:"created_by"`
				UpdatedBy db.Ref `db:"updated_by"`
			}

			err := database.Get(ctx, &row, `SELECT created_by, updated_by FROM wt_forms_submission WHERE id=?`, id)
			if err != nil {
				t.Fatal(err)
			}

			return row.CreatedBy.String(), row.UpdatedBy.String()
		}

		if created, updated := audit(ids[0]); created != "" || updated != "" {
			t.Fatalf("visitor audit: %q %q", created, updated)
		}

		if err := service.Archive(ctx, ids[0]); err != nil {
			t.Fatal(err)
		}

		if created, updated := audit(ids[0]); created != "" || updated != editor {
			t.Fatalf("archive audit: %q %q", created, updated)
		}

		if err := service.DeleteSubmission(ctx, ids[0]); err != nil {
			t.Fatal(err)
		}

		if err := service.DeleteSubmission(ctx, ids[1]); err != nil {
			t.Fatal(err)
		}

		// Deleted submissions leave the inbox, the counts and the badge.
		if err := service.DeleteSubmission(ctx, ids[0]); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("delete twice: %v", err)
		}

		if err := service.Archive(ctx, ids[1]); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("archive deleted: %v", err)
		}

		if _, err := service.Submission(ctx, ids[1]); !errors.Is(err, content.ErrNotFound) {
			t.Fatalf("deleted submission: %v", err)
		}

		list, _, err := service.SubmissionsByStatus(ctx, "", 0, 0)
		if err != nil || len(list) != 1 || list[0].ID != ids[2] {
			t.Fatalf("inbox after delete: %+v %v", list, err)
		}

		archived, _, err := service.SubmissionsByStatus(ctx, StatusArchived, 0, 10)
		if err != nil || len(archived) != 0 {
			t.Fatalf("archived after delete: %+v %v", archived, err)
		}

		if all, err := service.Count(ctx, ""); err != nil || all != 1 {
			t.Fatalf("count after delete: %d %v", all, err)
		}

		if badge := service.badge(ctx); badge != 1 {
			t.Fatalf("badge after delete: %d", badge)
		}

		// The trash lists the submissions of this form only, newest deletion
		// first, by their answers.
		bin := service.trash()

		items, total, err := bin.List(ctx, "", 10, 0)
		if err != nil || total != 2 || len(items) != 2 || items[0].ID != ids[1] || items[0].Title != "second" ||
			items[0].Kind != "Submission" || items[0].DeletedBy != editor || items[0].DeletedAt.IsZero() {
			t.Fatalf("trash: %+v %d %v", items, total, err)
		}

		items, total, err = bin.List(ctx, "first", 10, 0)
		if err != nil || total != 1 || len(items) != 1 || items[0].ID != ids[0] {
			t.Fatalf("trash search: %+v %d %v", items, total, err)
		}

		items, total, err = bin.List(ctx, "", 1, 1)
		if err != nil || total != 2 || len(items) != 1 || items[0].ID != ids[0] {
			t.Fatalf("trash page: %+v %d %v", items, total, err)
		}

		// Restoring brings a submission back with its status.
		if err := bin.Restore(ctx, []string{ids[0]}); err != nil {
			t.Fatal(err)
		}

		restored, err := service.Submission(ctx, ids[0])
		if err != nil || restored.Status != StatusArchived {
			t.Fatalf("restored: %+v %v", restored, err)
		}

		if _, updated := audit(ids[0]); updated != editor {
			t.Fatalf("restore audit: %q", updated)
		}

		// Items that are no longer in the trash are reported, not failed.
		var failure *admin.TrashError

		err = bin.Restore(ctx, []string{ids[0]})
		if !errors.As(err, &failure) || failure.ID != ids[0] || failure.Reason != "This item is no longer in the trash." {
			t.Fatalf("restore twice: %v", err)
		}

		// Purging removes only what is in the trash.
		err = bin.Purge(ctx, []string{ids[2]})
		if !errors.As(err, &failure) || failure.ID != ids[2] {
			t.Fatalf("purge live submission: %v", err)
		}

		if err := bin.Purge(ctx, []string{ids[1]}); err != nil {
			t.Fatal(err)
		}

		var rows int
		if err := database.Get(ctx, &rows, `SELECT COUNT(*) FROM wt_forms_submission`); err != nil || rows != 3 {
			t.Fatalf("rows after purge: %d %v", rows, err)
		}

		if items, total, err := bin.List(ctx, "", 10, 0); err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("trash after purge: %+v %d %v", items, total, err)
		}
	})
}

func TestSyntax(t *testing.T) {
	source := "email* | email | Email | you@example.com\n" +
		"topic | select | Topic | | Pick one\n" +
		"- sales | Sales\n" +
		"- other\n" +
		"agree* | checkbox | I agree\n"

	fields, labels, err := parseFields(source)
	if err != nil {
		t.Fatal(err)
	}

	if len(fields) != 3 || !fields[0].Required || fields[1].Options[1] != "other" || labels["topic"].Help != "Pick one" {
		t.Fatalf("parsed: %+v %+v", fields, labels)
	}

	if again, _, _ := parseFields(formatFields(fields, labels)); !reflect.DeepEqual(again, fields) {
		t.Fatalf("format round trip: %+v", again)
	}

	invalid := map[string]error{
		"":                           errNoFields,
		"email | email":              errLine,
		"Email | email | Email":      errName,
		"website | text | Website":   errName,
		"a | text | A\na | text | B": errDuplicate,
		"a | color | A":              errType,
		"- loose":                    errOption,
		"a | select | A":             errNoOptions,
		"a | select | A\n- x\n- x":   errOptionValue,
	}

	for source, want := range invalid {
		if _, _, err := parseFields(source); !errors.Is(err, want) {
			t.Errorf("%q: want %v, got %v", source, want, err)
		}
	}
}

func TestCheck(t *testing.T) {
	fields, labels, err := parseFields(
		"email* | email | Email\nsize | number | Size\nday | date | Day\nok | checkbox | OK\nnote | text | Note\n",
	)
	if err != nil {
		t.Fatal(err)
	}

	item := Item{
		Fields:       Definition{Fields: fields},
		Translations: []content.Translation[Text]{{Locale: "en", Text: Text{Labels: labels}}},
	}

	// Values are read as the form is posted: trimmed, a checkbox "on". The
	// result is the rules that failed per field, and the answers.
	checker := form.NewForm()
	checker.RegisterLocalizedValidation(mail.Rule, mail.FormRule)

	schema := formSchema(item, "en")

	submit := func(values url.Values) (Answers, map[string][]string) {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		mapped := form.MapValues(request, schema)
		rules := map[string][]string{}

		for _, err := range checker.ValidateSchemaLocalized(schema, mapped, &form.DefaultLocalizer{}) {
			if validation, ok := err.(form.FieldValidationError); ok {
				rules[validation.Field] = append(rules[validation.Field], validation.Rule)
			}
		}

		return answersOf(item, "en", mapped), rules
	}

	has := func(rules map[string][]string, field, rule string) bool {
		return slices.Contains(rules[field], rule)
	}

	// An address without a dot is no address mail can be sent to
	// (mail.Rule), as on the site's other forms.
	_, errs := submit(url.Values{"email": {"a@b"}, "size": {"NaN"}, "day": {"2026-13-01"}})
	if !slices.Equal(errs["email"], []string{mail.Rule}) || len(errs["size"]) == 0 || len(errs["day"]) == 0 ||
		len(errs["ok"]) != 0 {
		t.Fatalf("errors: %v", errs)
	}

	answers, errs := submit(url.Values{"email": {" a@example.test "}, "size": {"1.5"}, "ok": {"on"}})
	if len(errs) != 0 || answers[0].Value != "a@example.test" || answers[3].Value != "yes" {
		t.Fatalf("answers: %+v %v", answers, errs)
	}

	answers, _ = submit(url.Values{"email": {"a@example.test"}})
	if answers[3].Value != "no" {
		t.Fatalf("unticked: %+v", answers)
	}

	_, errs = submit(url.Values{
		"email": {strings.Repeat("a", 250) + "@example.test"},
		"size":  {"Inf"},
	})
	if !has(errs, "email", "maxLength") || len(errs["size"]) == 0 {
		t.Fatalf("too long, infinite: %v", errs)
	}

	_, errs = submit(url.Values{"email": {"not an address"}})
	if !has(errs, "email", "email") {
		t.Fatalf("invalid address: %v", errs)
	}

	_, errs = submit(url.Values{})
	if !has(errs, "email", "required") {
		t.Fatalf("missing address: %v", errs)
	}

	// A text field holds one line of valid UTF-8 without control
	// characters.
	for _, note := range []string{"two\nlines", "a\ttab", "\xff"} {
		_, errs = submit(url.Values{"email": {"a@example.test"}, "note": {note}})
		if len(errs["note"]) == 0 || has(errs, "note", "maxLength") {
			t.Fatalf("note %q: %v", note, errs)
		}
	}

	_, errs = submit(url.Values{"email": {"a@example.test"}, "note": {strings.Repeat("a\n", 300) + "a"}})
	if !has(errs, "note", "maxLength") {
		t.Fatalf("long note: %v", errs)
	}
}

func TestNotification(t *testing.T) {
	fields, _, err := parseFields("name | text | Name\nemail* | email | Email\nother | email | Other\n")
	if err != nil {
		t.Fatal(err)
	}

	answers := Answers{
		{Name: "name", Label: "Name", Value: ""},
		{Name: "email", Label: "Email", Value: "jane@example.test"},
		{Name: "other", Label: "Other", Value: "joe@example.test"},
	}

	if got := replyTo(fields, answers); got != "jane@example.test" {
		t.Fatalf("reply to: %q", got)
	}

	if got := replyTo(fields[:1], answers); got != "" {
		t.Fatalf("reply to without email field: %q", got)
	}

	check := spec().Check
	if err := check(Definition{Fields: fields, Notify: "a@b, c@d"}, nil); !errors.Is(err, errNotify) {
		t.Fatalf("invalid notify address: %v", err)
	}
}
