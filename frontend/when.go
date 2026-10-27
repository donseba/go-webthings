package frontend

import "time"

// When is when something takes place, such as an event, for templates to
// show in one line: Start and End in the zone they are shown in, such as an
// event's own timezone, or as civil times (see Civil). End is zero when it
// has no end; for something that takes AllDay it is the last day. The
// events module's Event and the registrations module's Subject give theirs
// with a When method, so one partial shows both:
//
//	{{/* @dot github.com/donseba/go-webthings/frontend.When */}}
//	{{if .AllDay}}{{dateRange .Start .End "full"}}
//	{{else if .SameDay}}{{date .Start "full"}} · {{timeRange .Start .End}}
//	{{else}}{{date .Start "full"}}, {{time .Start}} – {{date .End "full"}}, {{time .End}}{{end}}
type When struct {
	Start  time.Time
	End    time.Time
	AllDay bool
}

// IsZero reports whether the time is not known: Start is zero, as for a
// registration for something other than an event.
func (w When) IsZero() bool {
	return w.Start.IsZero()
}

// SameDay reports whether it ends on the day it starts, by the calendar of
// Start's and End's own zones, or has no end.
func (w When) SameDay() bool {
	if w.End.IsZero() {
		return true
	}

	startYear, startMonth, startDay := w.Start.Date()
	endYear, endMonth, endDay := w.End.Date()

	return startYear == endYear && startMonth == endMonth && startDay == endDay
}
