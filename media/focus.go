package media

import (
	"context"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/donseba/go-webthings/db"
)

// FocalPoint is the part of an image that matters most, such as a face:
// every crop keeps it as near its centre as the image allows, so a point
// near a corner stays near that corner in every crop. X and Y are fractions
// of the upright image's width and height from the top left; the centre
// (0.5, 0.5) is the default.
type FocalPoint struct {
	X float64
	Y float64
}

// Centre is the focal point of images that have none of their own.
var Centre = FocalPoint{X: 0.5, Y: 0.5}

// focusScale is the precision of focal points: they are kept and named in
// variant keys in thousandths, finer than any editor can click.
const focusScale = 1000

// Valid reports whether both fractions lie between 0 and 1.
func (p FocalPoint) Valid() bool {
	inside := func(value float64) bool {
		return value >= 0 && value <= 1
	}

	return inside(p.X) && inside(p.Y)
}

// rounded is the point in thousandths, the precision it is stored with.
func (p FocalPoint) rounded() FocalPoint {
	return FocalPoint{
		X: float64(thousandths(p.X)) / focusScale,
		Y: float64(thousandths(p.Y)) / focusScale,
	}
}

func thousandths(value float64) int {
	return int(math.Round(value * focusScale))
}

// isCentre reports whether the point is the centre at the stored precision.
func (p FocalPoint) isCentre() bool {
	return p.rounded() == Centre
}

// Window places a rectangle of size within bounds so that the focal point
// is at its centre, moved as little as needed to stay inside bounds. A
// window as large as bounds on a side covers that side.
func (p FocalPoint) Window(bounds image.Rectangle, size image.Point) image.Rectangle {
	x := offset(bounds.Dx(), size.X, p.X)
	y := offset(bounds.Dy(), size.Y, p.Y)
	minimum := bounds.Min.Add(image.Pt(x, y))

	return image.Rectangle{Min: minimum, Max: minimum.Add(size)}
}

// offset is where a window of size starts on a side of length so that the
// fraction focus of the side is in its middle, kept inside the side.
func offset(length, size int, focus float64) int {
	if size >= length {
		return 0
	}

	focus = min(max(focus, 0), 1)
	start := int(math.Round(focus*float64(length) - float64(size)/2))

	return min(max(start, 0), length-size)
}

// stored is the point in the image as stored, for an EXIF orientation (see
// orient): editors place it on the upright image browsers show.
func (p FocalPoint) stored(orientation int) FocalPoint {
	switch orientation {
	case 2:
		return FocalPoint{X: 1 - p.X, Y: p.Y}
	case 3:
		return FocalPoint{X: 1 - p.X, Y: 1 - p.Y}
	case 4:
		return FocalPoint{X: p.X, Y: 1 - p.Y}
	case 5:
		return FocalPoint{X: p.Y, Y: p.X}
	case 6:
		return FocalPoint{X: p.Y, Y: 1 - p.X}
	case 7:
		return FocalPoint{X: 1 - p.Y, Y: 1 - p.X}
	case 8:
		return FocalPoint{X: 1 - p.Y, Y: p.X}
	default:
		return p
	}
}

// ObjectPosition is a CSS object-position value for a width × height image
// that the browser crops with object-fit: cover. With the proportions of
// the box (box.X wide, box.Y high) it shows exactly what a crop variant of
// that size shows. Without them (a zero box) it puts the focal point as far
// into the box as it is into the image: always visible, and in the corner
// when it is in a corner, but not centred.
func (p FocalPoint) ObjectPosition(width, height int, box image.Point) string {
	x, y := p.position(width, height, box)

	return percent(x) + " " + percent(y)
}

// PositionClass is the class of focal-points.css, the stylesheet core
// serves to themes, that sets about ObjectPosition: "fp-<x>-<y>" with both
// percentages rounded to a multiple of 5, such as "fp-35-60". Unlike a
// style attribute, a class works under a Content-Security-Policy without
// 'unsafe-inline'.
func (p FocalPoint) PositionClass(width, height int, box image.Point) string {
	x, y := p.position(width, height, box)

	return "fp-" + strconv.Itoa(bucket(x)) + "-" + strconv.Itoa(bucket(y))
}

// position is ObjectPosition as fractions of what sticks out of the box.
func (p FocalPoint) position(width, height int, box image.Point) (x, y float64) {
	if width < 1 || height < 1 || box.X < 1 || box.Y < 1 {
		return p.X, p.Y
	}

	part := Variant{Width: box.X, Height: box.Y, Crop: true}.cropPart(width, height)
	window := p.Window(image.Rect(0, 0, width, height), part)

	// The browser moves the image by the given fraction of what sticks out.
	fraction := func(start, length, size int) float64 {
		if size >= length {
			return 0.5
		}

		return float64(start) / float64(length-size)
	}

	return fraction(window.Min.X, width, part.X), fraction(window.Min.Y, height, part.Y)
}

// percent writes a fraction as a percentage with at most one decimal.
func percent(fraction float64) string {
	value := math.Round(fraction*1000) / 10

	return strconv.FormatFloat(value, 'f', -1, 64) + "%"
}

