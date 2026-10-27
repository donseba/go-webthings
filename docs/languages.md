# Languages

A site has one or more content languages. Every address starts with a language code, editors translate each page and module item, and the templates' own texts are translated in .po files.

## Content languages

The first start takes the languages from `Config.Locales`:

```go
app := webthings.New(webthings.Config{
    Name:    "Atelier Noord",
    Locales: locales.Config{Default: "nl", Enabled: []string{"nl", "en"}},
})
```

Without `Locales` the site starts in English only. After the first start the languages are part of the site settings and `Config.Locales` is ignored: editors change them under **Site settings**, in **Default language**, **Languages** and **Other languages**.

- **Languages** has a checkbox for 47 common languages, each with its own name and its code, such as "Nederlands (nl)" or "Deutsch (de)", the same for every editor. A regional variant gets its language's name, such as "Português (pt-BR)", and a code without a known name is shown as the code. Enabled languages that are not among them get a checkbox too.
- **Other languages** adds languages that are not listed, as codes separated by commas. Codes are language tags such as `pt-BR` or `en-US`; underscores are not allowed.
- **Default language** offers every language of the list, so a language ticked in the same save can be the default. A default that is not ticked under **Languages** is refused at the field: "Tick the default language under Languages as well."
- The language switch shows the languages in the order they were added: a new language comes last.

## Addresses per language

Every address starts with a language code: `/nl/over-ons` and `/en/about` are the same page in two languages, each with its own slug. `/` redirects to the home page of the default language.

