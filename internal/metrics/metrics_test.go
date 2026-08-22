// Table-driven tests for the registry and the text-format serializer
// (T-163 AC 3): TYPE/HELP lines, escaping, deterministic ordering, the
// histogram _bucket/_sum/_count rules, registration conflict handling and
// concurrent-update exactness (run under -race by the project's test target).

package metrics

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
)

// TestFormatCounterGaugeHistogram pins the exact exposition block for one
// family of each type — the FORMAT contract the /metrics endpoint serves.
func TestFormatCounterGaugeHistogram(t *testing.T) {
	r := NewRegistry()
	reqs, err := r.NewCounter("binflow_http_requests_total", "Total HTTP requests.")
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	inFlight, err := r.NewGauge("binflow_http_requests_in_flight", "In-flight requests.")
	if err != nil {
		t.Fatalf("NewGauge: %v", err)
	}
	dur, err := r.NewHistogram("binflow_http_request_duration_seconds", "Request latency.", []float64{0.1, 1, 10})
	if err != nil {
		t.Fatalf("NewHistogram: %v", err)
	}

	reqs.Inc("method", "GET", "status", "200")
	reqs.Add(2, "method", "GET", "status", "404")
	reqs.Inc() // unlabeled series
	inFlight.Set(3)
	dur.Observe(0.05, "method", "GET") // bucket <=0.1
	dur.Observe(2, "method", "GET")    // bucket <=10
	dur.Observe(99, "method", "GET")   // only +Inf
	dur.Observe(0.5)                   // unlabeled histogram series

	want := strings.Join([]string{
		`# HELP binflow_http_request_duration_seconds Request latency.`,
		`# TYPE binflow_http_request_duration_seconds histogram`,
		`binflow_http_request_duration_seconds_bucket{le="0.1"} 0`,
		`binflow_http_request_duration_seconds_bucket{le="1"} 1`,
		`binflow_http_request_duration_seconds_bucket{le="10"} 1`,
		`binflow_http_request_duration_seconds_bucket{le="+Inf"} 1`,
		`binflow_http_request_duration_seconds_sum 0.5`,
		`binflow_http_request_duration_seconds_count 1`,
		`binflow_http_request_duration_seconds_bucket{method="GET",le="0.1"} 1`,
		`binflow_http_request_duration_seconds_bucket{method="GET",le="1"} 1`,
		`binflow_http_request_duration_seconds_bucket{method="GET",le="10"} 2`,
		`binflow_http_request_duration_seconds_bucket{method="GET",le="+Inf"} 3`,
		`binflow_http_request_duration_seconds_sum{method="GET"} 101.05`,
		`binflow_http_request_duration_seconds_count{method="GET"} 3`,
		`# HELP binflow_http_requests_in_flight In-flight requests.`,
		`# TYPE binflow_http_requests_in_flight gauge`,
		`binflow_http_requests_in_flight 3`,
		`# HELP binflow_http_requests_total Total HTTP requests.`,
		`# TYPE binflow_http_requests_total counter`,
		`binflow_http_requests_total 1`,
		`binflow_http_requests_total{method="GET",status="200"} 1`,
		`binflow_http_requests_total{method="GET",status="404"} 2`,
		``,
	}, "\n")
	if got := r.Format(); got != want {
		t.Fatalf("Format mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestFormatEscaping: backslash, quote and newline are escaped in HELP text
// and label values per the text-format spec.
func TestFormatEscaping(t *testing.T) {
	r := NewRegistry()
	c, err := r.NewCounter("esc", `help with \ and
newline`)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	c.Inc("path", `/a"b\c
d`)
	want := strings.Join([]string{
		`# HELP esc help with \\ and\nnewline`,
		`# TYPE esc counter`,
		`esc{path="/a\"b\\c\nd"} 1`,
		``,
	}, "\n")
	if got := r.Format(); got != want {
		t.Fatalf("escaped Format mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestFormatEmptyRegistryAndZeroSeries: an empty registry formats to the
// empty string; a registered family with no series still carries its
// HELP/TYPE declaration (FR-61-AC1 wants the names present before traffic).
func TestFormatEmptyRegistryAndZeroSeries(t *testing.T) {
	if got := NewRegistry().Format(); got != "" {
		t.Fatalf("empty registry Format = %q, want %q", got, "")
	}
	r := NewRegistry()
	if _, err := r.NewGauge("never_set", "A gauge nobody sets."); err != nil {
		t.Fatalf("NewGauge: %v", err)
	}
	want := "# HELP never_set A gauge nobody sets.\n# TYPE never_set gauge\n"
	if got := r.Format(); got != want {
		t.Fatalf("zero-series Format = %q, want %q", got, want)
	}
}

// TestLabelPairOrderCanonicalization: the same pairs in different orders
// address ONE series (the canonical key sorts by label name).
func TestLabelPairOrderCanonicalization(t *testing.T) {
	r := NewRegistry()
	c, err := r.NewCounter("ordered", "help")
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	c.Inc("status", "200", "method", "GET")
	c.Inc("method", "GET", "status", "200")
	want := `ordered{method="GET",status="200"} 2` + "\n"
	if got := r.Format(); !strings.Contains(got, want) {
		t.Fatalf("Format = %q, want it to contain %q", got, want)
	}
}

// TestRegistrationConflict: same spec twice is idempotent; a divergent help
// or type, an invalid name and invalid buckets are registration errors.
func TestRegistrationConflict(t *testing.T) {
	r := NewRegistry()
	c1, err := r.NewCounter("dup", "help text")
	if err != nil {
		t.Fatalf("first NewCounter: %v", err)
	}
	c2, err := r.NewCounter("dup", "help text")
	if err != nil {
		t.Fatalf("idempotent NewCounter: %v", err)
	}
	if c1.f != c2.f {
		t.Fatal("re-registration returned a different family for the same spec")
	}

	cases := []struct {
		name     string
		register func(*Registry) error
	}{
		{"different help", func(r *Registry) error {
			_, err := r.NewCounter("dup", "other help")
			return err
		}},
		{"different type", func(r *Registry) error {
			_, err := r.NewGauge("dup", "help text")
			return err
		}},
		{"invalid metric name", func(r *Registry) error {
			_, err := r.NewCounter("1bad-name", "help")
			return err
		}},
		{"empty metric name", func(r *Registry) error {
			_, err := r.NewCounter("", "help")
			return err
		}},
		{"unsorted buckets", func(r *Registry) error {
			_, err := r.NewHistogram("h1", "help", []float64{1, 0.5})
			return err
		}},
		{"inf bucket bound", func(r *Registry) error {
			_, err := r.NewHistogram("h2", "help", []float64{0.5, inf()})
			return err
		}},
		{"empty buckets", func(r *Registry) error {
			_, err := r.NewHistogram("h3", "help", nil)
			return err
		}},
		{"buckets on counter", func(r *Registry) error {
			_, err := r.NewHistogram("dup", "help text", []float64{1}) // type conflict doubles as bucket misuse
			return err
		}},
		{"gauge with _total suffix", func(r *Registry) error {
			_, err := r.NewGauge("binflow_storage_blobs_total", "help")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.register(r); err == nil {
				t.Fatal("registration succeeded, want error")
			}
		})
	}
}

// TestNamingConventionTotalSuffix (T-197 / D5): the `_total` suffix is
// reserved for counters — gauge/histogram declarations carrying it are
// rejected at registration, while counters with the suffix (and every
// suffix-free non-counter name) register fine and re-register idempotently.
func TestNamingConventionTotalSuffix(t *testing.T) {
	cases := []struct {
		name     string
		wantErr  bool
		register func(*Registry) error
	}{
		{"gauge with total", true, func(r *Registry) error {
			_, err := r.NewGauge("binflow_storage_blobs_total", "help")
			return err
		}},
		{"histogram with total", true, func(r *Registry) error {
			_, err := r.NewHistogram("binflow_replication_latency_seconds_total", "help", []float64{0.5, 1})
			return err
		}},
		{"gauge without total", false, func(r *Registry) error {
			_, err := r.NewGauge("binflow_storage_blobs", "help")
			return err
		}},
		{"counter with total", false, func(r *Registry) error {
			_, err := r.NewCounter("binflow_auth_logins_total", "help")
			return err
		}},
		{"counter with total re-registered", false, func(r *Registry) error {
			if _, err := r.NewCounter("binflow_auth_logins_total", "help"); err != nil {
				return err
			}
			_, err := r.NewCounter("binflow_auth_logins_total", "help")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.register(NewRegistry())
			if tc.wantErr && err == nil {
				t.Fatal("registration succeeded, want naming-convention rejection")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("registration rejected a valid name: %v", err)
			}
			if tc.wantErr && err != nil && !strings.Contains(err.Error(), "_total") {
				t.Fatalf("error does not name the convention: %v", err)
			}
		})
	}
}

// TestMalformedLabelUse: odd pair counts, invalid and duplicate label names
// panic at use time (assembly bugs surface in tests, never corrupt output).
func TestMalformedLabelUse(t *testing.T) {
	r := NewRegistry()
	c, err := r.NewCounter("m", "help")
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	for name, use := range map[string]func(){
		"odd pair count":   func() { c.Inc("method") },
		"invalid name":     func() { c.Inc("bad-name", "GET") },
		"empty name":       func() { c.Inc("", "GET") },
		"duplicate name":   func() { c.Inc("method", "GET", "method", "POST") },
		"negative counter": func() { c.Add(-1) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("use succeeded, want panic")
				}
			}()
			use()
		})
	}
}

// TestConcurrentUpdatesExactTotals: concurrent Inc/Add/Observe/Set from many
// goroutines must land every delta exactly once (verified with -race).
func TestConcurrentUpdatesExactTotals(t *testing.T) {
	r := NewRegistry()
	c, _ := r.NewCounter("cc", "help")
	g, _ := r.NewGauge("gg", "help")
	h, _ := r.NewHistogram("hh", "help", []float64{0.5, 1})

	const workers, perWorker = 8, 500
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				c.Inc("worker", fmt.Sprint(w%2))
				g.Add(1)
				h.Observe(0.25)
			}
		}(w)
	}
	wg.Wait()

	out := r.Format()
	for _, want := range []string{
		fmt.Sprintf("cc{worker=\"0\"} %d", 4*perWorker),
		fmt.Sprintf("cc{worker=\"1\"} %d", 4*perWorker),
		fmt.Sprintf("gg %d", workers*perWorker),
		fmt.Sprintf("hh_bucket{le=\"0.5\"} %d", workers*perWorker),
		fmt.Sprintf("hh_count %d", workers*perWorker),
		fmt.Sprintf("hh_sum %s", formatFloat(0.25*float64(workers*perWorker))),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

// inf returns positive infinity (kept out of the table literals for
// readability).
func inf() float64 { return math.Inf(1) }
