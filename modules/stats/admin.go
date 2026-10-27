package stats

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

// deviceLabels are the translation keys of the device classes.
var deviceLabels = map[string]string{
	"desktop": "Desktop",
	"mobile":  "Mobile",
	"tablet":  "Tablet",
}

// ranges are the periods the dashboard offers, in days.
var ranges = []int{7, 30, 90}

type settingsInput struct {
	Retention int `form:"input,number" label:"Keep statistics for (days)" min:"0" max:"3650" description:"Older days are deleted. 0 keeps all statistics."`
}

// DashboardView is the data of the admin dashboard.
type DashboardView struct {
	Title    string
	Days     int
	Ranges   []RangeLink
	Views    int
	Visitors int
	// From and To are the first and last day of the chart, in the admin
	// language.
	From      string
	To        string
	Bars      []Bar
	Pages     []Row
	Referrers []Row
	Devices   []Row
	Locales   []Row
	// Settings is the retention form, for users who may edit.
	Settings       *form.RenderModel
	SettingsErrors form.FieldErrors
}

// RangeLink switches the dashboard's period.
type RangeLink struct {
	Days   int
	URL    string
	Active bool
}

// Bar is one day of the chart. Height is a percentage of the busiest day.
type Bar struct {
	// Date is the day in the admin language, such as "5 okt 2026".
	Date     string
	Views    int
	Visitors int
	Height   int
}

// Row is a line of a top list. Share is a percentage of the list's first row.
type Row struct {
	Name     string
	Detail   string
	URL      string
	Views    int
	Visitors int
	Share    int
}

type dashboard struct {
	service *Service
	admin   *admin.Admin
	routes  *admin.Routes
	title   string
}

func section(env webthings.Env, service *Service) admin.Section {
	return admin.Section{
		ID:    env.Key,
		Title: env.Title,
		Icon:  "numbers",
		Files: files,
		Routes: func(routes *admin.Routes) {
			screen := dashboard{service: service, admin: routes.Admin(), routes: routes, title: env.Title}

			routes.Get("", admin.View, screen.show).As("index")
			routes.Post("/settings", admin.Edit, screen.save).As("settings")
		},
	}
}

func (d dashboard) show(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 366 {
		days = 30
	}

	retention, err := d.service.Retention(r.Context())
	if err != nil {
		d.admin.Fail(w, r, err)

		return
	}

	d.render(w, r, days, &settingsInput{Retention: retention}, nil, http.StatusOK)
}

func (d dashboard) save(w http.ResponseWriter, r *http.Request) {
	var in settingsInput

	errs, err := d.admin.Bind(r, &in)
	if err != nil {
		d.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		d.render(w, r, 30, &in, errs, http.StatusUnprocessableEntity)

		return
	}

	if err := d.service.SetRetention(r.Context(), in.Retention); err != nil {
		d.admin.Fail(w, r, err)

		return
	}

	d.admin.Redirect(w, r, d.routes.URL("index"))
}

func (d dashboard) render(
	w http.ResponseWriter,
	r *http.Request,
	days int,
	in *settingsInput,
	errs form.FieldErrors,
	status int,
) {
	ctx := r.Context()

	// The dashboard shows the views of the last seconds too.
	if err := d.service.Flush(ctx); err != nil {
		slog.ErrorContext(ctx, "flush statistics", "module", d.service.key, "error", err)
	}

	report, err := d.service.Report(ctx, days)
	if err != nil {
		d.admin.Fail(w, r, err)

		return
	}

	view := DashboardView{
		Title:     d.title,
		Days:      report.Days,
		Views:     report.Views,
		Visitors:  report.Visitors,
		Bars:      bars(r.Context(), report.Daily),
		Pages:     pageRows(report.Pages),
		Referrers: countRows(report.Referrers),
		Devices:   countRows(report.Devices),
		Locales:   countRows(report.Locales),
	}

	if len(view.Bars) > 0 {
		view.From = view.Bars[0].Date
		view.To = view.Bars[len(view.Bars)-1].Date
	}

	// Device classes are stored in English and shown in the admin language.
	for i, row := range view.Devices {
		view.Devices[i].Name = d.admin.T(r, deviceLabels[row.Name])
	}

	// Languages are stored as codes and shown by their own name.
	for i, row := range view.Locales {
		if name := admin.LanguageName(row.Name); name != row.Name {
			view.Locales[i].Name = name
			view.Locales[i].Detail = row.Name
		}
	}

	for _, length := range ranges {
		view.Ranges = append(view.Ranges, RangeLink{
			Days:   length,
			URL:    d.routes.URL("index", url.Values{"days": {strconv.Itoa(length)}}),
			Active: length == report.Days,
		})
	}

	if d.admin.Can(r, admin.Permission{Section: d.service.key, Action: admin.Edit}) {
		settings := d.admin.Form(r, *in, d.routes.URL("settings"), "Save")
		view.Settings = &settings
		view.SettingsErrors = errs
	}

	d.admin.Render(w, r, admin.Page{
		Title:    d.title,
		Template: "templates/admin/dashboard.gohtml",
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

func bars(ctx context.Context, days []Day) []Bar {
	busiest := 0

	for _, day := range days {
		busiest = max(busiest, day.Views)
	}

	result := make([]Bar, 0, len(days))

	for _, day := range days {
		bar := Bar{Date: day.Date, Views: day.Views, Visitors: day.Visitors}

		// Days are calendar dates, which no timezone moves.
		if parsed, err := time.Parse(time.DateOnly, day.Date); err == nil {
			bar.Date = admin.Date(ctx, frontend.Civil(parsed))
		}

		if busiest > 0 {
			bar.Height = day.Views * 100 / busiest
		}

		result = append(result, bar)
	}

	return result
}

func pageRows(pages []Page) []Row {
	rows := make([]Row, 0, len(pages))

	for _, page := range pages {
		rows = append(rows, Row{Name: page.Path, Detail: page.Title, URL: page.Path, Views: page.Views, Visitors: page.Visitors})
	}

	return withShares(rows)
}

func countRows(counts []Count) []Row {
	rows := make([]Row, 0, len(counts))

	for _, c := range counts {
		rows = append(rows, Row{Name: c.Name, Views: c.Views, Visitors: c.Visitors})
	}

	return withShares(rows)
}

// withShares sets each row's share of the largest, for the bars behind it.
func withShares(rows []Row) []Row {
	if len(rows) == 0 || rows[0].Views == 0 {
		return rows
	}

	for i := range rows {
		rows[i].Share = rows[i].Views * 100 / rows[0].Views
	}

	return rows
}
