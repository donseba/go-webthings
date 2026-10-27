package registrations

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
)

// funcs are the template functions Registrations offers to every template.
//
// go-clue:funcmap
func funcs() template.FuncMap {
	return template.FuncMap{
		//go-clue:sig func(key string, subjectID string) github.com/donseba/go-webthings/modules/registrations.Availability
		"registration": availability,
		//go-clue:sig func(key string) string
		"registerURL": registerURL,
		//go-clue:sig func(key string) string
		"registrationResult": result,
		//go-clue:sig func(key string) string
		"registrationCode": code,
		//go-clue:sig func(key string, subjectID string) html/template.HTML
		"registrationForm": registrationForm,
	}
}

// availability tells whether visitors can register for a subject, with the
// notes in the page's language, for example
// {{with registration "signup" .ID}}{{if .Open}}…{{end}}{{end}} on an event
// page. An unknown instance or subject is closed and not Configured.
func availability(ctx context.Context, key, subjectID string) Availability {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return Availability{SubjectID: subjectID, Closed: true}
	}

	found, err := service.Availability(ctx, subjectID, frontend.Locale(ctx))
	if err != nil {
		slog.ErrorContext(ctx, "template registration", "module", key, "error", err)
	}

	return found
}

// registerURL is the address sign-up forms post to, for example
// <form method="post" action="{{registerURL "signup"}}">, or "" for an
// unknown instance.
func registerURL(ctx context.Context, key string) string {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return ""
	}

	return service.registerURL()
}

// result is the outcome of the visitor's last post to the instance on this
// page: "registered", "full", "closed", "invalid", "cancelled", "throttled"
// or empty.
func result(ctx context.Context, key string) string {
	request := frontend.Request(ctx)
	if request == nil {
		return ""
	}

	return frontend.Result(request, key, results...)
}

// code is the visitor's registration code after registering on a page when
// the instance is placed on no page of its own, for example
// {{with registrationCode "signup"}}<p>Your code: {{.}}</p>{{end}}; "" otherwise.
func code(ctx context.Context, key string) string {
	request := frontend.Request(ctx)
	if request == nil || result(ctx, key) != ResultRegistered {
		return ""
	}

	return normalizeCode(request.URL.Query().Get("code"))
}
