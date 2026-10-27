// Package redirects sends visitors from old URLs to new ones, for example
// after moving a site to WebThings. Editors manage the redirects in the admin:
// an old path, a new address (a path on this site or a full URL) and the kind
// of redirect (301, 302, 307 or 308). Each redirect counts its hits. Add the
// module with:
//
//	app.Use(redirects.Module())
//
// Its admin section is at <AdminPath>/redirects; redirects.Module(redirects.Named("old",
// "Old addresses")) chooses another key and title.
//
// The module wraps the public site. GET and HEAD requests whose path matches
// a redirect are redirected before the site handles them; the query string is
// kept when the new address has none. Paths are compared after normalising:
// host, query and fragment are dropped, a leading slash is added, trailing
// slashes and dot segments are removed, and case is kept. The home page "/"
// and the paths WebThings serves itself (webthings.App.CorePaths: the admin,
// visitor actions (/_/), media, assets, theme files, sitemap.xml,
// robots.txt, /healthz, /readyz and /metrics) are never redirected.
//
// Requests that end in "not found" are counted as missed paths; the admin
// lists the most frequent ones so editors can add redirects for them. A "not
// found" that asks not to be indexed (X-Robots-Tag: noindex or none), such as
// a mistyped registration code, is not a missed page and is not counted.
//
// The admin can import redirects as CSV, one per line: old path, new
// address and, optionally, the status (301 when empty). A first line starting
// with "from" is skipped, and semicolons may separate the fields instead of
// commas. An import saves all lines or, when one is wrong, none.
//
// Deleting a redirect moves it to the admin's trash: it stops redirecting
// and its path is free for a new redirect. Restoring it fails while another
// redirect uses the path.
//
// The module also redirects for the page editor (webthings.Parts.Redirects,
// see Service.Redirect): when a page that published content links to is
// unpublished, or a revision that changes its address is published, the
// editor offers to redirect the addresses it leaves, and the report of
// broken links offers to redirect an address that links lead to.
//
// The module has no public templates or template functions.
package redirects

