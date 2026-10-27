package mediaadmin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/media"
)

// The admin's Markdown editor (admin/assets/editor) lists the images of the
// library to insert them as ![alt](media:<id>), and shows images that the
// Markdown references through their URL. Both read this endpoint:
//
//	GET <admin>/media/picker?page=1&q=beach   images, newest first, searched like the library
//	GET <admin>/media/picker?id=<id>&id=<id>  the given images, unknown ones left out
//
// It needs the media section's View permission, like the library itself.

// pickerPageSize is how many images one page of the picker lists.
const pickerPageSize = 24

// pickerMaxIDs bounds how many images one request may look up by ID.
const pickerMaxIDs = 100

// Thumbnails fill the picker's grid; previews show images in the editor.
var (
	pickerThumb   = media.Variant{Width: 320, Height: 320}
	pickerPreview = media.Variant{Width: 800}
)

// PickerItem is an image as the editor's picker lists it.
type PickerItem struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	// URL is the original file.
	URL string `json:"url"`
	// Thumb is a small copy for the picker's grid.
	Thumb string `json:"thumb"`
	// Preview is the copy the editor shows in the text.
	Preview string `json:"preview"`
}

// PickerPage is a page of the picker.
type PickerPage struct {
	Items []PickerItem `json:"items"`
	// Next tells whether a next page exists.
	Next bool `json:"next"`
}

func (s section) picker(w http.ResponseWriter, r *http.Request) {
	assets, err := s.library.ListWithTranslations(r.Context())
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	query := r.URL.Query()
	images := imagesOnly(assets)

	var page PickerPage

	if ids := query["id"]; len(ids) > 0 {
		page.Items = s.pickerItems(r, byID(images, ids))
	} else {
		// The search matches like the library's list, ignoring case and
		// accents.
		matching := admin.Filter(admin.NewSearch(query.Get("q")), images, searchFields)
		number, _ := strconv.Atoi(query.Get("page"))
		shown, next := admin.Paginate(matching, number, pickerPageSize)

		page.Items = s.pickerItems(r, shown)
		page.Next = next
	}

	if page.Items == nil {
		page.Items = []PickerItem{}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if err := json.NewEncoder(w).Encode(page); err != nil {
		s.admin.Fail(w, r, err)
	}
}

func imagesOnly(assets []media.Asset) []media.Asset {
	var images []media.Asset

	for _, asset := range assets {
		if asset.IsImage() {
			images = append(images, asset)
		}
	}

	return images
}

// byID keeps the images with the given IDs, in the order of ids.
func byID(images []media.Asset, ids []string) []media.Asset {
	index := make(map[string]media.Asset, len(images))

	for _, image := range images {
		index[image.ID] = image
	}

	var found []media.Asset

	seen := map[string]bool{}

	for _, id := range ids[:min(len(ids), pickerMaxIDs)] {
		id = strings.ToLower(strings.TrimSpace(id))

		image, ok := index[id]
		if !ok || seen[id] {
			continue
		}

		seen[id] = true
		found = append(found, image)
	}

	return found
}

func (s section) pickerItems(r *http.Request, images []media.Asset) []PickerItem {
	items := make([]PickerItem, 0, len(images))

	for _, image := range images {
		original := s.library.URL(r.Context(), image.ID)
		item := PickerItem{
			ID:       image.ID,
			Filename: image.Filename,
			URL:      original,
			Thumb:    original,
			Preview:  original,
		}

		// A GIF may be animated, which a resized copy would lose.
		if s.library.Resizable(r.Context(), image.ID) {
			if thumb, err := s.library.VariantURL(r.Context(), image.ID, pickerThumb); err == nil {
				item.Thumb = thumb
			}

			if preview, err := s.library.VariantURL(r.Context(), image.ID, pickerPreview); err == nil {
				item.Preview = preview
			}
		}

		items = append(items, item)
	}

	return items
}
