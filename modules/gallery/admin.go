package gallery

import (
	"errors"
	"net/http"
	"net/url"
	"slices"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/media"
)

// order lets editors put the images of an album in order, one place up or
// down at a time like the editor's overview: the album form only chooses
// which images an album has, and adds new ones at the end.
type order struct {
	service *Service
	admin   *admin.Admin
	editor  editor.Editor[AlbumFields, AlbumText, albumInput]
	routes  *admin.Routes
	// section is the section's title, as a translation key.
	section string
}

// OrderView is the data of templates/admin/order.gohtml: the albums whose
// images can be put in order.
type OrderView struct {
	BackURL string
	// BackLabel names the albums overview BackURL leads to, as a
	// translation key.
	BackLabel string
	Albums    []OrderAlbum
}

// OrderAlbum is one album on the order screen.
type OrderAlbum struct {
	Title string
	// URL opens the images of the album.
	URL    string
	Images int
}

// ImagesView is the data of templates/admin/images.gohtml: the images of one
// album in order.
type ImagesView struct {
	Title   string
	BackURL string
	EditURL string
	Images  []ImageRow
}

// ImageRow is one image of an album.
type ImageRow struct {
	Name string
	// URL shows the image; it is empty when the file is no longer in the
	// media library.
	URL string
	// Moves are the image's up and down arrows; nil without the right to
	// edit albums.
	Moves []admin.Move
}

// register adds the order screens to the album editor's routes, whose own
// names, such as "up", are taken.
func (o order) register() {
	o.routes.Get("/images", admin.View, o.albums).As("order")
	o.routes.Get("/{id}/images", admin.View, o.images).As("images")
	o.routes.Post("/{id}/images/{image}/up", admin.Edit, o.move(-1)).As("image-up")
	o.routes.Post("/{id}/images/{image}/down", admin.Edit, o.move(1)).As("image-down")
}

// albums lists the albums, each linking to its images.
func (o order) albums(w http.ResponseWriter, r *http.Request) {
	locale, _, err := o.editor.Locale(r)
	if err != nil {
		o.admin.Fail(w, r, err)

		return
	}

	items, err := o.service.Store().List(r.Context())
	if err != nil {
		o.admin.Fail(w, r, err)

		return
	}

	query := url.Values{"locale": {locale}}
	view := OrderView{BackURL: o.routes.URL("index", query), BackLabel: o.section}

	search := admin.SearchOf(r)

	for _, item := range items {
		title := o.title(r, item, locale)
		if !search.Matches(title) {
			continue
		}

		view.Albums = append(view.Albums, OrderAlbum{
			Title:  title,
			URL:    o.routes.URL("images", "id", item.ID, query),
			Images: len(item.Fields.Images),
		})
	}

	o.admin.Render(w, r, admin.Page{
		Title:    "Image order",
		Template: "templates/admin/order.gohtml",
		Files:    files,
		Data:     view,
	})
}

// images lists the images of one album with buttons to move them.
func (o order) images(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	locale, _, err := o.editor.Locale(r)
	if err != nil {
		o.admin.Fail(w, r, err)

		return
	}

	item, err := o.service.Store().Load(ctx, r.PathValue("id"), "")
	if errors.Is(err, content.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		o.admin.Fail(w, r, err)

		return
	}

	query := url.Values{"locale": {locale}}
	canMove := o.admin.Can(r, admin.Permission{Section: o.service.key, Action: admin.Edit})
	ids := item.Fields.Images

	view := ImagesView{
		Title:   o.title(r, item, locale),
		BackURL: o.routes.URL("order", query),
		EditURL: o.routes.URL("edit", "id", item.ID, query),
	}

	for i, id := range ids {
		row := ImageRow{Name: id}

		asset, err := o.service.media.Get(ctx, id)
		if err != nil && !errors.Is(err, media.ErrNotFound) {
			o.admin.Fail(w, r, err)

			return
		}

		if err == nil {
			row.Name = asset.Filename
			row.URL = o.service.media.URL(ctx, id)

			if title := asset.Text(locale).Title; title != "" {
				row.Name = title + " (" + asset.Filename + ")"
			}
		}

		if canMove {
			var upURL, downURL string

			if i > 0 {
				upURL = o.routes.URL("image-up", "id", item.ID, "image", id, query)
			}

			if i < len(ids)-1 {
				downURL = o.routes.URL("image-down", "id", item.ID, "image", id, query)
			}

			if len(ids) > 1 {
				row.Moves = admin.Moves(upURL, downURL)
			}
		}

		view.Images = append(view.Images, row)
	}

	o.admin.Render(w, r, admin.Page{
		Title:    "Image order",
		Template: "templates/admin/images.gohtml",
		Files:    files,
		Data:     view,
	})
}

// move returns a handler that moves an image of an album one place up (-1)
// or down (+1).
func (o order) move(step int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		locale, _, err := o.editor.Locale(r)
		if err != nil {
			o.admin.Fail(w, r, err)

			return
		}

		store := o.service.Store()

		item, err := store.Load(ctx, r.PathValue("id"), "")
		if errors.Is(err, content.ErrNotFound) {
			http.NotFound(w, r)

			return
		}

		if err != nil {
			o.admin.Fail(w, r, err)

			return
		}

		fields := item.Fields
		fields.Images = moved(fields.Images, r.PathValue("image"), step)

		_, err = store.Save(ctx, content.Draft[AlbumFields, AlbumText]{
			ID:           item.ID,
			Fields:       fields,
			Translations: item.Translations,
		})
		if err != nil {
			o.admin.Fail(w, r, err)

			return
		}

		o.admin.Redirect(w, r, o.routes.URL("images", "id", item.ID, url.Values{"locale": {locale}}))
	}
}

// title names an album in the admin: its title in locale, or in its first
// language when it is not translated yet.
func (o order) title(r *http.Request, item AlbumItem, locale string) string {
	text, ok := item.Text(locale)
	if !ok && len(item.Translations) > 0 {
		text = item.Translations[0].Text
	}

	if text.Title == "" {
		return o.admin.T(r, "Untitled")
	}

	return text.Title
}

// moved returns ids with id one place earlier (step -1) or later (step 1).
// The order stays the same when id is not in ids or already at that end.
func moved(ids content.IDs, id string, step int) content.IDs {
	from := slices.Index(ids, id)
	to := from + step

	if from < 0 || to < 0 || to >= len(ids) {
		return ids
	}

	out := slices.Clone(ids)
	out[from], out[to] = out[to], out[from]

	return out
}