import (
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Statuses are the HTTP statuses a redirect can answer with.
var Statuses = []int{301, 302, 307, 308}

const (
	maxPath   = 500
	maxTarget = 2000
	// maxImport limits the lines of one CSV import, maxImportSize its
	// bytes.
	maxImport     = 5000
	maxImportSize = 1 << 20
	// maxHops is how far loops are followed through other redirects.
	maxHops = 20
)

var (
	errFrom      = errors.New("redirects_invalid_from")
	errReserved  = errors.New("redirects_reserved_from")
	errTo        = errors.New("redirects_invalid_to")
	errStatus    = errors.New("redirects_invalid_status")
	errLoop      = errors.New("redirects_loop")
	errDuplicate = errors.New("redirects_duplicate_from")
	// errTaken is a redirect in the trash whose path another redirect uses now.
	errTaken    = errors.New("redirects_undelete_taken")
	errRepeated = errors.New("redirects_import_repeated_from")
	errEmpty    = errors.New("redirects_empty_import")
	errTooMany  = errors.New("redirects_import_too_many_rows")
	errTooLarge = errors.New("redirects_import_too_large")
	errColumns  = errors.New("redirects_import_columns")
)

// Redirect sends requests for From to To.
type Redirect struct {
	ID string `db:"id"`
	// From is the normalised old path, for example "/old-page".
	From string `db:"from_path"`
	// To is a path on the site, such as "/en/news", or an absolute URL.
	To      string     `db:"to_url"`
	Status  int        `db:"status"`
	Hits    int64      `db:"hits"`
	LastHit *time.Time `db:"last_hit_at"`
	// CreatedAt is when the redirect was added, CreatedBy by whom (empty for
	// the system).
	CreatedAt time.Time `db:"created_at"`
	CreatedBy db.Ref    `db:"created_by"`
	// UpdatedAt and UpdatedBy are the last change by an editor; hits do not
	// count as changes.
	UpdatedAt time.Time `db:"updated_at"`
	UpdatedBy db.Ref    `db:"updated_by"`
	// DeletedAt is set while the redirect is in the trash, with DeletedBy.
	DeletedAt *time.Time `db:"deleted_at"`
	DeletedBy db.Ref     `db:"deleted_by"`
}

// columns are the columns of Redirect.
const columns = `id, from_path, to_url, status, hits, last_hit_at,
	created_at, created_by, updated_at, updated_by, deleted_at, deleted_by`

// Miss is a path that was not found, with how often and when it was last
// requested.
type Miss struct {
	Path    string    `db:"path"`
	Hits    int64     `db:"hits"`
	LastHit time.Time `db:"last_hit_at"`
}

// Service is one Redirects instance, for the website's own code.
type Service struct {
	key string
	db  *db.DB
	// host is the site's own host from BaseURL; absolute URLs to it count
	// as paths on the site when looking for loops.
	host string
	// isCorePath reports the paths the app serves itself
	// (webthings.Env.IsCorePath).
	isCorePath func(path string) bool

	// table is the in-memory copy of the redirects the middleware reads.
	table     atomic.Pointer[table]
	reloading sync.Mutex
	counts    counter
}

// Of returns the Redirects instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// List returns all redirects ordered by old path, without those in the
// trash.
func (s *Service) List(ctx context.Context) ([]Redirect, error) {
	var list []Redirect

	err := s.db.Select(ctx, &list, `
		SELECT `+columns+`
		FROM wt_redirect
		WHERE instance=? AND deleted_at IS NULL
		ORDER BY from_path`,
		s.key,
	)

	return list, err
}

// Get returns one redirect, or db.ErrNotFound when it does not exist or is in
// the trash.
func (s *Service) Get(ctx context.Context, id string) (Redirect, error) {
	var redirect Redirect

	err := s.db.Get(ctx, &redirect, `
		SELECT `+columns+`
		FROM wt_redirect
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		s.key, id,
	)

	return redirect, err
}

// Save validates and stores a redirect: a new one when ID is empty,
// otherwise it changes From, To and Status of an existing one. A redirect
// that would lead back to its own path, directly or through other redirects,
// is refused.
func (s *Service) Save(ctx context.Context, redirect Redirect) (Redirect, error) {
	redirect, err := s.clean(redirect)
	if err != nil {
		return redirect, err
	}

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		existing, err := s.List(ctx)
		if err != nil {
			return err
		}

		rules := s.rules(existing, redirect.ID)
		if s.loops(redirect.From, redirect.To, rules) {
			return errLoop
		}

		if redirect.ID == "" {
			redirect.ID = db.NewID()

			return s.insert(ctx, redirect)
		}

		if _, err := s.Get(ctx, redirect.ID); err != nil {
			return err
		}

		return s.update(ctx, redirect)
	})
	if err != nil {
		return redirect, err
	}

	s.reload(ctx)

	return s.Get(ctx, redirect.ID)
}

func (s *Service) insert(ctx context.Context, redirect Redirect) error {
	now, actor := db.Stamp(ctx)

	_, err := s.db.Exec(ctx, `
		INSERT INTO wt_redirect
			(id, instance, from_path, to_url, status, hits, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		redirect.ID, s.key, redirect.From, redirect.To, redirect.Status, now, actor, now, actor,
	)
	if db.IsUnique(err) {
		return errDuplicate
	}

	if err != nil {
		return err
	}

	return s.forget(ctx, redirect.From)
}

func (s *Service) update(ctx context.Context, redirect Redirect) error {
	at, by := db.Stamp(ctx)

	_, err := s.db.Exec(ctx, `
		UPDATE wt_redirect SET from_path=?, to_url=?, status=?, updated_at=?, updated_by=?
		WHERE instance=? AND id=? AND deleted_at IS NULL`,
		redirect.From, redirect.To, redirect.Status, at, by, s.key, redirect.ID,
	)
	if db.IsUnique(err) {
		return errDuplicate
	}

	if err != nil {
		return err
	}

	return s.forget(ctx, redirect.From)
}

// forget removes a path from the missed paths once it redirects.
func (s *Service) forget(ctx context.Context, from string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM wt_redirect_miss WHERE instance=? AND path=?`, s.key, from)

	return err
}

// Redirect sends the visitors of address to target, a path on this site
// or a full URL, with a permanent redirect (301) from now on: it replaces
// the redirect of address when there is one, and moves a redirect of
// target to the trash, as it would hide the page that is there now. It is
// the module's links.Redirector (webthings.Parts.Redirects): the admin
// redirects with it the addresses that links lead to when a page leaves
// them. A reserved address, or one target leads back to, is refused, as by
// Save.
func (s *Service) Redirect(ctx context.Context, address, target string) error {
	redirect, err := s.clean(Redirect{From: address, To: target, Status: 301})
	if err != nil {
		return err
	}

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		existing, err := s.List(ctx)
		if err != nil {
			return err
		}

		hiding := s.local(redirect.To)
		kept := existing[:0]

		for _, other := range existing {
			if other.From == hiding && hiding != redirect.From {
				if err := s.rows().Delete(ctx, other.ID); err != nil {
					return err
				}

				continue
			}

			if other.From == redirect.From {
				redirect.ID = other.ID
			}

			kept = append(kept, other)
		}

		if s.loops(redirect.From, redirect.To, s.rules(kept, redirect.ID)) {
			return errLoop
		}

		if redirect.ID == "" {
			redirect.ID = db.NewID()

			return s.insert(ctx, redirect)
		}

		return s.update(ctx, redirect)
	})
	if err != nil {
		return err
	}

	s.reload(ctx)

	return nil
}

// Redirected returns where the redirect of address, a path on this site,
// sends visitors; ok is false when no redirect has it. It is part of the
// module's links.Redirector, with Redirect.
func (s *Service) Redirected(ctx context.Context, address string) (string, bool) {
	redirect, ok := s.Resolve(ctx, address)

	return redirect.To, ok
}

// rows is the instance's part of wt_redirect, for moving redirects to the
// trash and back.
func (s *Service) rows() db.SoftTable {
	return db.SoftTable{DB: s.db, Table: "wt_redirect", Scope: "instance=?", Args: []any{s.key}}
}

// Delete moves a redirect to the trash: it stops redirecting and its path
// is free for a new redirect. An unknown one, or one already in the trash,
// is db.ErrNotFound.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.rows().Delete(ctx, id); err != nil {
		return err
	}

	s.reload(ctx)

	return nil
}

// Trashed returns redirects in the trash, most recently deleted first: at
// most limit of them from offset (all for a limit of 0), and how many the
// trash holds in all.
func (s *Service) Trashed(ctx context.Context, limit, offset int) ([]Redirect, int, error) {
	var list []Redirect

	total, err := s.rows().Trashed(ctx, &list, columns, limit, offset)

	return list, total, err
}

// Undelete brings a redirect back from the trash. When another redirect
// uses its path now, or it would lead back to its own path through the
// redirects made meanwhile, it stays in the trash (errTaken, errLoop). One
// that is not in the trash is db.ErrNotFound.
func (s *Service) Undelete(ctx context.Context, id string) error {
	err := s.db.Tx(ctx, func(ctx context.Context) error {
		var redirect Redirect

		err := s.rows().Undelete(ctx, id, func(ctx context.Context) error {
			if err := s.rows().GetTrashed(ctx, &redirect, columns, id); err != nil {
				return err
			}

			return s.restorable(ctx, redirect)
		})
		if db.IsUnique(err) {
			return errTaken
		}

		if err != nil {
			return err
		}

		// The path redirects again, so it is no longer a missed path.
		return s.forget(ctx, redirect.From)
	})
	if err != nil {
		return err
	}

	s.reload(ctx)

	return nil
}

// restorable returns errTaken when another redirect uses the path of
// redirect now, and errLoop when it would lead back to its own path through
// the redirects made meanwhile.
func (s *Service) restorable(ctx context.Context, redirect Redirect) error {
	existing, err := s.List(ctx)
	if err != nil {
		return err
	}

	// Checked first, because Postgres aborts the transaction on a unique
	// violation; the index still guards against races.
	for _, other := range existing {
		if other.From == redirect.From {
			return errTaken
		}
	}

	if s.loops(redirect.From, redirect.To, s.rules(existing, "")) {
		return errLoop
	}

	return nil
}

// Purge removes a redirect in the trash for good; one that is not in the
// trash is db.ErrNotFound.
func (s *Service) Purge(ctx context.Context, id string) error {
	return s.rows().Purge(ctx, id)
}

// LineError is a CSV line an import refused.
type LineError struct {
	Line int
	Err  error
}

// ImportError lists the lines an import refused; nothing was saved.
type ImportError struct {
	Lines []LineError
}

// Error lists the refused lines with their reasons.
func (e *ImportError) Error() string {
	parts := make([]string, 0, len(e.Lines))

	for _, line := range e.Lines {
		parts = append(parts, fmt.Sprintf("line %d: %v", line.Line, line.Err))
	}

	return "redirects import: " + strings.Join(parts, "; ")
}

// Import reads CSV lines "old path,new address[,status]" and saves them,
// replacing redirects with the same old path. It saves all lines or, when a
// line is wrong, none; then the error is an *ImportError. A source over 1
// MiB or 5000 lines is refused whole. It returns the number of redirects
// saved.
func (s *Service) Import(ctx context.Context, source io.Reader) (int, error) {
	records, err := readCSV(source)
	if err != nil {
		return 0, err
	}

	var saved int

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		existing, err := s.List(ctx)
		if err != nil {
			return err
		}

		byPath := map[string]Redirect{}
		for _, redirect := range existing {
			byPath[redirect.From] = redirect
		}

		rules := s.rules(existing, "")
		redirects, problems := s.parse(records, byPath, rules)

		if len(problems) > 0 {
			return &ImportError{Lines: problems}
		}

		for _, redirect := range redirects {
			if redirect.ID == "" {
				redirect.ID = db.NewID()
				err = s.insert(ctx, redirect)
			} else {
				err = s.update(ctx, redirect)
			}

			if err != nil {
				return err
			}
		}

		saved = len(redirects)

		return nil
	})
	if err != nil {
		return 0, err
	}

	s.reload(ctx)

	return saved, nil
}

// record is one CSV line with its line number.
type record struct {
	line   int
	fields []string
}

// readCSV reads the lines of an import (see admin.ReadCSV) without the
// header line "from,to,status" when there is one. A source over 1 MiB is
// refused whole, as cutting it would import only the lines before the cut.
func readCSV(source io.Reader) ([]record, error) {
	data, err := io.ReadAll(io.LimitReader(source, maxImportSize+1))
	if err != nil {
		return nil, err
	}

	if len(data) > maxImportSize {
		return nil, errTooLarge
	}

	rows, err := admin.ReadCSV(bytes.NewReader(data))

	var malformed *csv.ParseError
	if errors.As(err, &malformed) {
		return nil, &ImportError{Lines: []LineError{{Line: malformed.StartLine, Err: errColumns}}}
	}

	if err != nil {
		return nil, err
	}

	var records []record

	for i, row := range rows {
		if i == 0 && strings.EqualFold(strings.TrimSpace(row.Values[0]), "from") {
			continue
		}

		// A value over several lines is a quote that was never closed: no
		// address has a line break.
		if slices.ContainsFunc(row.Values, func(value string) bool {
			return strings.ContainsAny(value, "\r\n")
		}) {
			return nil, &ImportError{Lines: []LineError{{Line: row.Line, Err: errColumns}}}
		}

		records = append(records, record{line: row.Line, fields: row.Values})

		if len(records) > maxImport {
			return nil, errTooMany
		}
	}

	if len(records) == 0 {
		return nil, errEmpty
	}

	return records, nil
}

// parse validates the records of an import against the existing redirects
// (byPath) and each other. rules grows with every line, so loops between
// lines are found too.
func (s *Service) parse(records []record, byPath map[string]Redirect, rules map[string]string) ([]Redirect, []LineError) {
	var (
		redirects []Redirect
		problems  []LineError
		seen      = map[string]bool{}
	)

	for _, rec := range records {
		redirect, err := s.parseRecord(rec.fields)
		if err == nil && seen[redirect.From] {
			err = errRepeated
		}

		if err == nil && s.loops(redirect.From, redirect.To, without(rules, redirect.From)) {
			err = errLoop
		}

		if err != nil {
			problems = append(problems, LineError{Line: rec.line, Err: err})

			continue
		}

		seen[redirect.From] = true
		redirect.ID = byPath[redirect.From].ID

		if target := s.local(redirect.To); target != "" {
			rules[redirect.From] = target
		} else {
			delete(rules, redirect.From)
		}

		redirects = append(redirects, redirect)
	}

	return redirects, problems
}

func (s *Service) parseRecord(fields []string) (Redirect, error) {
	if len(fields) < 2 || len(fields) > 3 {
		return Redirect{}, errColumns
	}

	redirect := Redirect{From: fields[0], To: fields[1], Status: 301}

	if len(fields) == 3 && strings.TrimSpace(fields[2]) != "" {
		status, err := strconv.Atoi(strings.TrimSpace(fields[2]))
		if err != nil {
			return Redirect{}, errStatus
		}

		redirect.Status = status
	}

	return s.clean(redirect)
}

// without returns rules without from, the redirect being replaced.
func without(rules map[string]string, from string) map[string]string {
	if _, ok := rules[from]; !ok {
		return rules
	}

	copied := make(map[string]string, len(rules))

	for key, value := range rules {
		if key != from {
			copied[key] = value
		}
	}

	return copied
}

// clean normalises and validates the editable fields of a redirect.
func (s *Service) clean(redirect Redirect) (Redirect, error) {
	from, err := Normalize(redirect.From)
	if err != nil {
		return redirect, err
	}

	if s.reserved(from) {
		return redirect, errReserved
	}

	to, err := cleanTarget(redirect.To)
	if err != nil {
		return redirect, err
	}

	if !slices.Contains(Statuses, redirect.Status) {
		return redirect, errStatus
	}

	redirect.From = from
	redirect.To = to

	return redirect, nil
}

// Normalize turns an old address into the path redirects are matched on: the
// host, query and fragment are dropped, a leading slash is added and
// trailing slashes and dot segments are removed. "https://old.example/a/?b=1"
// becomes "/a".
func Normalize(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", errFrom
	}

	parsed, err := url.Parse(address)
	if err != nil {
		return "", errFrom
	}

	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errFrom
	}

	cleaned := cleanPath(parsed.Path)
	if len(cleaned) > maxPath {
		return "", errFrom
	}

	return cleaned, nil
}

// cleanPath is the form request paths are compared in.
func cleanPath(p string) string {
	return path.Clean("/" + p)
}

// reserved paths belong to WebThings itself and are never redirected: the
// home page and the core paths, which core serves before the middleware
// sees them. A Service made without the app knows the default ones.
func (s *Service) reserved(p string) bool {
	isCorePath := s.isCorePath
	if isCorePath == nil {
		isCorePath = webthings.Env{}.IsCorePath
	}

	return p == "/" || isCorePath(p)
}

// cleanTarget accepts a path on the site or an absolute http(s) URL.
func cleanTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" || len(target) > maxTarget || strings.ContainsAny(target, " \t\r\n") {
		return "", errTo
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return "", errTo
	}

	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") && parsed.Scheme == "" {
		return target, nil
	}

	if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		return target, nil
	}

	return "", errTo
}

// local returns the normalised path of a target on this site, or "" for
// other websites.
func (s *Service) local(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return ""
	}

	if parsed.Host != "" && !strings.EqualFold(parsed.Host, s.host) {
		return ""
	}

	return cleanPath(parsed.Path)
}

// rules maps the old paths of redirects to their local targets, without the
// redirect with skip as ID.
func (s *Service) rules(redirects []Redirect, skip string) map[string]string {
	rules := map[string]string{}

	for _, redirect := range redirects {
		if redirect.ID == skip && skip != "" {
			continue
		}

		if target := s.local(redirect.To); target != "" {
			rules[redirect.From] = target
		}
	}

	return rules
}

// loops reports whether following to through rules comes back at from.
func (s *Service) loops(from, to string, rules map[string]string) bool {
	current := s.local(to)

	for range maxHops {
		if current == "" {
			return false
		}

		if current == from {
			return true
		}

		current = rules[current]
	}

	return false
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
