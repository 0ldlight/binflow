// Package metrics is the concurrency-safe metric registry behind GET
// /metrics (ADR-0022: stdlib-only implementation, hand-written Prometheus
// text-format serialization — no prometheus/client_golang dependency).
//
// The storage is a sync.Map of metric families keyed by name; every family
// holds its label series in a second sync.Map whose values are lock-free
// cells (CAS-loop float64, atomic counts). Inc/Set/Observe never block the
// request path; Format takes a copy-and-sort snapshot under Range.
//
// Metric family layout (four families, T-163/FR-61): HTTP (request counter,
// latency histogram, in-flight gauge), storage (blob count / byte gauges,
// engine label), auth (login counter by source) and replication (task gauge
// by status). This package is generic plumbing only — the BinFlow family
// names, labels and snapshot sources live in internal/httpapi (the consumer,
// per the interfaces-at-the-consumer convention).
package metrics

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Metric types of the Prometheus data model. A family's type is declared at
// registration and fixes both the # TYPE line and the sample rendering.
const (
	TypeCounter   = "counter"
	TypeGauge     = "gauge"
	TypeHistogram = "histogram"
)

// DefaultBuckets are the latency buckets the HTTP histogram registers with
// (PRD FR-61: .005 .01 .025 .05 .1 .25 .5 1 2.5 5 10 seconds). The implicit
// +Inf bucket is rendered by Format and must not appear in the slice.
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Registry is the process metric store. It is safe for concurrent use; one
// registry serves one server process (cmd constructs it, httpapi registers
// the families and mounts the endpoint).
type Registry struct {
	families sync.Map // metric name -> *family
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// NewCounter registers (or idempotently returns) a counter family.
func (r *Registry) NewCounter(name, help string) (*Counter, error) {
	f, err := r.register(name, help, TypeCounter, nil)
	if err != nil {
		return nil, err
	}
	return &Counter{f: f}, nil
}

// NewGauge registers (or idempotently returns) a gauge family.
func (r *Registry) NewGauge(name, help string) (*Gauge, error) {
	f, err := r.register(name, help, TypeGauge, nil)
	if err != nil {
		return nil, err
	}
	return &Gauge{f: f}, nil
}

// NewHistogram registers (or idempotently returns) a histogram family with
// the given upper bounds. Buckets must be sorted ascending, non-empty and
// finite; the +Inf bucket is implicit.
func (r *Registry) NewHistogram(name, help string, buckets []float64) (*Histogram, error) {
	f, err := r.register(name, help, TypeHistogram, buckets)
	if err != nil {
		return nil, err
	}
	return &Histogram{f: f}, nil
}

// register enters one family. Registering the SAME specification twice
// returns the existing family (assembly code and tests may re-run); a name
// reused with a different type, help or bucket set is an error, so two
// subsystems can never silently share a metric name with divergent meaning.
func (r *Registry) register(name, help, typ string, buckets []float64) (*family, error) {
	if err := validateMetricName(name); err != nil {
		return nil, err
	}
	if err := validateBuckets(typ, buckets); err != nil {
		return nil, err
	}
	f := &family{name: name, help: help, typ: typ, buckets: buckets}
	if actual, loaded := r.families.LoadOrStore(name, f); loaded {
		prev := actual.(*family)
		if prev.typ != typ || prev.help != help || !sameBuckets(prev.buckets, buckets) {
			return nil, fmt.Errorf("metrics: %q is already registered with a different specification", name)
		}
		return prev, nil
	}
	return f, nil
}

// family is one named metric: its declaration plus the label series.
type family struct {
	name    string
	help    string
	typ     string
	buckets []float64
	series  sync.Map // rendered label set ("" when unlabeled) -> *series
}

// seriesFor resolves (or creates) the series for a label pair list. The key
// is the canonical rendered form — labels sorted by name, values escaped —
// so two calls with the same pairs in different orders address one series.
func (f *family) seriesFor(pairs []string) (*series, error) {
	key, err := renderLabelKey(pairs)
	if err != nil {
		return nil, fmt.Errorf("metrics: %s: %w", f.name, err)
	}
	if v, ok := f.series.Load(key); ok {
		return v.(*series), nil
	}
	s := newSeries(key, len(f.buckets))
	v, loaded := f.series.LoadOrStore(key, s)
	if loaded {
		return v.(*series), nil
	}
	return s, nil
}

// mustSeries is seriesFor with the assembly-time posture: malformed label
// pairs (odd length, bad name, duplicate key) are a programming error, and
// the panic surfaces in tests rather than corrupting the exposition.
func (f *family) mustSeries(pairs []string) *series {
	s, err := f.seriesFor(pairs)
	if err != nil {
		panic(err)
	}
	return s
}

// series is one label combination's cells. Counters/gauges share the value
// cell; histograms add the observation count, sum and per-bucket counts.
type series struct {
	key     string
	value   atomicFloat   // counter total / gauge level
	count   atomic.Uint64 // histogram observations
	sum     atomicFloat   // histogram observation sum
	buckets []atomic.Uint64
}

func newSeries(key string, nBuckets int) *series {
	s := &series{key: key}
	if nBuckets > 0 {
		s.buckets = make([]atomic.Uint64, nBuckets)
	}
	return s
}

// atomicFloat is a lock-free float64 cell. Add spins on a CAS loop (the same
// technique prometheus/client_golang uses for its synchronized floats);
// Set/Load are plain atomic word operations. NaN never enters through the
// public API (counters add non-negative deltas, gauges set finite values,
// histograms observe finite durations).
type atomicFloat struct{ bits atomic.Uint64 }

func (f *atomicFloat) Add(d float64) {
	for {
		old := f.bits.Load()
		next := math.Float64bits(math.Float64frombits(old) + d)
		if f.bits.CompareAndSwap(old, next) {
			return
		}
	}
}

func (f *atomicFloat) Set(v float64) { f.bits.Store(math.Float64bits(v)) }
func (f *atomicFloat) Load() float64 { return math.Float64frombits(f.bits.Load()) }

// Counter is a monotonically increasing total per label combination.
type Counter struct{ f *family }

// Inc adds 1 to the series addressed by the alternating key/value pairs.
func (c *Counter) Inc(pairs ...string) { c.Add(1, pairs...) }

// Add increases the series by a non-negative delta. A delta of 0 still
// creates the series — the pre-seeding trick that keeps a family visible in
// Format before its first real event.
func (c *Counter) Add(delta float64, pairs ...string) {
	if delta < 0 {
		panic(fmt.Errorf("metrics: %s: counter Add(%g): counters are monotone", c.f.name, delta))
	}
	c.f.mustSeries(pairs).value.Add(delta)
}

// Gauge is a level that may rise and fall per label combination.
type Gauge struct{ f *family }

// Set replaces the series value.
func (g *Gauge) Set(v float64, pairs ...string) { g.f.mustSeries(pairs).value.Set(v) }

// Add shifts the series value by delta (the in-flight gauge's +/-1).
func (g *Gauge) Add(delta float64, pairs ...string) { g.f.mustSeries(pairs).value.Add(delta) }

// Histogram accumulates observations into bucket counts plus sum and count.
type Histogram struct{ f *family }

// Observe records v into the series addressed by the pairs. Only the ONE
// bucket whose bound first covers v is incremented — the cumulative form the
// exposition requires is computed at Format time, which keeps the hot path
// at a single atomic add for the common single-bucket case.
func (h *Histogram) Observe(v float64, pairs ...string) {
	s := h.f.mustSeries(pairs)
	s.count.Add(1)
	s.sum.Add(v)
	for i, ub := range h.f.buckets {
		if v <= ub {
			s.buckets[i].Add(1)
			return
		}
	}
	// Above every finite bound: only the implicit +Inf bucket grows, and it
	// is count at render time.
}

// Format renders the whole registry in the Prometheus text exposition
// format (version 0.0.4). Families are emitted sorted by name and series
// sorted by label set, so the output is byte-stable for a given state —
// tests can golden it and operators can diff two scrapes.
func (r *Registry) Format() string {
	var fams []*family
	r.families.Range(func(_, v any) bool {
		fams = append(fams, v.(*family))
		return true
	})
	sort.Slice(fams, func(i, j int) bool { return fams[i].name < fams[j].name })

	var b strings.Builder
	for _, f := range fams {
		b.WriteString("# HELP ")
		b.WriteString(f.name)
		b.WriteByte(' ')
		b.WriteString(escapeHelp(f.help))
		b.WriteByte('\n')
		b.WriteString("# TYPE ")
		b.WriteString(f.name)
		b.WriteByte(' ')
		b.WriteString(f.typ)
		b.WriteByte('\n')

		var rows []*series
		f.series.Range(func(_, v any) bool {
			rows = append(rows, v.(*series))
			return true
		})
		sort.Slice(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
		for _, s := range rows {
			f.renderSeries(&b, s)
		}
	}
	return b.String()
}

// renderSeries appends one series' sample lines (three lines per histogram
// series plus one per bucket).
func (f *family) renderSeries(b *strings.Builder, s *series) {
	switch f.typ {
	case TypeHistogram:
		// Cumulative bucket counts: the stored per-bucket deltas are
		// prefix-summed here, then the implicit +Inf row closes at the
		// observation count.
		var running uint64
		for i, ub := range f.buckets {
			running += s.buckets[i].Load()
			fmt.Fprintf(b, "%s_bucket%s %d\n", f.name, withExtraLabel(s.key, "le", formatFloat(ub)), running)
		}
		fmt.Fprintf(b, "%s_bucket%s %d\n", f.name, withExtraLabel(s.key, "le", "+Inf"), s.count.Load())
		fmt.Fprintf(b, "%s_sum%s %s\n", f.name, s.key, formatFloat(s.sum.Load()))
		fmt.Fprintf(b, "%s_count%s %d\n", f.name, s.key, s.count.Load())
	default:
		fmt.Fprintf(b, "%s%s %s\n", f.name, s.key, formatFloat(s.value.Load()))
	}
}

// renderLabelKey canonicalizes alternating key/value pairs into the rendered
// label set: `{"a"="1","b"="2"}` with labels sorted by name and values
// escaped, or "" when there are no labels. Odd pair counts, invalid label
// names and duplicate names are rejected — a malformed series key could
// never be addressed twice.
func renderLabelKey(pairs []string) (string, error) {
	if len(pairs)%2 != 0 {
		return "", fmt.Errorf("labels must be alternating key/value pairs, got %d elements", len(pairs))
	}
	type lv struct{ name, value string }
	lvs := make([]lv, 0, len(pairs)/2)
	seen := make(map[string]bool, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		name, value := pairs[i], pairs[i+1]
		if err := validateLabelName(name); err != nil {
			return "", err
		}
		if seen[name] {
			return "", fmt.Errorf("duplicate label %q", name)
		}
		seen[name] = true
		lvs = append(lvs, lv{name, value})
	}
	if len(lvs) == 0 {
		return "", nil
	}
	sort.Slice(lvs, func(i, j int) bool { return lvs[i].name < lvs[j].name })
	var b strings.Builder
	b.WriteByte('{')
	for i, l := range lvs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(l.name)
		b.WriteString(`="`)
		b.WriteString(escapeLabelValue(l.value))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String(), nil
}

// withExtraLabel appends one more label (le) to an already-rendered label
// set, preserving the Prometheus convention of le last in histogram rows.
func withExtraLabel(rendered, name, value string) string {
	if rendered == "" {
		return "{" + name + `="` + value + `"}`
	}
	return rendered[:len(rendered)-1] + "," + name + `="` + value + `"}`
}

// escapeHelp escapes a HELP text: backslash and newline (the two characters
// the text format reserves in HELP).
func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// escapeLabelValue escapes a label value: backslash, double quote and
// newline.
func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// formatFloat renders a sample value: shortest round-trip form, which keeps
// integers clean (42, not 42.0) and is accepted by every Prometheus parser.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// validateMetricName enforces the Prometheus name grammar
// [a-zA-Z_:][a-zA-Z0-9_:]*.
func validateMetricName(name string) error {
	if name == "" {
		return fmt.Errorf("metrics: metric name must not be empty")
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == ':':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return fmt.Errorf("metrics: invalid metric name %q", name)
		}
	}
	return nil
}

// validateLabelName enforces the label grammar [a-zA-Z_][a-zA-Z0-9_]*.
func validateLabelName(name string) error {
	if name == "" {
		return fmt.Errorf("label name must not be empty")
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return fmt.Errorf("invalid label name %q", name)
		}
	}
	return nil
}

// validateBuckets checks the histogram precondition: ascending, finite,
// non-empty bounds. Only histograms carry buckets.
func validateBuckets(typ string, buckets []float64) error {
	if typ != TypeHistogram {
		if len(buckets) != 0 {
			return fmt.Errorf("metrics: buckets are only valid on %s families", TypeHistogram)
		}
		return nil
	}
	if len(buckets) == 0 {
		return fmt.Errorf("metrics: histogram needs at least one bucket bound")
	}
	for i, ub := range buckets {
		if math.IsNaN(ub) || math.IsInf(ub, 0) {
			return fmt.Errorf("metrics: bucket bound %d (%s) must be finite; +Inf is implicit", i, formatFloat(ub))
		}
		if i > 0 && ub <= buckets[i-1] {
			return fmt.Errorf("metrics: bucket bounds must be strictly ascending (bound %d = %s)", i, formatFloat(ub))
		}
	}
	return nil
}

// sameBuckets reports whether two bucket slices are equal.
func sameBuckets(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
