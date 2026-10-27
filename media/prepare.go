package media

import (
	"context"
	"errors"
	"log/slog"
)

// The first visitor of a page with a new image would wait for its variants
// to be made. Prepare makes them in the background after the upload
// instead: the copy pages show by default and the smaller ones of its
// srcset (see Image). The upload itself is kept as it is.

// prepareQueue is how many uploaded images can wait for Prepare; the
// variants of images beyond it, as in a large import, are made on their
// first request.
const prepareQueue = 1024

// Prepare makes the variants pages show of each uploaded image, one image
// at a time, until ctx ends: the ImageWidth copy first, then the smaller
// ones of its srcset. It shares the limit on variants made at once with
// requests, and a request for a variant it is making waits for it rather
// than making it again. When ctx ends it finishes the variant it is making
// and drops the images still waiting. App runs it from Start until Close;
// without it, variants are made on their first request.
func (s *Service) Prepare(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.dropPrepared()

			return
		case id := <-s.preparing:
			s.prepareImage(ctx, id)
			s.donePreparing()
		}
	}
}

// WaitPrepared waits until Prepare has made the variants of the images
// uploaded so far, or ctx ends, for tests and tools that need them stored.
func (s *Service) WaitPrepared(ctx context.Context) error {
	s.pendingMu.Lock()

	if s.pending == 0 {
		s.pendingMu.Unlock()

		return nil
	}

	prepared := s.prepared
	s.pendingMu.Unlock()

	select {
	case <-prepared:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// prepare queues an uploaded image for Prepare without waiting; a full
// queue leaves its variants to their first request.
func (s *Service) prepare(ctx context.Context, id string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	select {
	case s.preparing <- id:
	default:
		slog.DebugContext(ctx, "media variants left to their first request", "asset", id)

		return
	}

	if s.pending == 0 {
		s.prepared = make(chan struct{})
	}

	s.pending++
}

// donePreparing counts an image off the queue.
func (s *Service) donePreparing() {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	s.pending--

	if s.pending == 0 {
		close(s.prepared)
	}
}

// dropPrepared empties the queue when Prepare stops.
func (s *Service) dropPrepared() {
	for {
		select {
		case <-s.preparing:
			s.donePreparing()
		default:
			return
		}
	}
}

// prepareImage makes the variants Image gives of an image at ImageWidth
// that are not stored yet. Assets that are gone, or not committed yet, are
// left out.
func (s *Service) prepareImage(ctx context.Context, id string) {
	info, err := s.info(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) && ctx.Err() == nil {
			slog.WarnContext(ctx, "prepare media variants", "asset", id, "error", err)
		}

		return
	}

	if !info.resizable() || info.width <= 0 {
		return
	}

	widths := srcsetWidths(min(ImageWidth, info.width, MaxVariantSize))

	// The largest is the copy pages show by default.
	for index := len(widths) - 1; index >= 0; index-- {
		if ctx.Err() != nil {
			return
		}

		variant := Variant{Width: widths[index], Format: DefaultVariantFormat}
		key := variant.key(id)

		if s.stored(ctx, key) {
			continue
		}

		if _, err := s.makeOnce(ctx, id, key, variant); err != nil {
			if !errors.Is(err, ErrNotFound) {
				slog.WarnContext(ctx, "prepare media variants", "asset", id, "key", key, "error", err)
			}

			return
		}
	}
}

// stored reports whether the Storage has a file under key.
func (s *Service) stored(ctx context.Context, key string) bool {
	file, err := s.storage.Open(ctx, key)
	if err != nil {
		return false
	}

	_ = file.Close()

	return true
}
