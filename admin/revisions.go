package admin

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/donseba/go-webthings/content"

	form "github.com/donseba/go-form/v2"
)

// LocaleTab links to a page in one content language; LocaleTabs builds
// them.
type LocaleTab struct {
	Code string
	// Name is the language's own name, such as "Nederlands" (LanguageName),
	// with its code when another tab has the same name, such as
	// "Português (pt-BR)" next to "Português (pt)" (LanguageLabel).
	Name   string
	URL    string
	Active bool
	// Complete means the translation can be published; Started means some
	// text exists but it is not complete yet.
	Complete bool
	Started  bool
	// Overview tabs show only the language, without a translation status,
	// as on a list screen; see LocaleTabs.
	Overview bool
}

// RevisionPanel is the publication box beside revisioned editors.
type RevisionPanel struct {
	Selected     int
	Latest       int
	Published    int
	PreviewURL   string
	CanPublish   bool
	CanUnpublish bool
	Publish      form.RenderModel
	Unpublish    form.RenderModel
	History      []HistoryEntry
	// Schedule plans the publication of the selected revision and/or the
	// unpublication of the item; Unschedule cancels the plan. Both are nil
	// without the right to publish.
	Schedule   *form.RenderModel
	Unschedule *form.RenderModel
	// ScheduledRevision is published at PublishAt; UnpublishAt takes the item
	// offline. They are empty when not planned. All times, also those of
	// the history, are in Timezone, the site's timezone.
	ScheduledRevision int
	PublishAt         string
	UnpublishAt       string
	Timezone          string
	// PublishTime and UnpublishTime are PublishAt and UnpublishAt in RFC
	// 3339, for the datetime attribute.
	PublishTime   string
	UnpublishTime string
	// Permanent items cannot be unpublished; see Revisions.Permanent.
	Permanent bool
	// Review is on while the section's review workflow is (see
	// Revisions.Review); the rest describes the selected revision's review.
	Review bool
	ReviewPanel
}

// ReviewPanel is the review part of the publication box. Labels are
// translation keys.
type ReviewPanel struct {
	// State names the selected revision's state, such as "In review";
	// SubmittedBy/At and ReviewedBy/At say who did what and when, with the
	// reviewer's Note.
	State       string
	SubmittedBy string
	SubmittedAt string
	ReviewedBy  string
	ReviewedAt  string
	Note        string
	// SubmittedTime and ReviewedTime are SubmittedAt and ReviewedAt in RFC
	// 3339, for the datetime attribute.
	SubmittedTime string
	ReviewedTime  string
	// NeedsApproval is set when the selected revision cannot be published
	// or scheduled until a reviewer approves it.
	NeedsApproval bool
	// Submit asks for a review (editors); Approve and Reject decide on it
	// (reviewers). Nil when not possible.
	Submit  *form.RenderModel
	Approve *form.RenderModel
	Reject  *form.RenderModel
}

// ReviewInput is a reviewer's decision: a note, required when rejecting,
// and whether to publish an approved revision at once.
type ReviewInput struct {
	Note    string `form:"textarea" label:"Note"                      maxLength:"500"`
	Publish bool   `form:"checkbox" label:"Publish this revision now"`
}

// approveInput is ReviewInput for reviewers who may not publish.
type approveInput struct {
	Note string `form:"textarea" label:"Note" maxLength:"500"`
}

// rejectInput is ReviewInput for a rejection, which needs a note.
type rejectInput struct {
	Note string `form:"textarea" label:"Reason for rejection" required:"true" maxLength:"500"`
}

// ReviewLabel names a review state for editors, as a translation key.
func ReviewLabel(state content.ReviewState) string {
	switch state {
	case content.ReviewPending:
		return "In review"
	case content.ReviewApproved:
		return "Approved"
	case content.ReviewRejected:
		return "Rejected"
	default:
		return "Draft"
	}
}

// ScheduleInput is the form that plans publication changes. Editors enter
// the times in the site's timezone (see Admin.MapForm); zero means not
// planned.
type ScheduleInput struct {
	PublishAt   time.Time `form:"input,datetime-local" label:"Publish at"`
	UnpublishAt time.Time `form:"input,datetime-local" label:"Unpublish at"`
}

// publishInput is ScheduleInput for items that cannot be unpublished.
type publishInput struct {
	PublishAt time.Time `form:"input,datetime-local" label:"Publish at"`
}

// ParseSchedule reads a submitted ScheduleInput as instants in UTC, entered
// in the site's timezone. Empty fields give zero times; a date-time that
// cannot be read is content.ErrSchedule, rather than no time at all.
func (a *Admin) ParseSchedule(r *http.Request) (publishAt, unpublishAt time.Time, err error) {
	var input ScheduleInput

	errs, err := a.MapForm(r, &input)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %w", content.ErrSchedule, err)
	}

	if len(errs) > 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %s", content.ErrSchedule, errs[0])
	}

	return input.PublishAt.UTC(), input.UnpublishAt.UTC(), nil
}

