package media

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}(?:\.[a-z0-9]{1,10})?$`)

// Filesystem uses os.Root so even a symlink cannot escape the asset directory.
// The application owns Close. Files are served by the library's routes
// (Service.RegisterRoutes), or at a base URL of their own.
type Filesystem struct {
	root    *os.Root
	baseURL string
}

// NewFilesystem keeps files in directory, made when missing. An empty
// baseURL leaves serving them to the library's routes (see
// Service.RegisterRoutes), as webthings.App does; otherwise they are served
// at baseURL, a path on the site or an absolute http(s) address, for example
// by a file server or a CDN. It returns ErrInvalid for any other baseURL.
func NewFilesystem(directory, baseURL string) (*Filesystem, error) {
	if baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || !validBaseURL(parsed, baseURL) {
			return nil, ErrInvalid
		}
	}

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}

	return &Filesystem{root: root, baseURL: strings.TrimSuffix(baseURL, "/")}, nil
}

// validBaseURL accepts a site-relative path or an absolute http(s) URL,
// without query, fragment or credentials.
func validBaseURL(parsed *url.URL, raw string) bool {
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || strings.HasPrefix(raw, "//") {
		return false
	}

	if parsed.Scheme == "" {
		return strings.HasPrefix(raw, "/")
	}

	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// Close releases the directory.
func (s *Filesystem) Close() error {
	return s.root.Close()
}

// Ping checks that the asset directory can still be read.
func (s *Filesystem) Ping(context.Context) error {
	_, err := s.root.Stat(".")

	return err
}

// Put implements Storage. A write that ends short of size leaves no file
// behind.
func (s *Filesystem) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	if !keyPattern.MatchString(key) {
		return ErrUnsafeKey
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	file, err := s.root.OpenFile(key, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	written, writeErr := io.Copy(file, reader)
	closeErr := file.Close()
	err = errors.Join(writeErr, closeErr)

	if err == nil && written != size {
		err = io.ErrUnexpectedEOF
	}

	if err != nil {
		err = errors.Join(err, s.root.Remove(key))
	}

	return err
}

// Open implements Storage.
func (s *Filesystem) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !keyPattern.MatchString(key) {
		return nil, ErrUnsafeKey
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.root.Open(key)
}

// Delete implements Storage.
func (s *Filesystem) Delete(ctx context.Context, key string) error {
	if !keyPattern.MatchString(key) {
		return ErrUnsafeKey
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	err := s.root.Remove(key)

	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return err
}

// URL implements Storage: the key below the base URL, or "" without one,
// for the library's routes to serve.
func (s *Filesystem) URL(key string) string {
	if s.baseURL == "" {
		return ""
	}

	return s.baseURL + "/" + key
}
