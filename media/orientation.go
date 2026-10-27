package media

import (
	"bytes"
	"encoding/binary"
	"image"
)

// Cameras store photos as the sensor saw them and record in EXIF how to turn
// them upright. Browsers apply that to originals, but a resized copy carries no
// EXIF, so variants are turned before encoding.

// jpegOrientation returns the EXIF orientation (1 to 8) of JPEG data; 1, the
// upright default, for other images or when there is none.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}

	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}

		marker := data[i+1]

		// Image data starts at SOS; metadata comes before it.
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}

		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}

		segment := data[i+4 : i+2+size]
		if marker == 0xE1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}

		i += 2 + size
	}

	return 1
}

// tiffOrientation reads the orientation tag (0x0112) from the first image
// directory of EXIF's TIFF structure.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}

	var order binary.ByteOrder

	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}

	offset := int(order.Uint32(tiff[4:]))
	if offset < 8 || offset+2 > len(tiff) {
		return 1
	}

	count := int(order.Uint16(tiff[offset:]))

	for n := range count {
		entry := offset + 2 + n*12
		if entry+12 > len(tiff) {
			return 1
		}

		if order.Uint16(tiff[entry:]) != 0x0112 {
			continue
		}

		value := int(order.Uint16(tiff[entry+8:]))
		if value < 1 || value > 8 {
			return 1
		}

		return value
	}

	return 1
}

// orient turns an image upright for an EXIF orientation: 2 mirrors, 3 turns
// half, 4 flips, 5 transposes, 6 turns a quarter clockwise, 7 transverses and
// 8 turns a quarter anticlockwise.
func orient(picture *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return picture
	}

	bounds := picture.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	size := image.Pt(width, height)
	if orientation >= 5 {
		size = image.Pt(height, width)
	}

	upright := image.NewRGBA(image.Rectangle{Max: size})

	for y := range height {
		for x := range width {
			var target image.Point

			switch orientation {
			case 2:
				target = image.Pt(width-1-x, y)
			case 3:
				target = image.Pt(width-1-x, height-1-y)
			case 4:
				target = image.Pt(x, height-1-y)
			case 5:
				target = image.Pt(y, x)
			case 6:
				target = image.Pt(height-1-y, x)
			case 7:
				target = image.Pt(height-1-y, width-1-x)
			default:
				target = image.Pt(y, width-1-x)
			}

			from := picture.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
			to := upright.PixOffset(target.X, target.Y)
			copy(upright.Pix[to:to+4], picture.Pix[from:from+4])
		}
	}

	return upright
}
