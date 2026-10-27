package events

// Starting points for the templates (see webthings.App.WriteTemplates). The
// dates use the template functions date, time, dateRange and timeRange, so
// they appear in the language of the page, in one line by Event.When
// (frontend.When): the days of an all-day event, the day and times of one
// that ends on the day it starts, and the day and time of start and end of
// one that runs over several days.

// listExample is the starting point for events/list.gohtml.
const listExample = `{{/* @dot github.com/donseba/go-webthings/modules/events.ListView */}}
<main id="content">
    <h1>{{.Title}}</h1>

    <nav>
        {{if .Past}}<a href="{{.UpcomingURL}}">Upcoming</a>{{else}}<a href="{{.PastURL}}">Past events</a>{{end}}
    </nav>

    {{range .Events}}
        <article>
            {{with .ImageURL}}<img src="{{image . "400x300 crop"}}" alt="">{{end}}
            <h2><a href="{{.URL}}">{{.Title}}</a></h2>
            <p>
                <time datetime="{{.Start.Format "2006-01-02T15:04:05Z07:00"}}">{{with .When}}{{if .AllDay}}{{dateRange .Start .End "full"}}{{else if .SameDay}}{{date .Start "full"}} · {{timeRange .Start .End}}{{else}}{{date .Start "full"}}, {{time .Start}} – {{date .End "full"}}, {{time .End}}{{end}}{{end}}</time>
                {{with .Location}} · {{.}}{{end}}
            </p>
            {{with .Summary}}<p>{{.}}</p>{{end}}
        </article>
    {{else}}
        <p>No events.</p>
    {{end}}
</main>
`

// eventExample is the starting point for events/event.gohtml.
const eventExample = `{{/* @dot github.com/donseba/go-webthings/modules/events.Event */}}
<main id="content">
    <article>
        <h1>{{.Title}}</h1>
        <p>
            <time datetime="{{.Start.Format "2006-01-02T15:04:05Z07:00"}}">{{with .When}}{{if .AllDay}}{{dateRange .Start .End "full"}}{{else if .SameDay}}{{date .Start "full"}} · {{timeRange .Start .End}}{{else}}{{date .Start "full"}}, {{time .Start}} – {{date .End "full"}}, {{time .End}}{{end}}{{end}}</time>{{if not .AllDay}} ({{.Timezone}}){{end}}
        </p>
        {{with .Location}}<p>{{.}}{{with $.Address}}<br>{{.}}{{end}}</p>{{end}}
        {{if .ImageURL}}<img src="{{.ImageURL}}" srcset="{{.ImageSrcset}}" sizes="100vw" alt="">{{end}}
        {{with .Summary}}<p>{{.}}</p>{{end}}
        <div>{{.Body}}</div>
    </article>
</main>
`
