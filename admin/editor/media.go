package editor

import (
	"context"

	"github.com/donseba/go-webthings/media"

	form "github.com/donseba/go-form/v2"
)

// MediaChoices is a dropdown of media files by name. Its field offers none
// with a placeholder, such as placeholder:"—". With images set, only images
// are offered.
func MediaChoices(ctx context.Context, library *media.Service, images bool, selected string) (form.SortedSelect[string], error) {
	choices := map[string]string{}

	assets, err := library.List(ctx)
	if err != nil {
		return form.SortedSelect[string]{}, err
	}

	for _, asset := range assets {
		if !images || asset.IsImage() {
			choices[asset.ID] = asset.Filename
		}
	}

	if selected != "" && choices[selected] == "" {
		choices[selected] = selected
	}

	return form.NewSortedSelect(choices).SortByLabel().Selected(selected), nil
}
