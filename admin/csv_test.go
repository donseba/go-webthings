package admin_test

import (
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/donseba/go-webthings/admin"
)

func TestReadCSV(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"comma":     "\ufeffname,email\nAda, ada@example.test\n",
		"semicolon": "name;email\nAda;ada@example.test\n",
		"tab":       "name\temail\nAda\tada@example.test\n",
	}

	for name, source := range cases {
		rows, err := admin.ReadCSV(strings.NewReader(source))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if len(rows) != 2 || !slices.Equal(rows[0].Values, []string{"name", "email"}) ||
			!slices.Equal(rows[1].Values, []string{"Ada", "ada@example.test"}) || rows[1].Line != 2 {
			t.Fatalf("%s: %+v", name, rows)
		}
	}

	// Rows may differ in length; a stray quote is kept.
	rows, err := admin.ReadCSV(strings.NewReader("a,b\nO\"Brien\n\nlast,x,y\n"))
	if err != nil || len(rows) != 3 || rows[1].Values[0] != `O"Brien` || rows[2].Line != 4 || len(rows[2].Values) != 3 {
		t.Fatalf("lenient rows: %+v %v", rows, err)
	}

	empty, err := admin.ReadCSV(strings.NewReader(""))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty file: %v %v", empty, err)
	}

	if _, err := admin.ReadCSV(iotest.ErrReader(errors.New("disk"))); err == nil {
		t.Fatal("a failing source is reported")
	}
}

func TestCSVCell(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":            "",
		"Ada":         "Ada",
		"=SUM(A1)":    "'=SUM(A1)",
		"+31 20":      "'+31 20",
		"-1":          "'-1",
		"@cmd":        "'@cmd",
		"\tindented":  "'\tindented",
		"ada@example": "ada@example",
	}

	for value, want := range cases {
		if got := admin.CSVCell(value); got != want {
			t.Errorf("%q: %q, want %q", value, got, want)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()

	err := admin.WriteCSV(w, "news-subscribers", []string{"email", "name"}, [][]string{
		{"ada@example.test", "=HYPERLINK()"},
		{"bob@example.test", "Bob, Jr."},
	})
	if err != nil {
		t.Fatal(err)
	}

	if w.Header().Get("Content-Type") != "text/csv; charset=utf-8" ||
		w.Header().Get("Content-Disposition") != "attachment; filename=news-subscribers.csv" ||
		w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers: %v", w.Header())
	}

	want := "email,name\nada@example.test,'=HYPERLINK()\nbob@example.test,\"Bob, Jr.\"\n"
	if w.Body.String() != want {
		t.Fatalf("body:\n%s", w.Body.String())
	}
}
