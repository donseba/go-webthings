// Package admin is the shared back office: one shell (layout, navigation,
// notices), sign-in with password reset and first-run setup, translations and
// a permission check on every route. Form posts from other websites are
// refused (http.CrossOriginProtection), so forms need no tokens. Core
// packages and modules contribute Sections.
//
// # Routes and links
//
// The admin is served at a path of the site's choosing (Options.Path,
// webthings.Config.AdminPath; /admin by default), so nothing links into it
// by path. Every page is a named route, and links are built from the names
// with go-router's reverse routing:
//
//   - The admin's own pages are "admin.dashboard", "admin.login",
//     "admin.logout", "admin.account", "admin.trash", "admin.setup",
//     "admin.forgot-password", "admin.reset-password", "admin.presence",
//     and its files "admin.asset" (with "file", such as "icons.svg") and
//     "admin.stylesheet". Templates link to the shared files with
//     go-importmap's importmap_url, which adds the file's version so
//     browsers may cache it: {{importmap_url "icons.svg"}}; a script or
//     stylesheet tag comes from importmap_script and importmap_style, which
//     add its Subresource Integrity too: {{importmap_script "tree.js"
//     "type" "module"}}.
//
//   - A section's routes are named "admin.<section ID>.<name>" (RouteName):
//     Routes.Get and Routes.Post return a Route, and Route.As gives it its
//     short name. By convention the section's own page is "index", the form
//     for a new item "new", an item's page "edit" or "show", and actions are
//     named after what they do, such as "delete" or "settings"; a POST to
//     the address of a named GET needs no name of its own. Sections made
//     with package editor name theirs (index, new, edit, delete, publish,
//     …), so screens a module adds to them choose other names.
//
//   - In Go, Routes.URL builds the address of one of the section's own
//     routes by its short name, Admin.URL (or Admin.TryURL) that of any
//     route by its full name, RouteURL that of the admin of a request's
//     context, for hooks such as Section.Recent, and
//     webthings.Env.AdminURL links into the admin from outside it, such as
//     an edit link on the site or a link in an e-mail:
//
//     routes.Get("/{id}", admin.View, show).As("show")
//     routes.URL("show", "id", id, url.Values{"locale": {locale}})
//     env.AdminURL(admin.RouteName(env.Key, "show"), "id", id)
//
//   - In templates, go-router's functions build them: route (name, then
//     key value pairs, maps and url.Values; an unknown route or parameter
//     fails the template), routePath, routePathWithParams and routeURL:
//
//     <a href="{{route "admin.news.edit" "id" .ID}}">…</a>
//     <use href="{{importmap_url "icons.svg"}}#close"/>
//
// Section.Path is data, the section's address below the admin ("/news";
// empty is "/" followed by its ID), never a link. External modules use the
// same public API and the same convention.
//
// # Styling section templates
//
// The admin ships one compiled stylesheet. Section templates may use its
// component classes and a fixed set of Tailwind utilities; any other Tailwind
// class used only by a module outside core is not in the stylesheet and has no
// effect.
//
// Components:
//
//   - Page frame: every page is <main id="content" class="admin-page"> around
//     one <div class="admin-page-body">, which gives all pages the same
//     padding, centred width and spacing. The body starts with
//     <header class="admin-page-header"> holding <h1 class="admin-page-title">
//     and a <div class="admin-page-actions"> of buttons; .admin-page-intro is
//     a muted line below it. Then come notices, tabs and content boxes: a
//     <section class="card"> per part of the page, titled with
//     <h2 class="card-title">. Tables inside the content area are styled
//     automatically and scroll inside their box on small screens. Give numeric cells .numeric
//     (right-aligned, tabular figures). .card-footer separates the actions at
//     the end of a box.
//   - Lists: declare the search box, {{/* @model Search
//     github.com/donseba/go-webthings/admin.SearchBox */}}, put
//     {{template "admin-search" Search}} first in the header's actions and
//     read the rows with ListPage, which searches and pages them the same
//     way on every screen; show {{template "admin-empty" Search}} when
//     nothing is left and {{template "admin-pager" .Pager}} below the
//     rows. Filter tabs come from FilterTabs and show with
//     {{template "admin-tabs" .Tabs}}; language tabs from LocaleTabs, with
//     {{template "locale-tabs.gohtml" .Tabs}}. Ordered lists show their
//     arrows with {{template "admin-moves" .Moves}} (see Moves). ReadCSV,
//     CSVCell and WriteCSV import and export lists as CSV.
//   - Text: .muted for secondary text, .text-danger for problems, .notice for a
//     message box.
//   - Buttons: .button (primary), .button-secondary and .button-danger, also on
//     links and summaries; all share one size, .button-sm is the compact one.
//     Buttons in table rows and in a .row-actions group are compact; forms
//     of only a button (go-form's) add no margin there or in an .action-bar.
//   - Badges: .badge for a neutral label ("Archived"), .badge-accent for a
//     count that asks for attention.
//   - Filters and tabs: a <nav class="filter-tabs"> of links, as the
//     "admin-tabs" template writes it; the current one has
//     aria-current="page". Put a .badge in a link for a count.
//   - Figures: a .stat-tiles grid of .stat-tile boxes, each with a .stat-label
//     and a .stat-value.
//   - Bars: <progress class="meter" max="100" value="40"> is a horizontal share
//     bar. A .bar-chart holds vertical .bar elements whose height is one of
//     the classes bar-0 to bar-100, in percent: <span class="bar bar-40"></span>.
//
// No admin page has style attributes or <style> elements, which a strict
// Content-Security-Policy blocks: a section styles its markup with classes
// and its own stylesheet, and its scripts may set element.style.
//
// Utilities (with sm:, md: and lg: variants where noted):
//
//   - flex, inline-flex, grid, block, inline, inline-block, hidden, flex-wrap,
//     flex-col, flex-1, grow, shrink-0, min-w-0, w-full, max-w-full
//   - items-start|center|end|baseline, justify-start|center|end|between
//   - grid-cols-1…4 and col-span-1…3|full (sm:, md:, lg:)
//   - gap, gap-x, gap-y, m, mt, mb, ml, mr, mx, my, p, pt, pb, px, py with 0…6
//     and 8; space-y-1…6
//   - text-xs|sm|base|lg|xl|2xl|3xl, text-left|center|right, font-medium|
//     semibold|bold, whitespace-pre-wrap, break-all, break-words, truncate,
//     tabular-nums, sr-only
//   - border, border-t, border-b, rounded, rounded-lg, rounded-xl
//   - max-w-xl…6xl, max-h-40|64|96, h-40, w-16|24|32, min-h-24, overflow-auto,
//     overflow-x-auto
//
// For anything else a section brings its own stylesheet: Section.Stylesheet
// names a CSS file in Section.Files, which every admin page links from
// <admin>/assets/sections/<section>.css. All sections share the page, so scope
// the rules under a class of the module, and use the theme's custom
// properties (--surface, --ink, --muted, --border, --accent, --danger, …) so
// the light, dark and retro themes keep working.
//
// # Navigation and dashboard
//
// The navigation lists the sections a user may open. The Dashboard and the
// sections without Group (Pages and Media) come first, without heading; then
// the groups, sorted by translated name, each with its sections sorted by
// translated title; GroupSystem (Accounts, Groups, Your account, the
// site's settings and Backup) comes last. Editors can collapse a group;
// the browser remembers it, and the group of the current page is always
// open.
//
// The dashboard is built from the same sections: "Needs attention" lists the
// revisions waiting for the user's review (Section.Review) and the sections
// with a Badge count, "Recently edited" the newest items of every
// Section.Recent, "Scheduled" the planned changes of every
// Section.Scheduled, and quick actions open the section's new item where the
// user may create: the route Section.NewRoute names, preferably, or else
// Section.NewPath below the admin. Sections made with package editor
// provide all of these. The
// page shows at once; its panels load after it, each on its own, asking all
// sections at the same time for a few items and waiting a few seconds at
// most, so a slow section delays only its panel and is named there. The
// navigation's badges are asked for at the same time too, and a badge that
// takes longer than a second shows no count.
//
// # Presence
//
// Every admin page of a section shows, in the header, who else is looking
// at it: the other users' initials, with their names as tooltip (never their
// e-mail addresses), to users who may view the section. The language tabs
// of an editor are one page. The browser reports the page it shows when it
// opens it and every 15 seconds, and follows the other viewers over a stream
// of server-sent events (go-partial's exp/sse) while the tab is visible;
// tabs that close or stop reporting are gone within a minute. Presence locks
// nothing. It is kept in memory (package presence), so it covers one server.
//
// # Confirmations and notices
//
// Forms built with Admin.Form whose action ends in /delete ask "Move this
// item to the trash?" before they are sent. A screen with one form, such as
// a module's settings, is a FormScreen. A refused post shows its problems at
// the fields (FieldError and Admin.Validate), or as an error toast
// (WithError) when they concern the whole request. Admin.Confirm (or the confirm
// template function) sets another question or removes it, as for deletes
// that cannot be undone; a hand-written form can set hx-confirm itself.
// Every question is asked in the layout's dialog, with the buttons Cancel
// and Confirm in the admin's language, rather than with the browser's
// window.confirm, which some embedded browsers never show.
//
// Admin.Redirect shows a notice after the redirect chosen by the last part of
// the name of the post's route: publish shows "Published.", unpublish
// "Unpublished.", restore "Revision restored as a new draft.", delete "Moved
// to the trash.", schedule "Publication scheduled." and unschedule
// "Schedule cancelled.", other posts "Changes saved.";
// Admin.RedirectNotice names the notice explicitly, as a translation key, and
// Admin.RedirectFlash carries a toast of any level whose text is translated
// already. Notices show as toasts in the corner of the screen; a handler can
// add one to the page it renders with go-partial's flash.Add (success and
// info hide themselves, warnings and errors stay until dismissed). Refused
// posts answer with Admin.BadRequest or Admin.Forbidden: an HTMX request
// gets their translated text as an error toast added out of band, any other
// request the text itself.
//
// # Trash
//
// Deleting moves items to the trash (see Trash and Section.Trash): the
// admin's trash ("admin.trash") lists them per section, restores them or
// deletes them for good. Sections made with package editor have a trash;
// modules with tables of their own implement Trash. Admin.PurgeTrash
// deletes for good, in every section, the items deleted before a time, as
// webthings.App does every hour with the days Site settings › Trash set.
package admin
