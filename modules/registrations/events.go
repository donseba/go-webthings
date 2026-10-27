package registrations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/events"
)

// ErrNoEvents is returned when the instance is linked to a key (ForEvents)
// that names no events instance.
var ErrNoEvents = errors.New("registrations_no_events")

// Subject is what visitors register for, as the admin and e-mail show it.
type Subject struct {
	ID    string
	Title string
	// Start is when an event starts and End when it ends, in the event's
	// timezone; zero for other subjects. End is zero when the event has none,
	// and the last day of all-day events.
	Start  time.Time
	End    time.Time
	AllDay bool
	// URL is the event's page, when visitors can see it.
	URL string
}

// When is when the event takes place, as events.Event.When gives it; zero
// for other subjects.
func (s Subject) When() frontend.When {
	return frontend.When{Start: s.Start, End: s.End, AllDay: s.AllDay}
}

// eventSource is the linked events instance; *events.Service is one.
type eventSource interface {
	Event(ctx context.Context, id, locale string) (events.Info, error)
	Events(ctx context.Context, locale string) ([]events.Info, error)
}

// linkedEvents returns the events instance the subjects are, resolved when
// needed since other instances are only reachable once the app runs.
func linkedEvents(service func(key string) any, key string) func() (eventSource, error) {
	return func() (eventSource, error) {
		source, ok := service(key).(*events.Service)
		if !ok || source == nil {
			return nil, fmt.Errorf("%w: %q", ErrNoEvents, key)
		}

		return source, nil
	}
}

// linked reports whether the subjects are the events of an events instance.
func (s *Service) linked() bool {
	return s.events != nil
}

// event returns one event of the linked instance in locale (any title when
// empty), or ErrNotFound.
func (s *Service) event(ctx context.Context, id, locale string) (Subject, error) {
	source, err := s.events()
	if err != nil {
		return Subject{}, err
	}

	info, err := source.Event(ctx, id, locale)
	if err != nil {
		return Subject{}, err
	}

	return eventSubject(info), nil
}

// eventSubjects returns every event of the linked instance in locale, latest
// start first.
func (s *Service) eventSubjects(ctx context.Context, locale string) ([]Subject, error) {
	source, err := s.events()
	if err != nil {
		return nil, err
	}

	list, err := source.Events(ctx, locale)
	if err != nil {
		return nil, err
	}

	subjects := make([]Subject, 0, len(list))

	for _, info := range list {
		subjects = append(subjects, eventSubject(info))
	}

	return subjects, nil
}

func eventSubject(info events.Info) Subject {
	return Subject{
		ID:     info.ID,
		Title:  info.Title,
		Start:  info.Start,
		End:    info.End,
		AllDay: info.AllDay,
		URL:    info.URL,
	}
}
