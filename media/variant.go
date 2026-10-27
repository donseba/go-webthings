package media

import (
	"bytes"
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
)

// Images are shown in many sizes; a variant is a resized copy of an image
// asset, made on its first request (or before, for the copies pages show
// of a new upload, by Prepare) and then kept in the Storage next to the
// original. Variant URLs are signed with the site's key, so only sizes the
// site itself asked for (through VariantURL, Image, the {{image}} and
// {{srcset}} template functions) can be made: nobody can fill the storage by trying
// sizes. They never change, so browsers and proxies may cache them forever:
//
//	/media/v/<signature>/<asset id>_<width>x<height>[c[-<x>-<y>]].<format>
//
// A width or height of 0 leaves that side free; "c" crops to the exact size,
// around the image's focal point (see FocalPoint) in thousandths of its
// width and height, left out for the centre. A new focal point gives crops
// new URLs.

// MaxVariantSize is the largest width or height of a variant in pixels.
const MaxVariantSize = 4000

// DefaultVariantFormat is the format of variants that do not name one.
const DefaultVariantFormat = "webp"

// ErrVariant is returned for a variant specification that cannot be made.
var ErrVariant = errors.New("invalid_image_variant")

// variantMIME are the formats variants can have and their types.
var variantMIME = map[string]string{
	"webp": "image/webp",
	"jpg":  "image/jpeg",
	"png":  "image/png",
}

// Variant describes a resized copy of an image.
type Variant struct {
	// Width and Height bound the size in pixels; 0 leaves that side free.
	Width  int
	Height int
	// Crop cuts the image to exactly Width × Height instead of fitting it
	// inside. It needs both sides.
	Crop bool
	// Focus is the point a crop keeps as near its centre as it can; nil is
	// the centre. VariantURL sets the asset's focal point when it is nil.
	Focus *FocalPoint
	// Format is webp (the default), jpg or png.
	Format string
}

// ParseVariant reads a specification as templates write it: a size, then
// optionally "crop" and a format, separated by spaces:
//
//	"800x600"        fit inside 800 × 600, as WebP
//	"800"            800 wide
//	"x600"           600 high
//	"800x600 crop"   exactly 800 × 600, cut around the focal point
//	"800 jpg"        800 wide, as JPEG
func ParseVariant(spec string) (Variant, error) {
	fields := strings.Fields(strings.ToLower(spec))
	if len(fields) == 0 {
		return Variant{}, ErrVariant
	}

	variant := Variant{Format: DefaultVariantFormat}

	width, height, sized := strings.Cut(fields[0], "x")

	var err error

	if variant.Width, err = dimension(width); err != nil {
		return Variant{}, err
	}

	if sized {
		if variant.Height, err = dimension(height); err != nil {
			return Variant{}, err
		}
	}

	for _, option := range fields[1:] {
		switch {
		case option == "crop":
			variant.Crop = true
		case variantMIME[option] != "":
			variant.Format = option
		case option == "jpeg":
			variant.Format = "jpg"
		default:
			return Variant{}, fmt.Errorf("%w: unknown option %q", ErrVariant, option)
		}
	}

	return variant, variant.check()
}

// dimension parses one side; empty means free.
func dimension(text string) (int, error) {
	if text == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(text)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%w: size %q", ErrVariant, text)
	}

	return value, nil
}

func (v Variant) check() error {
	switch {
	case v.Width == 0 && v.Height == 0:
		return fmt.Errorf("%w: no size", ErrVariant)
	case v.Width > MaxVariantSize || v.Height > MaxVariantSize || v.Width < 0 || v.Height < 0:
		return fmt.Errorf("%w: at most %d pixels per side", ErrVariant, MaxVariantSize)
	case v.Crop && (v.Width == 0 || v.Height == 0):
		return fmt.Errorf("%w: crop needs a width and a height", ErrVariant)
	case variantMIME[v.Format] == "":
		return fmt.Errorf("%w: format %q", ErrVariant, v.Format)
	case v.Focus != nil && !v.Crop:
		return fmt.Errorf("%w: a focal point needs a crop", ErrVariant)
	case v.Focus != nil && !v.Focus.Valid():
		return fmt.Errorf("%w: focal point outside the image", ErrVariant)
	}

	return nil
}