The code in the address is written as in the site settings; `/EN/about` redirects to `/en/about`. An address with a code that is not enabled, or without a code, is "not found". [Pages](pages.md#canonical-addresses) lists all redirects.

## Translating content

Every editor in the admin has a tab per language: pages, module items, media and the site settings. A tab names its language in that language, such as "Nederlands" or "Deutsch", with the code only where two languages share a name, such as "Português (pt)" and "Português (pt-BR)". Each tab shows the state of that language:

- **Translated**: the item can be shown in this language.
- **Incomplete translation**: some text exists, but not enough to show it.
- **Missing translation**: nothing has been written yet.

The page tree and the overview of each module's items have a tab per language too. These tabs only name the language, since a whole list has no single state; instead, each item that is not complete in that language shows "Missing translation".

What makes a translation complete depends on the content. A page needs a title, and every page except Home also a **Web address**, which is made from the title when it is left empty; a news post needs a title and a web address, made from its title in the same way. Each module's README says what it needs.

- Publishing needs a complete translation in the default language. The admin answers "Complete the default language before publishing." otherwise.
- In other languages an item is shown once its translation is complete. Until then it is left out of lists, menus, search and `sitemap.xml` in that language.
- A page is on the site in a language only when its parent pages are translated there too.
- A revision of a page, post or event holds all its languages, so publishing it publishes every translation at once.

The site settings have their own texts per language: **Site title**, **Tagline**, **SEO title**, **SEO description** and **Footer**, which is formatted text like a page's content. Media files have a title and alt text per language; see [Media and images](media.md).

## Adding or removing a language

To add a language, tick it under **Languages** in the site settings, or write its code under **Other languages**, and save. Then:

1. Fill in the site texts in the new language's tab of the site settings.
2. Translate the home page and publish it. Every page of a language hangs below the home page, so `/de/` is "not found" until the home page has a German title.
3. Translate and publish the other pages and the module content.
4. Add a `.po` file for the templates' texts (see below).

A language whose code is the first segment of the [admin path](configuration.md#admin-path), such as `en` with the admin at `/en/admin`, cannot be enabled: the admin would take its addresses. The site settings refuse it with "A language cannot have the code that one of the website's own addresses starts with, such as the admin's: its pages could not be reached.", and a start with such a language enabled stops with an error. While the [content API](api.md) is on, the code `api` cannot be enabled either.

Unticking a language takes its addresses offline. Its translations stay in the database and come back when the language is enabled again.

Changing the default language changes where `/` leads, and from then on publishing needs a complete translation in the new default language.

## The language switch

The layout gets `.Languages`, a list of `frontend.Language` with `Locale`, `URL`, `Active` (the current language) and `Default` (the site's default language). It links to the same content in each language where that content is published:

- on a page, to the same page in the languages it has;
- on a module screen, to the same list or item, such as a post, in the languages it is translated in; a screen whose module gives no translations (`frontend.Content.Alternates`) links to the module's page in the other languages and to itself in its own language (see [Search and SEO](search-and-seo.md));
- on the search screen, to the search in each language, keeping the query;
- on "not found", to the home page of each language;
- on a preview, as on the published page or item: a page's published languages, or a post's or event's translations.

```gotemplate
<html lang="{{.Locale}}">
<head>
    {{slot "head"}}
</head>
<body>
    <nav aria-label="{{tl localizer "Language"}}">
        {{range .Languages}}
            <a href="{{.URL}}" hreflang="{{.Locale}}" lang="{{.Locale}}" {{if .Active}}aria-current="true"{{end}}>{{.Locale}}</a>
        {{end}}
    </nav>
```

Core's head tags, which `{{slot "head"}}` places (see [The head region](themes.md#the-head-region)), link to the same content in each language with `hreflang`, and, when the content is in more than one language, to its address in the default language as `hreflang="x-default"`, for visitors whose language the site does not have; `sitemap.xml` does the same. `.Locale` is the current language in the layout; other templates get it with `{{locale}}`. [Search and SEO](search-and-seo.md#canonical-and-alternate-links) covers the alternate links.

## Translating the templates' texts

The texts written in the templates themselves, such as "Read more" or "Back to the home page", are translated with [gettext](https://www.gnu.org/software/gettext/) files. The starter loads them in `main.go`:

```go
translations, err := frontend.Translations("translations")
if err != nil {
    log.Fatal(err)
}

app := webthings.New(webthings.Config{
    Templates:    os.DirFS("templates"),
    Translations: translations,
})
```

`frontend.Translations` reads `translations/<code>.po` for every language, such as `nl.po` and `en.po`, and returns go-translator's `*translator.Translator`. In `Config.Translations`, it gives every template go-translator's four template functions; `localizer` is the page's language:

| Function | Use |
| --- | --- |
| `{{tl localizer "Read more"}}` | a text |
| `{{tl localizer "%d of %d" .Number .Total}}` | a text with values, in `fmt` style |
| `{{tn localizer "%d comment" "%d comments" .Count .Count}}` | singular and plural, chosen by the number |
| `{{ctl localizer "menu" "Home"}}` | a text with a context, for words that translate differently in different places |
| `{{ctn localizer "menu" "%d item" "%d items" .Count .Count}}` | plural with a context |

Core translates its own texts with the same files: the title of the 404 page, the heading and the message of the error page (see [Templates](templates.md#500gohtml)), the names of social networks, the title of feeds, go-form's texts in visitor forms, and the names of months and days and the date patterns (see [Dates, times and money](#dates-times-and-money)). Without `Config.Translations`, every text shows as written. `app.Frontend.SetTranslations` sets the translations after `Start`, before the site serves.

Write the texts in English. A text without a translation is shown as written, so a new language works before its file exists. `tn` and `ctn` then show the singular when the number is 1 and the plural otherwise, as in English.

A page's language finds its file whatever the spelling: `nl-BE` uses `nl_BE.po` or `nl-BE.po`. A text that file lacks comes from the file of the language alone, `nl.po`, and then from another region of the same language, `nl_NL.po` first. So `nl_BE.po` only needs the texts that differ from `nl.po`, and a site with only `nl_NL.po` translates `nl` pages too. The file of another language is never used: German without a file of its own shows the texts as written. A file serves the language its name names, whatever its `Language` header says, so name each file by its language code: a file such as `dutch.po` serves no page.

go-form's texts in the [visitor forms](visitor-forms.md#the-fields) are translated with these files too, in the page's language. That includes go-form's own texts, such as the "(required)" that screen readers announce, and the messages a visitor's post with mistakes gets at its fields, such as "This field is required.", in the language of the page the form was on; see [Visitor forms](visitor-forms.md#a-post-with-mistakes). One of go-form's own texts that the site's file does not translate shows in go-form's catalogue of the page's language, or of the language alone, such as `nl` for `nl-BE`. go-form brings English and Dutch, so `nl` and `nl-BE` pages show "Dit veld is verplicht." without an entry in `nl.po`; other languages show its texts in English. Nothing is added to the site's files. The files are read when the site starts: restart it after changing them.

### Keeping the files up to date

The starter's `Taskfile.yml` has two tasks for this:

```sh
task translations          # write every text to translations/translations.pot
task translations:check    # list what each language misses
```

1. `task translations` scans every template of every theme, plus the default templates, components and e-mail of the modules and core that not every theme replaces, and writes their texts to `translations/translations.pot`. It adds the texts core translates in Go, with `core:texts` as their reference: "Not found", the title of the 404 page, "Something went wrong. Please try again.", the message of the error page (see [Templates](templates.md#500gohtml)), the status texts of its heading ("Not Found", "Too Many Requests", "Internal Server Error" and "Service Unavailable"), the title of the feeds, "%[1]s from %[2]s", and the names of the social networks. It adds the names of months and days and the date patterns, with their contexts, as `core:calendar` (see [Dates, times and money](#dates-times-and-money)). When a template uses `form_open`, `form_field` or `form_render`, it also adds the labels, help texts, placeholders, option labels, submit texts and messages of the modules' visitor forms (`Module.Forms`), as `module:<type>/forms`. It prints how many texts it wrote, from how many templates.
2. Open a language's `.po` file in [Poedit](https://poedit.net/), choose *Translation › Update from POT File…* and pick `translations/translations.pot`. For a new language, create its file from the same POT file.
3. Translate the new texts, save and restart the site.

`task translations:check` lists the texts each `.po` file still misses, or has with an empty translation, and fails when any are missing, so it can run in CI. `task translations:check -- --csv` writes the list as CSV for a spreadsheet. It lists the calendar texts only for languages without built-in names: not for `en.po` and `nl.po`, nor for regions of English and Dutch, such as `en_US.po` and `nl_BE.po`, which need not contain them.

The tasks run `go run . translations` and `go run . translations check`, which call `App.Translations` (see [`translations.go`](../translations.go) and the [`translations`](../translations/command.go) package). `App.TranslationCatalog()` returns the same texts as go-translator's `*extract.Catalog`, for tools of your own. They parse the templates, so they find every text, also on pages nobody visited, and the running site never writes to its catalogues.

Only literal texts can be found. A call such as `{{tl localizer .Status}}` is reported as a warning, and updating from the POT file would mark its translations obsolete. Name each possible text in a literal call, for example in a `{{define}}` that is never used:

```gotemplate
{{define "status-texts"}}{{tl localizer "open"}}{{tl localizer "full"}}{{tl localizer "cancelled"}}{{end}}
```

The modules' default components and e-mail use `tl` too, so the same files translate them.

## Dates, times and money

`date`, `time`, `dateRange`, `timeRange`, `weekday` and `money` write in the page's language:

```gotemplate
{{date .Start "full"}}             Sunday 25 October 2026 · zondag 25 oktober 2026
{{time .Start}}                    14:00 (2:00 PM in en-US)
{{money .PriceCents "EUR"}}        €45.00 · € 45,00
```

Times stored in UTC are shown in the site's timezone (**Site settings › Timezone**). That field lists the time zones by name, each with its current offset from UTC, such as "Europe/Amsterdam (UTC+02:00)". English and Dutch month and day names and date patterns are built in; `en` writes the day first and `en-US` the month first, with a 12-hour clock. Money uses the language's separators in every language.

The site's `.po` files give the names and patterns of other languages, and can replace the built-in ones of English and Dutch. Each is an English name with a context (`msgctxt`), so the same word can translate differently, such as "May" the month and "May" its short name:

| Context | msgids |
| --- | --- |
| `month` | `January` … `December` |
| `month-short` | `Jan` … `Dec` |
| `day` | `Sunday` … `Saturday` |
| `day-short` | `Sun` … `Sat` |
| `time` | `AM` and `PM` |
| `date` | the patterns of the styles: `short`, `medium`, `long`, `full` and `time` |

```text
msgctxt "month"
msgid "October"
msgstr "Oktober"

msgctxt "date"
msgid "long"
msgstr "d. MMMM y"
```

With these two entries in `de.po`, `{{date .Start}}` writes "25. Oktober 2026" on a German page. An entry replaces the built-in name or pattern in every language, English and Dutch included: a `month` entry for `October` in `nl.po` replaces "oktober". A name or pattern without a translation, or with an empty one, falls back to the built-in one, or to English for a language without built-in names; the same msgid without its context does not count. `en.po` and `nl.po` therefore need none of these entries, and other languages need all of them. An entry is found as other texts are, so a `date` pattern in `en.po` also replaces the American one of `en-US` unless `en_US.po` has its own. `dateRange` keeps its built-in shorter form, such as "25–27 Oct 2026", only for a style whose pattern is not replaced.

`task translations` adds all of them to the POT file, so they need no template, with `core:calendar` (`frontend.CalendarReference`) as their reference; `frontend.CalendarTexts()` lists them, and `frontend.BuiltInCalendar(locale)` reports whether a language has built-in ones. Msgids such as `date:long`, without a context, are no longer read: move their translations to the `date` context.

In Go code, such as a module's e-mail, `app.Frontend.Format(ctx, locale)` writes dates with the same names and patterns. It keeps them per language in a `frontend.Calendars`, which `SetTranslations` clears. A `frontend.Format` of your own with a `Lookup` looks them up for every date, unless its `Calendars` field points to a `frontend.Calendars` it can keep them in.

[Template functions](template-functions.md) describes every style and pattern.

## The admin language

The admin has one language for everyone, set in `Config.AdminLanguage`:

```go
app := webthings.New(webthings.Config{
    AdminLanguage: "nl",
})
```

English (the default) and Dutch are built in: the admin itself and each section, such as pages, media, the site settings and every module, bring their own catalogue. The messages of the admin's forms, such as "This field is required.", come from go-form's catalogue of the admin language; go-form brings English and Dutch. Texts without a translation are shown in English. The admin's pages name their language in `<html lang>` as a BCP 47 tag, such as `pt-BR` for `AdminLanguage` `pt_BR`.

To change admin texts, or to add a language, put a catalogue in `<StateDir>/translations`, such as `var/translations/nl.po`. The admin reads that folder at start when it exists, and never writes to it. A catalogue serves its language whatever the spelling: `nl.po`, `nl_NL.po` and `nl-NL.po` all serve `AdminLanguage` `nl`, and `de_DE.po` serves `de`. Its translations win over the built-in ones, which fill in the rest, so it can change any admin text. An entry left empty counts as missing and keeps the built-in translation, so updating the file from a POT file in Poedit is safe. Restart the site after changing the file.

The built-in catalogues are in go-webthings: the admin's own in `admin/translations/`, with its template `translations.pot`, and those of the sections in the `translations/` folder of their package, such as `modules/comments/translations/`. For a new language, start from them in [Poedit](https://poedit.net/). The admin records no texts while it serves, so what editors type, such as titles and file names, never ends up in a catalogue.

Module titles and navigation groups from `main.go`, such as "News" or `.Group("Website")`, are texts like any other: they are shown translated when a catalogue has them, and as written otherwise. The group names Content, Website, Visitors, Communication, Shop and Agenda are built in; add others, and module titles, to a catalogue in `<StateDir>/translations`.

Dates, times and amounts of money in the admin follow the admin language, with times in the site's timezone.

The admin language is independent of the content languages: a Dutch admin edits English content in the English tab. Modules write e-mail for staff, such as a notification about a new form entry, in the admin language (`Env.AdminLanguage`).
