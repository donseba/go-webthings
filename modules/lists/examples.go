package lists

// listExample is the starting point for lists/list.gohtml. Every entry has
// id="{{.ID}}": the site search links to entries on this page at #<ID>.
const listExample = `{{/* @dot github.com/donseba/go-webthings/modules/lists.ListView */}}
<main id="content">
    <h1>{{.Title}}</h1>

    {{range .Entries}}
        <section id="{{.ID}}">
            {{with .ImageURL}}<img src="{{image . "400x300 crop"}}" alt="">{{end}}
            <h2>{{.Title}}</h2>
            {{.Body}}
            {{with .Link}}<p><a href="{{.}}">Read more</a></p>{{end}}
        </section>
    {{else}}
        <p>Nothing here yet.</p>
    {{end}}
</main>
`