// focus is the point a crop keeps in view.
func (v Variant) focus() FocalPoint {
	if v.Focus == nil {
		return Centre
	}

	return *v.Focus
}

// key is the storage key of this variant of an asset. Crops name their
// focal point unless it is the centre, so moving it changes the key.
func (v Variant) key(assetID string) string {
	var key [80]byte

	return string(v.appendKey(key[:0], assetID))
}

var variantKey = regexp.MustCompile(
	`^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})_(\d{1,4})x(\d{1,4})(c(?:-(\d{1,4})-(\d{1,4}))?)?\.([a-z]+)$`,
)

// parseKey reads a variant storage key back into the asset ID and Variant.
func parseKey(key string) (string, Variant, error) {
	match := variantKey.FindStringSubmatch(key)
	if match == nil {
		return "", Variant{}, ErrVariant
	}

	width, _ := strconv.Atoi(match[2])
	height, _ := strconv.Atoi(match[3])
	variant := Variant{Width: width, Height: height, Crop: match[4] != "", Format: match[7]}

	if match[5] != "" {
		x, _ := strconv.Atoi(match[5])
		y, _ := strconv.Atoi(match[6])
		variant.Focus = &FocalPoint{X: float64(x) / focusScale, Y: float64(y) / focusScale}
	}

	// Only the canonical spelling is accepted, so one variant has one key.
	if variant.check() != nil || variant.key(match[1]) != key {
		return "", Variant{}, ErrVariant
	}

	return match[1], variant, nil
}

// Fit returns the size of the variant of a width × height image, and the
// size of the part of the image it shows, which Resize places around the
// focal point (see FocalPoint.Window). Images are never made larger: a box
// larger than the image gives the image's own size, and a crop larger than
// the image cuts the largest part with the box's proportions.
func (v Variant) Fit(width, height int) (size, part image.Point) {
	part = image.Pt(width, height)

	if v.Crop {
		part = v.cropPart(width, height)

		if part.X <= v.Width {
			return part, part
		}

		return image.Pt(v.Width, v.Height), part
	}

	scale := 1.0

	if v.Width > 0 {
		scale = min(scale, float64(v.Width)/float64(width))
	}

	if v.Height > 0 {
		scale = min(scale, float64(v.Height)/float64(height))
	}

	size = image.Pt(
		max(1, int(float64(width)*scale+0.5)),
		max(1, int(float64(height)*scale+0.5)),
	)

	return size, part
}

// cropPart is the largest part of a width × height image with the
// proportions of the variant's box: it fills the image's height or width.
func (v Variant) cropPart(width, height int) image.Point {
	part := image.Pt(width, height)

	if width*v.Height > height*v.Width {
		part.X = max(1, roundDiv(height*v.Width, v.Height))
	} else {
		part.Y = max(1, roundDiv(width*v.Height, v.Width))
	}

	return part
}

func roundDiv(a, b int) int {
	return (a + b/2) / b
}

// SetSigningKey sets the key that signs variant URLs. Every process of a site
// must use the same key; App uses a secret kept in the site settings. Without
// one the Service uses a random key, so variant URLs change on restart.
func (s *Service) SetSigningKey(key []byte) {
	s.signer.Store(newSigner(key))
}

func (s *Service) sign(key string) string {
	return s.signer.Load().sign(key)
}

