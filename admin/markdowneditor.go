package admin

import (
	"bytes"
	"encoding/json"
	"html/template"

	translator "github.com/donseba/go-translator/v2"
)

// Markdown fields, go-form textareas with the class "markdown", get a
// formatted-text editor in the browser that stores Markdown (see
// assets/editor):
//
//	Body string `form:"textarea" label:"Content" class:"markdown"`
//
// The layout loads its small loader on every page; the editor itself is
// fetched only by pages with such a field.

// markdownEditorLabels are the editor's texts, translated into the admin
// language for its buttons, dialog and messages.
var markdownEditorLabels = []string{
	"Text formatting",
	"Paragraph",
	"Heading 2",
	"Heading 3",
	"Heading 4",
	"Bold",
	"Italic",
	"Code",
	"Link",
	"Bulleted list",
	"Numbered list",
	"Quote",
	"Code block",
	"Horizontal line",
	"Image from the media library",
	"Undo",
	"Redo",
	"Show the text as Markdown",
	"Link address (leave empty to remove the link)",
	"This link address cannot be used.",
	"This text contains Markdown the editor cannot show, such as a table. Edit it as Markdown.",
	"Choose an image",
	"Close",
	"Search images",
	"Images",
	"Show more",
	"Alternative text",
	"Optional. When empty, the image's alternative text from the media library is used.",
	"Insert",
	"Cancel",
	"Loading",
	"The media library could not be loaded.",
	"No images found.",
}

// markdownEditor is the layout's script tags for the editor: its texts in
// the admin language and the loader, which carries the address of the media
// library's picker ("admin.media.picker"; empty without a media library).
func (a *Admin) markdownEditor(loc translator.Localizer) (template.HTML, error) {
	labels := make(map[string]string, len(markdownEditorLabels))

	for _, label := range markdownEditorLabels {
		labels[label] = a.tr.Tl(loc, label)
	}

	// json.Marshal escapes <, > and &, so the labels cannot end the script.
	data, err := json.Marshal(labels)
	if err != nil {
		data = []byte("{}")
	}

	var out bytes.Buffer

	out.WriteString(`<script type="application/json" id="markdown-editor-labels">`)
	out.Write(data)
	out.WriteString(`</script>`)

	picker, _ := a.TryURL(RouteName("media", "picker"))

	// The loader's tag, with its integrity, from go-importmap.
	loader, err := a.assets.importMap.Script("editor/loader.js", "type", "module", "data-media-picker", picker)
	if err != nil {
		return "", err
	}

	//nolint:gosec // Fixed markup around JSON with <, > and & escaped.
	return template.HTML(out.String()) + loader, nil
}