// HistoryEntry is one revision in the history of an item, newest first.
// Restore copies it into a new revision; nil for the revision being shown
// and without the right to edit.
type HistoryEntry struct {
	Number int
	// When is the revision's time as shown; Time is the same in RFC 3339,
	// for the datetime attribute.
	When      string
	Time      string
	Author    string
	Note      string
	URL       string
	Published bool
	Selected  bool
	Restore   *form.RenderModel
	// Review names the revision's review state while the workflow is on,
	// such as "Approved"; empty otherwise.
	Review string
	// PreviewURL shows the revision on the website; empty without preview.
	PreviewURL string
}

// Revisions describes the revision state an editor shows.
type Revisions struct {
	// Section is the section ID, for permission checks.
	Section string
	// URL builds the address of one of the item's editor routes by its
	// short name, such as "edit" or "revision-publish" (see admin/editor),
	// with the item's ID filled in; args add the other route parameters and
	// url.Values for the query, as Admin.URL takes them. The panel only asks
	// for the review and schedule routes while Review or Schedule is set.
	URL    func(name string, args ...any) string
	Locale string
	// Selected is the revision in the editor; Latest is the newest number.
	Selected    content.Revision
	Latest      int
	PublishedID string
	History     []content.Revision
	// Names maps author IDs to display names.
	Names map[string]string
	// Preview adds a preview link when the website can render previews.
	Preview bool
	// Schedule is the item's planned publication change; nil when the content
	// cannot be scheduled.
	Schedule *content.Schedule
	// Permanent items must stay on the website, such as the home page: the
	// panel offers no unpublish and plans publications only.
	Permanent bool
	// Review is on while the section's review workflow is (see
	// content.WithReview): only approved revisions can be published or
	// scheduled, and the panel offers submitting, approving and rejecting.
	Review bool
}

// RevisionPanel builds the publication box: publish, unpublish, preview and history.
func (a *Admin) RevisionPanel(r *http.Request, revisions Revisions) RevisionPanel {
	locale := url.Values{"locale": {revisions.Locale}}
	selected := revisions.Selected
	canPublish := a.Can(r, Permission{Section: revisions.Section, Action: Publish})
	approved := !revisions.Review || selected.Review.State == content.ReviewApproved

	panel := RevisionPanel{
		Selected:     selected.Number,
		Latest:       revisions.Latest,
		CanPublish:   canPublish && approved && selected.ID != "" && selected.ID != revisions.PublishedID,
		CanUnpublish: canPublish && revisions.PublishedID != "" && !revisions.Permanent,
		Publish: a.Form(
			r,
			struct{}{},
			revisions.URL("revision-publish", "revision", selected.ID, locale),
			"Publish this revision",
		),
		Unpublish: a.Form(r, struct{}{}, revisions.URL("unpublish", locale), "Unpublish"),
		Permanent: revisions.Permanent,
	}

	if revisions.Preview && selected.ID != "" {
		panel.PreviewURL = revisions.URL("revision-preview", "revision", selected.ID, locale)
	}

	if revisions.Review {
		a.reviewPanel(r, revisions, &panel)
	}

	canEdit := a.Can(r, Permission{Section: revisions.Section, Action: Edit})
	location := a.Location(r)

	// The history shows times in the site's timezone, like the schedule.
	panel.Timezone = location.String()

	for _, revision := range revisions.History {
		entry := HistoryEntry{
			Number:    revision.Number,
			When:      DateTime(r.Context(), revision.CreatedAt),
			Time:      machineTime(revision.CreatedAt, location),
			Author:    revisions.Names[revision.Author],
			Note:      revision.Note,
			URL:       revisions.URL("edit", url.Values{"revision": {revision.ID}, "locale": {revisions.Locale}}),
			Published: revision.ID == revisions.PublishedID,
			Selected:  revision.ID == selected.ID,
		}

		if revisions.Review {
			entry.Review = ReviewLabel(revision.Review.State)
		}

		if revisions.Preview {
			entry.PreviewURL = revisions.URL("revision-preview", "revision", revision.ID, locale)
		}

		if entry.Published {
			panel.Published = revision.Number
		}

		if canEdit && !entry.Selected {
			restore := a.Form(
				r,
				struct{}{},
				revisions.URL("revision-restore", "revision", revision.ID, locale),
				"Restore as new draft",
			)
			entry.Restore = &restore
		}

		panel.History = append(panel.History, entry)
	}

	// The schedule stays open while a revision waits for approval: an
	// unpublication needs none, and planning its publication is refused
	// with the reason.
	if revisions.Schedule != nil {
		a.schedulePanel(r, revisions, canPublish, &panel)
	}

	// Publishing or unpublishing by hand cancels the plan, so editors are
	// told before they lose it.
	if revisions.Schedule != nil && !revisions.Schedule.IsZero() {
		panel.Publish = a.Confirm(r, panel.Publish, "Publishing now cancels the schedule. Continue?")
		panel.Unpublish = a.Confirm(r, panel.Unpublish, "Unpublishing now cancels the schedule. Continue?")
	}

	return panel
}

