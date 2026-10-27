package media

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
)

// Pages show images as variants, not as the uploaded originals, which can be
// many megabytes: Image gives the copy a page shows by default, the srcset
// of smaller copies for small and dense screens, and the original for
// downloads.

// ImageWidth is the largest width pages show an image in by default, as
// module views and content do.
const ImageWidth = 1600

// LargeImageWidth is the width of an image seen on its own, such as in a
// gallery's lightbox.
const LargeImageWidth = 2400

// ImageWidths are the widths an image's srcset offers below its largest
// width (see Service.Image).
var ImageWidths = []int{480, 800, 1200, 1600}

// MailImageWidth is the width of images in e-mail: twice the 600 pixels
// most mail layouts are wide, so they stay sharp on dense screens.
const MailImageWidth = 1200

// Besides the variant formats (webp, jpg and png), ImageAs takes formats
// that depend on the image.
const (
	// OriginalFormat keeps the format of the upload: a PNG stays a lossless
	// PNG and a JPEG a JPEG, for images whose sharp edges lossy WebP would
	// blur, such as a logo.
	OriginalFormat = "original"
	// MailFormat is a format every e-mail program shows, which WebP is not:
	// JPEG, or PNG for a PNG with transparency, which JPEG would lose.
	MailFormat = "mail"
)

// Image is an image as a page shows it.
type Image struct {
	// URL is a copy at most the asked width wide, in DefaultVariantFormat
	// unless ImageAs asked for another, keeping the image's proportions and
	// never larger than the original. Images a variant would change, such
	// as a GIF that may be animated, and files outside the library keep
	// their own address, as does a PNG asked for as PNG that needs no
	// resizing: a copy would only be larger.
	URL string
	// Srcset lists copies of the image up to the width of URL, for a srcset
	// attribute; empty when URL is the original.
	Srcset string
	// OriginalURL is the uploaded file, for downloads.
	OriginalURL string
	// Width and Height are the size of URL in pixels, for an img's width and
	// height attributes; 0 when unknown.
	Width  int
	Height int
}

// Image returns how a page shows an image at most width pixels wide (for
// example ImageWidth), in DefaultVariantFormat. ref is an asset ID or the
// URL of an asset, as for VariantURL; anything else, such as a theme's own
// SVG logo, is shown as it is. Unknown assets give an empty Image. It only
// builds addresses: variants are made when they are first requested, or
// before by Prepare.
func (s *Service) Image(ctx context.Context, ref string, width int) Image {
	return s.ImageAs(ctx, ref, width, DefaultVariantFormat)
}

// ImageAs is Image in another format: a variant format (webp, jpg or png),
// OriginalFormat or MailFormat. The srcset has the same format.
func (s *Service) ImageAs(ctx context.Context, ref string, width int, format string) Image {
	id, ok := assetID(ref)
	if !ok {
		return Image{URL: ref, OriginalURL: ref}
	}

	info, err := s.info(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			slog.WarnContext(ctx, "media image", "asset", id, "error", err)
		}

		return Image{}
	}

	// The Image is made once for each version of the asset. One for
	// e-mail is not kept: whether it stays a PNG may depend on a file that
	// could not be read this time (see transparent).
	memo := imageMemo{width: width, format: format}
	if image, ok := info.urls.images.Load(memo); ok {
		return image.(Image)
	}

	image, complete := s.image(ctx, id, info, width, format)
	if complete && format != MailFormat {
		info.urls.images.Store(memo, image)
	}

	return image
}

