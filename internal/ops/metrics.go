package ops

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/donseba/go-partial/exp/metrics"
	"github.com/donseba/go-router/middleware"
)

// Metrics counts the site's requests and renders and writes them in the
// Prometheus text format. It keeps a few counters and histograms per route
// class and render kind, so it stays small whatever the site's URLs are.
type Metrics struct {
	started  time.Time
	inFlight atomic.Int64

	mu               sync.Mutex
	requests         map[requestKey]uint64
	requestDurations map[string]*histogram
	renders          map[renderKey]uint64
	renderDurations  map[string]*histogram
}

type requestKey struct {
	class  string
	method string
	code   int
}

type renderKey struct {
	kind   string
	result string
}

// requestBuckets are the upper bounds of the request duration histogram, in
// seconds; renderBuckets those of a single template's render.
var (
	requestBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	renderBuckets  = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 1}
)

// NewMetrics returns empty metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		started:          time.Now(),
		requests:         map[requestKey]uint64{},
		requestDurations: map[string]*histogram{},
		renders:          map[renderKey]uint64{},
		renderDurations:  map[string]*histogram{},
	}
}

// ObserveRequest counts one answered request.
func (m *Metrics) ObserveRequest(class, method string, code int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests[requestKey{class: class, method: method, code: code}]++

	durations := m.requestDurations[class]
	if durations == nil {
		durations = newHistogram(requestBuckets)
		m.requestDurations[class] = durations
	}

	durations.observe(duration.Seconds())
}

// Record counts one render of go-partial; Metrics is a sink of its
// exp/metrics stage.
func (m *Metrics) Record(record metrics.Record) {
	kind := cmp.Or(string(record.Kind), "unknown")

	result := "ok"
	if record.Error != nil {
		result = "error"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.renders[renderKey{kind: kind, result: result}]++

	durations := m.renderDurations[kind]
	if durations == nil {
		durations = newHistogram(renderBuckets)
		m.renderDurations[kind] = durations
	}

	durations.observe(record.Duration.Seconds())
}

// WriteTo writes the metrics in the Prometheus text format, followed by what
// extra writes, for example the database's connection pool.
func (m *Metrics) WriteTo(w io.Writer, extra ...func(*Exposition)) error {
	out := &Exposition{w: bufio.NewWriter(w)}

	m.mu.Lock()
	m.writeRequests(out)
	m.writeRenders(out)
	m.mu.Unlock()

	out.Gauge("webthings_http_requests_in_flight", "Requests being answered.", float64(m.inFlight.Load()))
	out.Gauge("webthings_start_time_seconds", "When the process started serving, in Unix seconds.", float64(m.started.Unix()))

	for _, write := range extra {
		write(out)
	}

	return out.w.Flush()
}

func (m *Metrics) writeRequests(out *Exposition) {
	out.family("webthings_http_requests_total", "counter", "Requests answered, by route class, method and status code.")

	keys := slices.SortedFunc(maps.Keys(m.requests), func(a, b requestKey) int {
		return cmp.Or(strings.Compare(a.class, b.class), strings.Compare(a.method, b.method), cmp.Compare(a.code, b.code))
	})

	for _, key := range keys {
		out.sample("webthings_http_requests_total", labels(
			"class", key.class,
			"method", key.method,
			"code", strconv.Itoa(key.code),
		), float64(m.requests[key]))
	}

	out.histograms(
		"webthings_http_request_duration_seconds",
		"How long requests took to answer, by route class.",
		"class",
		m.requestDurations,
	)
}

func (m *Metrics) writeRenders(out *Exposition) {
	out.family("webthings_render_total", "counter", "Templates rendered by go-partial, by render kind and result.")

	keys := slices.SortedFunc(maps.Keys(m.renders), func(a, b renderKey) int {
		return cmp.Or(strings.Compare(a.kind, b.kind), strings.Compare(a.result, b.result))
	})

	for _, key := range keys {
		out.sample("webthings_render_total", labels("kind", key.kind, "result", key.result), float64(m.renders[key]))
	}

	out.histograms(
		"webthings_render_duration_seconds",
		"How long a template took to render, by render kind.",
		"kind",
		m.renderDurations,
	)
}

// Exposition writes metric families in the Prometheus text format.
type Exposition struct {
	w *bufio.Writer
}

// Gauge writes a gauge without labels.
func (e *Exposition) Gauge(name, help string, value float64) {
	e.family(name, "gauge", help)
	e.sample(name, "", value)
}

// Counter writes a counter without labels.
func (e *Exposition) Counter(name, help string, value float64) {
	e.family(name, "counter", help)
	e.sample(name, "", value)
}

func (e *Exposition) family(name, kind, help string) {
	_, _ = fmt.Fprintf(e.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

func (e *Exposition) sample(name, labels string, value float64) {
	_, _ = e.w.WriteString(name + labels + " " + strconv.FormatFloat(value, 'g', -1, 64) + "\n")
}

// histograms writes one histogram per value of label.
func (e *Exposition) histograms(name, help, label string, series map[string]*histogram) {
	e.family(name, "histogram", help)

	for _, value := range slices.Sorted(maps.Keys(series)) {
		h := series[value]

		var cumulative uint64

		for i, bound := range h.bounds {
			cumulative += h.counts[i]
			bucket := labels(label, value, "le", strconv.FormatFloat(bound, 'g', -1, 64))
			e.sample(name+"_bucket", bucket, float64(cumulative))
		}

		e.sample(name+"_bucket", labels(label, value, "le", "+Inf"), float64(h.count))
		e.sample(name+"_sum", labels(label, value), h.sum)
		e.sample(name+"_count", labels(label, value), float64(h.count))
	}
}

// labels formats name/value pairs as {name="value",…}; the values are route
// classes, methods and kinds WebThings chooses, but quotes are escaped all
// the same.
func labels(pairs ...string) string {
	var out strings.Builder

	out.WriteString("{")

	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			out.WriteString(",")
		}

		out.WriteString(pairs[i] + "=" + strconv.Quote(pairs[i+1]))
	}

	out.WriteString("}")

	return out.String()
}

// histogram counts observations per bucket; counts[i] holds those at most
// bounds[i] and above the bound before. Its Metrics' mutex guards it.
type histogram struct {
	bounds []float64
	counts []uint64
	sum    float64
	count  uint64
}

func newHistogram(bounds []float64) *histogram {
	return &histogram{bounds: bounds, counts: make([]uint64, len(bounds))}
}

func (h *histogram) observe(value float64) {
	h.sum += value
	h.count++

	for i, bound := range h.bounds {
		if value <= bound {
			h.counts[i]++

			return
		}
	}
}

// method is the request method for a label; unusual ones are "other", so a
// client cannot add label values.
func method(r *http.Request) string {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return r.Method
	default:
		return "other"
	}
}