// VariantURL is the address of a variant of an image. ref is an asset ID,
// the URL of an asset (for example /media/<id>.jpg) or the URL of one of its
// variants, as module views give them (see Image). Crops keep the asset's focal point in view unless the
// variant names its own Focus. It returns ErrNotFound for unknown assets
// and ErrVariant for files that are not images. The address is that of the
// library's route named variant (see RegisterRoutes), whatever the Storage,
// because variants are made on their first request.
func (s *Service) VariantURL(ctx context.Context, ref string, variant Variant) (string, error) {
	if variant.Format == "" {
		variant.Format = DefaultVariantFormat
	}

	if err := variant.check(); err != nil {
		return "", err
	}

	id, ok := assetID(ref)
	if !ok {
		return "", ErrNotFound
	}

	info, err := s.info(ctx, id)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(info.mimeType, "image/") {
		return "", ErrVariant
	}

	// The address is made once for each version of the asset.
	memo := variant.memo(info.focus)
	if address, ok := info.urls.variants.Load(memo); ok {
		return address.(string), nil
	}

	if variant.Crop && variant.Focus == nil {
		focus := info.focus
		variant.Focus = &focus
	}

	key := variant.key(id)

	address, err := s.variantPath(s.sign(key), key)
	if err != nil {
		return "", err
	}

	info.urls.variants.Store(memo, address)

	return address, nil
}

// Srcset lists variants of an image in the given widths for a srcset
// attribute, for example "/media/v/…_400x0.webp 400w, /media/v/…_800x0.webp 800w".
// Widths beyond the image's own width are left out; the image's own width is
// added instead, so a small image is never offered as a large one.
func (s *Service) Srcset(ctx context.Context, ref, format string, widths ...int) (string, error) {
	id, ok := assetID(ref)
	if !ok {
		return "", ErrNotFound
	}

	info, err := s.info(ctx, id)
	if err != nil {
		return "", err
	}

	var candidates []string

	for _, width := range widths {
		if width <= 0 {
			continue
		}

		last := info.width > 0 && width >= info.width
		if last {
			width = info.width
		}

		link, err := s.VariantURL(ctx, id, Variant{Width: min(width, MaxVariantSize), Format: format})
		if err != nil {
			return "", err
		}

		candidates = append(candidates, link+" "+strconv.Itoa(min(width, MaxVariantSize))+"w")

		if last {
			break
		}
	}

	return strings.Join(candidates, ", "), nil
}

// assetID finds the asset ID in an ID, an asset URL or a variant URL:
// storage keys are the ID with the file extension, wherever the storage
// keeps them, and variant keys start with the ID. So the template functions
// accept what Image gives as well as the original.
func assetID(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)

	if end := strings.IndexAny(ref, "?#"); end >= 0 {
		ref = ref[:end]
	}

	name := path.Base(ref)

	// A variant key starts with the asset ID and an underscore.
	if len(name) > 36 && name[36] == '_' {
		if match := variantKey.FindStringSubmatch(name); match != nil {
			return match[1], true
		}
	}

	name = strings.TrimSuffix(name, path.Ext(name))

	if !isUUID(name) {
		return "", false
	}

	return strings.ToLower(name), true
}

// variant returns the bytes of a variant by its signed key, making it on the
// first request.
func (s *Service) variant(ctx context.Context, signature, key string) ([]byte, string, error) {
	id, variant, err := parseKey(key)
	if err != nil || !hmac.Equal([]byte(signature), []byte(s.sign(key))) {
		return nil, "", ErrNotFound
	}

	mimeType := variantMIME[variant.Format]

	// Variants of an asset in the trash are not served, though their files
	// stay until it is purged.
	if _, err := s.info(ctx, id); err != nil {
		return nil, "", err
	}

	if file, err := s.storage.Open(ctx, key); err == nil {
		defer func() { _ = file.Close() }()

		data, err := io.ReadAll(file)

		return data, mimeType, err
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}

	data, err := s.makeOnce(ctx, id, key, variant)
	if err != nil {
		return nil, "", err
	}

	return data, mimeType, nil
}

// makeOnce makes a variant once for concurrent requests for it, and to the
// end when a request is cancelled, so the next one finds it stored.
func (s *Service) makeOnce(ctx context.Context, id, key string, variant Variant) ([]byte, error) {
	result, err, _ := s.flights.Do(key, func() (any, error) {
		return s.makeVariant(context.WithoutCancel(ctx), id, key, variant)
	})
	if err != nil {
		return nil, err
	}

	return result.([]byte), nil
}