// image is ImageAs for the asset id with info. complete is false when an
// address could not be made, so the Image is not kept.
func (s *Service) image(ctx context.Context, id string, info assetInfo, width int, format string) (image Image, complete bool) {
	original := s.URL(ctx, id)
	image = Image{
		URL:         original,
		OriginalURL: original,
		Width:       info.width,
		Height:      info.height,
	}

	if !info.resizable() || width <= 0 {
		return image, original != ""
	}

	// The original's width keeps one variant to one address: a box wider
	// than the image gives the same copy.
	if info.width > 0 {
		width = min(width, info.width)
	}

	format = s.imageFormat(ctx, id, info, format)

	// A PNG is lossless: a copy of the same size would only be larger.
	if format == "png" && info.mimeType == "image/png" && width == info.width {
		return image, original != ""
	}

	variant := Variant{Width: min(width, MaxVariantSize), Format: format}

	link, err := s.VariantURL(ctx, id, variant)
	if err != nil {
		slog.WarnContext(ctx, "media image", "asset", id, "error", err)

		return image, false
	}

	image.URL = link

	if info.width > 0 && info.height > 0 {
		size, _ := variant.Fit(info.width, info.height)
		image.Width = size.X
		image.Height = size.Y
	}

	image.Srcset, err = s.Srcset(ctx, id, format, srcsetWidths(variant.Width)...)
	if err != nil {
		image.Srcset = ""

		return image, false
	}

	return image, original != ""
}

// srcsetWidths are ImageWidths below width, and width itself.
func srcsetWidths(width int) []int {
	var widths []int

	for _, candidate := range ImageWidths {
		if candidate < width {
			widths = append(widths, candidate)
		}
	}

	return append(widths, width)
}

// imageFormat is the variant format of a resizable image asked for in
// format (see ImageAs).
func (s *Service) imageFormat(ctx context.Context, id string, info assetInfo, format string) string {
	png := info.mimeType == "image/png"

	switch {
	case format == OriginalFormat && png:
		return "png"
	case format == OriginalFormat:
		return "jpg"
	case format == MailFormat && png && s.transparent(ctx, id, info):
		return "png"
	case format == MailFormat:
		return "jpg"
	}

	return format
}

// transparent reports whether a PNG asset can have transparent pixels,
// from the start of its file. Files never change, so the answer is kept.
// A file that cannot be read counts as transparent, which keeps it PNG.
func (s *Service) transparent(ctx context.Context, id string, info assetInfo) bool {
	if known, ok := s.transparency.Load(id); ok {
		return known.(bool)
	}

	file, err := s.storage.Open(ctx, info.key)
	if err != nil {
		slog.WarnContext(ctx, "media transparency", "asset", id, "error", err)

		return true
	}

	transparent, err := pngTransparent(file)
	_ = file.Close()

	if err != nil {
		slog.WarnContext(ctx, "media transparency", "asset", id, "error", err)

		return true
	}

	if s.transparencies.Add(1) > maxCached {
		s.transparency.Clear()
		s.transparencies.Store(1)
	}

	s.transparency.Store(id, transparent)

	return transparent
}

// pngSignature starts every PNG file.
const pngSignature = "\x89PNG\r\n\x1a\n"

// pngTransparent reports whether a PNG can have transparent pixels: it has
// an alpha channel (colour types 4 and 6), or a tRNS chunk before its image
// data makes palette entries or one grey or colour transparent. It reads
// only the chunks before the image data.
func pngTransparent(reader io.Reader) (bool, error) {
	buffered := bufio.NewReader(reader)

	// The signature, then the IHDR chunk's length, type and data, whose
	// tenth byte is the colour type.
	header := make([]byte, 8+8+13)
	if _, err := io.ReadFull(buffered, header); err != nil {
		return false, err
	}

	if string(header[:8]) != pngSignature || string(header[12:16]) != "IHDR" {
		return false, ErrInvalid
	}

	colourType := header[16+9]
	if colourType == 4 || colourType == 6 {
		return true, nil
	}

	// The IHDR chunk's checksum.
	if _, err := buffered.Discard(4); err != nil {
		return false, err
	}

	chunk := make([]byte, 8)

	for {
		if _, err := io.ReadFull(buffered, chunk); err != nil {
			return false, err
		}

		switch string(chunk[4:]) {
		case "tRNS":
			return true, nil
		case "IDAT", "IEND":
			return false, nil
		}

		// The chunk's data and checksum.
		length := int64(binary.BigEndian.Uint32(chunk[:4]))
		if _, err := io.CopyN(io.Discard, buffered, length+4); err != nil {
			return false, err
		}
	}
}
