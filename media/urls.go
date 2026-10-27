package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"hash"
	"strconv"
	"sync"
	"sync/atomic"
)

// Pages ask for the addresses of the same images on every request: the
// logo, the favicon, the images of the content. They are made once for
// each version of an asset (see assetURLs), without a new HMAC for each
// signature (see signer).

// assetURLs keeps the addresses made of one version of an asset: its own,
// those of its variants by variant and its Images by width and format. A
// change of the asset makes a new assetInfo, with new assetURLs.
type assetURLs struct {
	own      atomic.Pointer[string]
	variants sync.Map
	images   sync.Map
}

// variantMemo is a variant as assetURLs keeps its address: with its focal
// point as a value.
type variantMemo struct {
	width, height int
	crop          bool
	focus         FocalPoint
	format        string
}

// imageMemo is an Image as assetURLs keeps it.
type imageMemo struct {
	width  int
	format string
}

// memo is the variant as assetURLs keeps it; a crop without a Focus of
// its own keeps the asset's focus (see VariantURL).
func (v Variant) memo(focus FocalPoint) variantMemo {
	memo := variantMemo{width: v.Width, height: v.Height, crop: v.Crop, format: v.Format}

	switch {
	case v.Crop && v.Focus != nil:
		memo.focus = *v.Focus
	case v.Crop:
		memo.focus = focus
	}

	return memo
}

// signer signs variant keys with one key. Making an HMAC hashes the key
// twice, so it keeps the HMACs it made, each with a buffer for the key and
// the sum.
type signer struct {
	pool sync.Pool
}

// signing is one HMAC of a signer with its buffers.
type signing struct {
	mac hash.Hash
	in  []byte
	sum []byte
}

// signatureLength is the length of a signature: 12 bytes of the HMAC in
// unpadded URL-safe base64.
const signatureLength = 16

func newSigner(key []byte) *signer {
	s := &signer{}
	s.pool.New = func() any {
		return &signing{mac: hmac.New(sha256.New, key), sum: make([]byte, 0, sha256.Size)}
	}

	return s
}

// sign is the signature of a variant key.
func (s *signer) sign(key string) string {
	signing := s.pool.Get().(*signing)
	defer s.pool.Put(signing)

	signing.mac.Reset()
	signing.in = append(signing.in[:0], key...)
	signing.mac.Write(signing.in)
	signing.sum = signing.mac.Sum(signing.sum[:0])

	var signature [signatureLength]byte

	base64.RawURLEncoding.Encode(signature[:], signing.sum[:12])

	return string(signature[:])
}

// appendKey appends the storage key of the variant of an asset (see
// Variant.key).
func (v Variant) appendKey(out []byte, assetID string) []byte {
	out = append(out, assetID...)
	out = append(out, '_')
	out = strconv.AppendInt(out, int64(v.Width), 10)
	out = append(out, 'x')
	out = strconv.AppendInt(out, int64(v.Height), 10)

	if v.Crop {
		out = append(out, 'c')

		if focus := v.focus(); !focus.isCentre() {
			out = append(out, '-')
			out = strconv.AppendInt(out, int64(thousandths(focus.X)), 10)
			out = append(out, '-')
			out = strconv.AppendInt(out, int64(thousandths(focus.Y)), 10)
		}
	}

	out = append(out, '.')

	return append(out, v.Format...)
}

// variantPath is the address of a variant, by its signature and key, from
// the library's route named variant.
func (s *Service) variantPath(signature, key string) (string, error) {
	return s.routePath("variant", "signature", signature, "key", key)
}

// isUUID reports whether text is an asset ID, a UUID in hexadecimal digits
// of either case, as assetIDPattern matches it.
func isUUID(text string) bool {
	if len(text) != 36 {
		return false
	}

	for i := range len(text) {
		c := text[i]

		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexDigit(c) {
				return false
			}
		}
	}

	return true
}

func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