// schedulePanel shows the planned changes and the forms to plan and cancel.
func (a *Admin) schedulePanel(r *http.Request, revisions Revisions, canPublish bool, panel *RevisionPanel) {
	locale := url.Values{"locale": {revisions.Locale}}
	schedule := *revisions.Schedule
	location := a.Location(r)
	input := ScheduleInput{PublishAt: schedule.PublishAt, UnpublishAt: schedule.UnpublishAt}

	panel.Timezone = location.String()

	if !schedule.PublishAt.IsZero() {
		panel.PublishAt = DateTime(r.Context(), schedule.PublishAt)
		panel.PublishTime = machineTime(schedule.PublishAt, location)

		for _, revision := range revisions.History {
			if revision.ID == schedule.RevisionID {
				panel.ScheduledRevision = revision.Number
			}
		}
	}

	if !schedule.UnpublishAt.IsZero() {
		panel.UnpublishAt = DateTime(r.Context(), schedule.UnpublishAt)
		panel.UnpublishTime = machineTime(schedule.UnpublishAt, location)
	}

	if !canPublish || revisions.Selected.ID == "" {
		return
	}

	action := revisions.URL("revision-schedule", "revision", revisions.Selected.ID, locale)
	plan := a.Form(r, input, action, "Schedule")

	if revisions.Permanent {
		plan = a.Form(r, publishInput{PublishAt: input.PublishAt}, action, "Schedule")
	}

	panel.Schedule = &plan

	if !schedule.IsZero() {
		cancel := a.Form(r, struct{}{}, revisions.URL("unschedule", locale), "Cancel schedule")
		panel.Unschedule = &cancel
	}
}

// reviewPanel fills the review part of the panel for the selected revision:
// its state, and the forms the user may use on it.
func (a *Admin) reviewPanel(r *http.Request, revisions Revisions, panel *RevisionPanel) {
	locale := url.Values{"locale": {revisions.Locale}}
	selected := revisions.Selected
	review := selected.Review

	panel.Review = true
	panel.State = ReviewLabel(review.State)
	panel.SubmittedBy = revisions.Names[review.SubmittedBy]
	panel.SubmittedAt = DateTime(r.Context(), review.SubmittedAt)
	panel.SubmittedTime = machineTime(review.SubmittedAt, a.Location(r))
	panel.ReviewedBy = revisions.Names[review.ReviewedBy]
	panel.ReviewedAt = DateTime(r.Context(), review.ReviewedAt)
	panel.ReviewedTime = machineTime(review.ReviewedAt, a.Location(r))
	panel.Note = review.Note

	published := selected.ID == revisions.PublishedID
	panel.NeedsApproval = selected.ID != "" && !published && review.State != content.ReviewApproved

	if selected.ID == "" || published {
		return
	}

	canEdit := a.Can(r, Permission{Section: revisions.Section, Action: Edit})
	canReview := a.Can(r, Permission{Section: revisions.Section, Action: Review})
	canPublish := a.Can(r, Permission{Section: revisions.Section, Action: Publish})

	switch review.State {
	case content.ReviewDraft, content.ReviewRejected:
		if canEdit {
			submit := a.Form(
				r,
				struct{}{},
				revisions.URL("revision-submit", "revision", selected.ID, locale),
				"Submit for review",
			)
			panel.Submit = &submit
		}
	case content.ReviewPending:
		if !canReview {
			return
		}

		approveURL := revisions.URL("revision-approve", "revision", selected.ID, locale)

		var approve form.RenderModel
		if canPublish {
			approve = a.Form(r, ReviewInput{}, approveURL, "Approve")
		} else {
			approve = a.Form(r, approveInput{}, approveURL, "Approve")
		}

		reject := a.Form(r, rejectInput{}, revisions.URL("revision-reject", "revision", selected.ID, locale), "Reject")
		panel.Approve = &approve
		panel.Reject = &reject
	}
}

// machineTime is t in location as RFC 3339, for the datetime attribute of
// a <time> element; a zero time gives "".
func machineTime(t time.Time, location *time.Location) string {
	if t.IsZero() {
		return ""
	}

	return t.In(location).Format(time.RFC3339)
}