// makeVariant resizes an asset, stores the result and records it, so it is
// deleted with the asset.
func (s *Service) makeVariant(ctx context.Context, id, key string, variant Variant) ([]byte, error) {
	// Resizing large images takes memory and time; a few at a time is enough.
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	info, err := s.info(ctx, id)
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(info.mimeType, "image/") {
		return nil, ErrNotFound
	}

	file, err := s.storage.Open(ctx, info.key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	original, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	_ = file.Close()

	if err != nil {
		return nil, err
	}

	data, err := Resize(original, variant)
	if err != nil {
		return nil, err
	}

	err = s.storage.Put(ctx, key, bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}

	_, err = s.db.Exec(ctx,
		`INSERT INTO wt_media_variant(storage_key,asset_id,created_at) VALUES(?,?,?)`,
		key, id, time.Now().UTC(),
	)

	switch {
	case db.IsForeignKey(err):
		// The asset was deleted meanwhile; its variant goes too.
		_ = s.storage.Delete(ctx, key)

		return nil, ErrNotFound
	case err != nil && !db.IsUnique(err):
		slog.WarnContext(ctx, "record media variant", "key", key, "error", err)
	}

	return data, nil
}

// Resize makes a variant of an encoded JPEG, PNG or GIF image: it is turned
// upright following its EXIF orientation, cropped around the variant's
// Focus, scaled with the Catmull-Rom filter and encoded in the variant's
// format. An animated GIF gives its first frame.
func Resize(original []byte, variant Variant) ([]byte, error) {
	if variant.Format == "" {
		variant.Format = DefaultVariantFormat
	}

	if err := variant.check(); err != nil {
		return nil, err
	}

	source, _, err := image.Decode(bytes.NewReader(original))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	orientation := jpegOrientation(original)
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// Orientations 5 to 8 turn the image a quarter: fit the upright size, then
	// scale the stored image to the turned result.
	turned := orientation >= 5
	if turned {
		width, height = height, width
	}

	size, part := variant.Fit(width, height)
	if turned {
		size = image.Pt(size.Y, size.X)
		part = image.Pt(part.Y, part.X)
	}

	// The focal point is placed on the upright image; the crop is cut from
	// the image as stored.
	scaled := image.NewRGBA(image.Rectangle{Max: size})
	region := variant.focus().stored(orientation).Window(bounds, part)

	if size == part {
		draw.Copy(scaled, image.Point{}, source, region, draw.Src, nil)
	} else {
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), source, region, draw.Src, nil)
	}

	return encode(orient(scaled, orientation), variant.Format)
}

func encode(picture *image.RGBA, format string) ([]byte, error) {
	var buffer bytes.Buffer

	var err error

	switch format {
	case "jpg":
		// JPEG has no transparency: transparent parts become white.
		flat := image.NewRGBA(picture.Bounds())
		draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), picture, picture.Bounds().Min, draw.Over)

		err = jpeg.Encode(&buffer, flat, &jpeg.Options{Quality: 85})
	case "png":
		err = png.Encode(&buffer, picture)
	default:
		err = webp.Encode(&buffer, straight(picture), webp.Options{Quality: 80, Method: 4})
	}

	return buffer.Bytes(), err
}

// straight converts premultiplied pixels to the straight alpha WebP expects;
// opaque images are the same in both.
func straight(picture *image.RGBA) image.Image {
	if picture.Opaque() {
		return &image.NRGBA{Pix: picture.Pix, Stride: picture.Stride, Rect: picture.Rect}
	}

	converted := image.NewNRGBA(picture.Bounds())
	draw.Draw(converted, converted.Bounds(), picture, picture.Bounds().Min, draw.Src)

	return converted
}

// deleteVariants removes the files of an asset's variants.
func (s *Service) deleteVariants(ctx context.Context, keys []string) {
	for _, key := range keys {
		if err := s.storage.Delete(ctx, key); err != nil {
			slog.WarnContext(ctx, "delete media variant", "key", key, "error", err)
		}
	}
}
