package helm

// The Enforce Layout policy's wire matrix (helm.md section 4.3 / S5): the
// two repository switches, judged on the .tgz upload hook, refusing with
// the VERBATIM policy wordings before any byte lands.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

const (
	cfgEnforceNameVersion = `{"forceMetadataNameVersion":true}`
	cfgEnforceNoDuplicate = `{"forceNonDuplicateChart":true}`
	cfgEnforceBoth        = `{"forceMetadataNameVersion":true,"forceNonDuplicateChart":true}`
)

// TestEnforceLayoutMatrix drives every 403 branch plus the pass-through
// arms (switches off, non-.tgz paths, first-of-name uploads).
func TestEnforceLayoutMatrix(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		uploadPath string
		chartYAML  string
		preSeed    bool // upload one mychart-0.1.0.tgz before the probe
		wantStatus int
		wantBody   string
	}{
		{
			name:       "off accepts mismatched filename",
			config:     "{}",
			uploadPath: "wrong-name.tgz",
			chartYAML:  defaultChartYAML("mychart", "0.1.0"),
			wantStatus: http.StatusCreated,
		},
		{
			name:       "nameversion refuses mismatched filename",
			config:     cfgEnforceNameVersion,
			uploadPath: "wrong-name.tgz",
			chartYAML:  defaultChartYAML("mychart", "0.1.0"),
			wantStatus: http.StatusForbidden,
			wantBody:   "This action is prevented due to the Enforce Layout Policy, the metadata of the package helm-enf/wrong-name.tgz could not be read or is malformed.",
		},
		{
			name:       "nameversion refuses non-semver version",
			config:     cfgEnforceNameVersion,
			uploadPath: "mychart-1.0.tgz",
			chartYAML:  defaultChartYAML("mychart", "1.0"),
			wantStatus: http.StatusForbidden,
			wantBody:   "This action is prevented due to the Enforce Layout Policy, the metadata of the package helm-enf/mychart-1.0.tgz could not be read or is malformed.",
		},
		{
			name:       "nameversion refuses unparsable archive",
			config:     cfgEnforceNameVersion,
			uploadPath: "mychart-0.1.0.tgz",
			chartYAML:  "", // garbage body
			wantStatus: http.StatusForbidden,
			wantBody:   "This action is prevented due to the Enforce Layout Policy, the metadata of the package helm-enf/mychart-0.1.0.tgz could not be read or is malformed.",
		},
		{
			name:       "nameversion accepts the canonical filename",
			config:     cfgEnforceNameVersion,
			uploadPath: "mychart-0.1.0.tgz",
			chartYAML:  defaultChartYAML("mychart", "0.1.0"),
			wantStatus: http.StatusCreated,
		},
		{
			name:       "duplicate refuses a second path for the same name+version",
			config:     cfgEnforceNoDuplicate,
			uploadPath: "copy/mychart-0.1.0.tgz",
			chartYAML:  defaultChartYAML("mychart", "0.1.0"),
			preSeed:    true,
			wantStatus: http.StatusForbidden,
			wantBody:   "This action is prevented due to the Enforce Layout Policy, a package with the same name and version mychart-0.1.0 already exists in the repository.",
		},
		{
			name:       "duplicate tolerates a new version",
			config:     cfgEnforceNoDuplicate,
			uploadPath: "mychart-0.2.0.tgz",
			chartYAML:  defaultChartYAML("mychart", "0.2.0"),
			preSeed:    true,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "duplicate off tolerates the same version at another path",
			config:     "{}",
			uploadPath: "copy/mychart-0.1.0.tgz",
			chartYAML:  defaultChartYAML("mychart", "0.1.0"),
			preSeed:    true,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "both switches: the filename arm refuses first",
			config:     cfgEnforceBoth,
			uploadPath: "wrong-name.tgz",
			chartYAML:  defaultChartYAML("mychart", "0.1.0"),
			preSeed:    true,
			wantStatus: http.StatusForbidden,
			wantBody:   "This action is prevented due to the Enforce Layout Policy, the metadata of the package helm-enf/wrong-name.tgz could not be read or is malformed.",
		},
		{
			name:       "both switches: canonical filename, duplicate path",
			config:     cfgEnforceBoth,
			uploadPath: "subdir/mychart-0.1.0.tgz",
			chartYAML:  defaultChartYAML("mychart", "0.1.0"),
			preSeed:    true,
			wantStatus: http.StatusForbidden,
			wantBody:   "This action is prevented due to the Enforce Layout Policy, a package with the same name and version mychart-0.1.0 already exists in the repository.",
		},
		{
			name:       "enforce never judges a .prov upload",
			config:     cfgEnforceBoth,
			uploadPath: "anything.prov",
			chartYAML:  "", // prov is a plain file; the body is not a chart
			wantStatus: http.StatusCreated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStack(t)
			s.seedRepo(t, "helm-enf", repo.TypeLocal, tt.config)
			if tt.preSeed {
				chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "0.1.0"), nil)
				if status, body, _ := s.put("/binflow/helm-enf/mychart-0.1.0.tgz", chart, nil); status != http.StatusCreated {
					t.Fatalf("pre-seed PUT = (%d, %s)", status, body)
				}
			}
			var body []byte
			if strings.HasSuffix(tt.uploadPath, ".prov") {
				body = []byte("-----BEGIN PGP SIGNATURE-----\nprov\n-----END PGP SIGNATURE-----\n")
			} else if tt.chartYAML == "" {
				body = []byte("this is not a tgz at all")
			} else {
				body = fixtureChart(t, "mychart", tt.chartYAML, nil)
			}
			status, respBody, _ := s.put("/binflow/helm-enf/"+tt.uploadPath, body, nil)
			if status != tt.wantStatus {
				t.Fatalf("PUT %s = (%d, %s), want %d", tt.uploadPath, status, respBody, tt.wantStatus)
			}
			if tt.wantBody != "" && respBody != tt.wantBody+"\n" {
				t.Fatalf("403 body = %q, want the verbatim policy wording %q", respBody, tt.wantBody)
			}
			// A refused upload leaves nothing behind (the hook fires
			// before the landing).
			if tt.wantStatus == http.StatusForbidden {
				if st, _, _ := s.get("/binflow/helm-enf/" + tt.uploadPath); st != http.StatusNotFound {
					t.Fatalf("refused upload left a node behind: GET = %d, want 404", st)
				}
			}
		})
	}
}

// TestEnforceNonTgzPathNeverJudged: a plain file under both switches
// stores fine (the policy is a .tgz-hook concern only).
func TestEnforceNonTgzPathNeverJudged(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-enf", repo.TypeLocal, cfgEnforceBoth)
	if status, body, _ := s.put("/binflow/helm-enf/README.md", []byte("notes"), nil); status != http.StatusCreated {
		t.Fatalf("plain PUT = (%d, %s), want 201", status, body)
	}
}