// bucket is a fraction as a percentage rounded to a multiple of 5, between
// 0 and 100: the steps of the classes of PositionClass.
func bucket(fraction float64) int {
	fraction = min(max(fraction, 0), 1)

	return int(math.Round(fraction*20)) * 5
}

// parseBox reads the proportions of a box as "4:3", "800x600" or a variant
// specification such as "800x600 crop"; a zero box when it names no width
// and height.
func parseBox(text string) image.Point {
	text = strings.Replace(strings.TrimSpace(text), ":", "x", 1)
	if text == "" {
		return image.Point{}
	}

	variant, err := ParseVariant(text)
	if err != nil || variant.Width == 0 || variant.Height == 0 {
		return image.Point{}
	}

	return image.Pt(variant.Width, variant.Height)
}

// SetFocalPoint sets the focal point of an image (ErrInvalid for other
// files or a point outside the image). Crop variants around the previous
// point are deleted: their URLs name the focal point, so pages get new
// URLs at once and long-cached copies of the old ones are not used.
// Variants that are not cropped do not change.
func (s *Service) SetFocalPoint(ctx context.Context, id string, point FocalPoint) error {
	if !point.Valid() {
		return ErrInvalid
	}

	point = point.rounded()

	var stale []string

	err := s.db.Tx(ctx, func(ctx context.Context) error {
		var mimeType string

		err := s.db.Get(ctx, &mimeType, `SELECT mime_type FROM wt_media_asset WHERE id=? AND deleted_at IS NULL`, id)
		if err != nil {
			return notFound(err)
		}

		if !strings.HasPrefix(mimeType, "image/") {
			return ErrInvalid
		}

		at, by := db.Stamp(ctx)

		_, err = s.db.Exec(ctx,
			`UPDATE wt_media_asset SET focal_x=?, focal_y=?, updated_at=?, updated_by=? WHERE id=?`,
			point.X, point.Y, at, by, id,
		)
		if err != nil {
			return err
		}

		stale, err = s.staleCrops(ctx, id, point)
		if err != nil || len(stale) == 0 {
			return err
		}

		query, args, err := s.db.In(`DELETE FROM wt_media_variant WHERE storage_key IN (?)`, stale)
		if err != nil {
			return err
		}

		_, err = s.db.Exec(ctx, query, args...)

		return err
	})
	if err != nil {
		return err
	}

	s.changed(id)
	s.deleteVariants(ctx, stale)

	return nil
}

// staleCrops are the keys of an asset's crop variants made around another
// focal point than point.
func (s *Service) staleCrops(ctx context.Context, id string, point FocalPoint) ([]string, error) {
	var keys []string

	err := s.db.Select(ctx, &keys, `SELECT storage_key FROM wt_media_variant WHERE asset_id=?`, id)
	if err != nil {
		return nil, err
	}

	var stale []string

	for _, key := range keys {
		_, variant, err := parseKey(key)
		if err != nil || !variant.Crop {
			continue
		}

		if variant.focus().rounded() != point {
			stale = append(stale, key)
		}
	}

	return stale, nil
}

// FocalPoint returns the focal point of an image; the centre for unknown
// assets. ref is an asset ID or the URL of an asset, as for VariantURL.
func (s *Service) FocalPoint(ctx context.Context, ref string) FocalPoint {
	id, ok := assetID(ref)
	if !ok {
		return Centre
	}

	info, err := s.info(ctx, id)
	if err != nil {
		return Centre
	}

	return info.focus
}

// ObjectPosition is a CSS object-position value that makes the browser's
// object-fit: cover crop of an image follow its focal point (see
// FocalPoint.ObjectPosition). box gives the proportions of the crop, as
// "4:3" or "800x600", to show exactly what the crop variant of that size
// shows; empty, the value suits boxes of any shape. Unknown assets give
// the centre. ref is an asset ID or the URL of an asset, as for VariantURL.
func (s *Service) ObjectPosition(ctx context.Context, ref, box string) string {
	id, ok := assetID(ref)
	if !ok {
		return Centre.ObjectPosition(0, 0, image.Point{})
	}

	info, err := s.info(ctx, id)
	if err != nil {
		return Centre.ObjectPosition(0, 0, image.Point{})
	}

	return info.focus.ObjectPosition(info.width, info.height, parseBox(box))
}

// PositionClass is the class of focal-points.css for ObjectPosition (see
// FocalPoint.PositionClass), such as "fp-35-60"; unknown assets give the
// centre, "fp-50-50". ref and box are as for ObjectPosition.
func (s *Service) PositionClass(ctx context.Context, ref, box string) string {
	id, ok := assetID(ref)
	if !ok {
		return Centre.PositionClass(0, 0, image.Point{})
	}

	info, err := s.info(ctx, id)
	if err != nil {
		return Centre.PositionClass(0, 0, image.Point{})
	}

	return info.focus.PositionClass(info.width, info.height, parseBox(box))
}
