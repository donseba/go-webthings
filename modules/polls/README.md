# Polls

Ask visitors a question with a few options and show the results. Every visitor has one vote per poll.

Polls appear on any page through a template function, for example on the home page or beside an article. Placing the instance on a page is optional; that page lists every poll.

## Add it to the site

```go
app.Use(polls.Module("polls", "Polls"))
```

The key (`polls`) is the instance's name in URLs and template functions and must be unique on the site. The title (`Polls`) is what editors see in the admin navigation and the page editor.

## Write a poll

In the admin, open the instance (`/admin/polls`; `/admin` is the default [admin path](../../docs/configuration.md#admin-path)) and choose **New poll**:

- **Question**, and an optional **Short name**, a name for templates such as `favourite-colour`.
- **Options**: one per line, at least two and at most 20.
- **Closes at**: when voting ends, in the site's timezone. Leave it empty to keep the poll open.
- **Show results before voting**: otherwise visitors see the results after voting or once the poll is closed.

Publish the poll when it is ready. It appears in each language that has a question and at least two options.

Options are matched by line in every language, so translate them in the same order and add new options at the end. Changing the text of a line renames that option and keeps its votes. Options that have votes cannot be removed until the votes are reset.

New polls go to the top of the list, and editors can move them up and down. That order is the order of the `polls` function, and the top poll is the one `{{pollBox "polls" ""}}` shows. The list shows the number of votes per poll and which polls are closed.

**Move to trash** moves a poll to the [trash](../../docs/editing.md#the-trash) with its votes, where it can be restored or deleted for good. Restoring is refused when another poll has taken its short name meanwhile.

## Show a poll on any page

```gotemplate
{{pollBox "polls" "favourite-colour"}}
{{pollBox "polls" ""}}
```

`pollBox` renders a published poll by short name or ID, or for `""` the top poll of the list, the newest one unless editors moved the polls: the question, the vote form, the results with bars when they may be shown, and a notice after voting. Without such a poll it renders nothing.

The box comes with plain, accessible default markup. A theme replaces it with `<key>/box.gohtml`, or `polls/box.gohtml` for every instance; `task templates -- polls` writes the default to start from. It uses stable `wt-*` classes (`wt-poll`, `wt-poll__results`, `wt-poll__bar`, …), laid out by the optional stylesheet `webthings.css`, which the layout links with `{{styles "webthings.css" "site.css"}}` (see [The base stylesheet](../../docs/themes.md#the-base-stylesheet)). A result's bar is a `<meter class="wt-poll__bar" min="0" max="100" aria-hidden="true">` whose value is the result's percentage, which also shows as text; it has no `style` attribute. `webthings.css` draws it as a `--wt-bar` track filled with `--wt-accent`. go-form renders the options with the site's form theme, as a `fieldset` with the question as its `legend`, by default `wt-form__field wt-form__fieldset` around `wt-form__options` with a `wt-form__option` each. Its texts go through `tl`, so the site's translations translate them.

For markup of your own, `poll` returns one poll (or nil), `polls` the first published polls (`{{polls "polls" 0}}` for all) and `pollURL` the address votes are posted to:

```gotemplate
{{with poll "polls" "favourite-colour"}}
    <section id="{{.Anchor}}">
        <h2>{{.Question}}</h2>
        {{if eq .Result "voted"}}<p>Thanks for voting.</p>{{end}}

        {{if .CanVote}}
            {{form_open .Form}}
                {{form_field .Form "poll" nil}}
                {{form_field .Form "option" nil}}
                <button>Vote</button>
            {{form_close}}
        {{end}}

        {{if .ShowResults}}
            {{range .Options}}
                <p>{{.Label}}: {{.Percent}}%{{if .Chosen}} (your vote){{end}}</p>
                <meter min="0" max="100" value="{{.Percent}}">{{.Percent}}%</meter>
            {{end}}
            <p>{{.Total}} votes</p>
        {{end}}
    </section>
{{end}}
```

`CanVote` is set while the poll is open and the visitor has not voted. `Percent` is rounded, `ClosesAt` is in the site's timezone and zero for a poll that stays open.

`.Form` is the vote form: `form_open` writes the form tag, posting to `.Action`, with the spam trap and the page to return to, the element whose id is `.Anchor` (`<key>-<poll ID>`), so several polls can share a page. `poll` is a hidden field with the poll's ID. `option` renders the poll's options as radio buttons, required, in a `fieldset` with the question as its `legend`, in the site's form theme; the ids start with `.Anchor`. See [Template functions](../../docs/template-functions.md#forms). Radio inputs written by hand, named `option` with an option's `.ID` as value, work too.

## Poll page

In the admin, open a page and choose the instance under **Shows on this page**. The page renders `polls/list.gohtml` with a `polls.ListView`: the page's published title, the instance key and every published poll in the editors' order. `task templates -- polls` writes a starting point for it too. The box works here:

```gotemplate
{{/* @dot github.com/donseba/go-webthings/modules/polls.ListView */}}
<h1>{{.Title}}</h1>

{{range .Polls}}
    {{pollBox $.Key .ID}}
{{end}}
```

## Voting

The vote form posts to `/_/<key>/vote` (`{{pollURL "polls"}}`) with the fields of `polls.VoteInput`, `poll` (the poll's ID) and `option` (an option's ID), plus the spam trap and the way back that `form_open` adds. The visitor returns to the poll's `.Anchor` on the page with `?<key>=<result>&<key>_poll=<poll ID>`. Only that poll gets the result as `.Result`, so other polls on the page show none; unknown results are ignored:

- `voted`: the vote was counted;
- `already`: this visitor already voted on this poll;
- `closed`: the poll is closed or no longer published;
- `invalid`: no option, or an option of another poll, was chosen;
- `throttled`: more than 20 votes in a minute from this visitor; nothing was counted.

With `invalid`, the poll shows the vote form again with the message in the page's language at its options. The box and `form_field` do this without a change to the template; inputs written by hand do not. See [A post with mistakes](../../docs/visitor-forms.md#a-post-with-mistakes). A vote that fails for another reason, such as an unreachable database, gets the site's error page, [500.gohtml](../../docs/templates.md#500gohtml).

Bots that fill in the spam trap are told their vote counted; nothing is stored.

The first vote sets a `wt_poll` cookie that recognises the visitor for a year. Visitors without it are recognised by their IP address, so scripts that drop cookies get one vote per address; behind a reverse proxy, list it in `Config.TrustedProxies`. When someone else on that address already voted, the visitor gets a cookie of their own and can vote next time. Only a hash of the visitor's ID is stored, and addresses are keyed with a secret of the instance first, so votes cannot be traced back to addresses.

Only the cookie marks a poll as voted on a page. The module names it to the site's output cache (`Env.PersonalCookie`), which keeps pages apart for each value of `wt_poll`, so a visitor who voted sees their choice and others the vote form; see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache). Only a vote that counted makes the cache render the pages again: every other result tells it that nothing changed (`frontend.NothingChanged`).

## Results

**Results** on the instance's admin page (`/admin/polls/results`) shows every poll with its status (draft, open or closed), the votes and percentage per option and the total. **Reset votes** removes all votes of a poll after confirmation; it needs the delete right.

## From Go

`polls.Of(app, "polls")` returns the instance once the app has started. `Latest` and `Find` take the visitor's request, or nil, to mark what that visitor chose; `Latest` with a limit of 0 or less returns all polls:

```go
survey := polls.Of(app, "polls")

newest, err := survey.Latest(ctx, nil, "en", 1)
poll, err := survey.Find(ctx, r, "en", "favourite-colour")
```

`Vote` counts a vote for any visitor ID, such as a member number, and returns `ErrAlreadyVoted`, `ErrClosed` or `ErrInvalidOption`; `ResetVotes` clears a poll. `Store()` gives write access for seed data. Votes and resets made from Go, and writes through `Store()`, show at once, also on pages in the site's output cache; see [Modules and the output cache](../../docs/modules.md#modules-and-the-output-cache). Code may choose its own option IDs, unique in the poll and at most 36 bytes:

```go
store := survey.Store()

saved, err := store.Save(ctx, content.Draft[polls.Fields, polls.Text]{
    Fields: polls.Fields{Options: polls.Lines{"red", "green", "blue"}},
    Translations: content.SetText(nil, "en", polls.Text{
        Question: "Which colour should the new logo have?",
        Slug:     "logo-colour",
        Options:  polls.Lines{"Red", "Green", "Blue"},
    }),
})

err = store.Publish(ctx, saved.ID, "", "en")
```

## Related modules

- [Forms](../forms/README.md) asks longer questions, with an inbox for the answers.
- [Comments](../comments/README.md) lets visitors say more than a vote.