// Observe counts every request in metrics under the route class class
// gives it and, when log is not nil, writes an access log line for it:
// method, path (without the query, which may hold tokens), status, size,
// duration, route class and request ID. It is go-router's Observe, so a
// request is recorded also when a panic passes through, such as
// http.ErrAbortHandler after the response started: as a 500 when no status
// was written yet. The panic goes on.
func Observe(m *Metrics, log *slog.Logger, class func(*http.Request) string) func(http.Handler) http.Handler {
	// LogHandler adds the request ID once, also when log already has it.
	if log != nil {
		log = slog.New(middleware.LogHandler(log.Handler()))
	}

	observe := middleware.Observe(func(r *http.Request, info middleware.ResponseInfo) {
		record(m, log, r, class(r), info)
	})

	return func(next http.Handler) http.Handler {
		observed := observe(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.inFlight.Add(1)
			defer m.inFlight.Add(-1)

			observed.ServeHTTP(w, r)
		})
	}
}

// record counts a request in the metrics and writes its access log line,
// when there is an access log.
func record(m *Metrics, log *slog.Logger, r *http.Request, class string, info middleware.ResponseInfo) {
	m.ObserveRequest(class, method(r), info.Status, info.Duration)

	if log == nil {
		return
	}

	log.LogAttrs(r.Context(), slog.LevelInfo, "request",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", info.Status),
		slog.Int64("bytes", info.Bytes),
		slog.Float64("duration_ms", float64(info.Duration.Microseconds())/1000),
		slog.String("class", class),
	)
}
