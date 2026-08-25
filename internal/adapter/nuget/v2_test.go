package nuget

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The v2 face: the FindPackagesById OData feed (the PRD's curl-assertion
// carrier) and the $metadata document.

// TestV2FindPackagesById: the feed lists every version ascending with the
// V2FeedPackage property set; the empty feed answers unknown ids.
func TestV2FindPackagesById(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	deps := flatDeps("Serilog", "4.0.0")
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0-beta1"} {
		pkg := buildNupkg(t, "Feed.Pkg", v, deps)
		if status, body, _ := s.put(pushPath("ng-local", "feed.pkg", v), pkg.body, nil); status != http.StatusCreated {
			t.Fatalf("push %s: %d %s", v, status, body)
		}
	}

	status, body, hdr := s.get(apiV2Path("ng-local") + "/FindPackagesById()?id='Feed.Pkg'")
	if status != http.StatusOK {
		t.Fatalf("feed status = %d, body %s", status, body)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("feed content-type = %q", ct)
	}

	var feed atomFeed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatalf("feed is not XML: %v\n%s", err, body)
	}
	if len(feed.Entries) != 3 {
		t.Fatalf("feed entries = %d, want 3 (body %s)", len(feed.Entries), body)
	}
	if feed.Entries[0].Properties.Version != "1.0.0" || feed.Entries[2].Properties.Version != "2.0.0-beta1" {
		t.Errorf("feed order = [%s %s %s], want ascending",
			feed.Entries[0].Properties.Version, feed.Entries[1].Properties.Version, feed.Entries[2].Properties.Version)
	}
	last := feed.Entries[2].Properties
	if !last.IsAbsoluteLatest || last.IsLatest {
		t.Errorf("beta entry latest flags = abs %v / stable %v, want true / false", last.IsAbsoluteLatest, last.IsLatest)
	}
	second := feed.Entries[1].Properties
	if !second.IsLatest || second.IsAbsoluteLatest {
		t.Errorf("1.1.0 latest flags = abs %v / stable %v, want false / true (the newest stable)", second.IsAbsoluteLatest, second.IsLatest)
	}
	first := feed.Entries[0].Properties
	if first.IsLatest || first.IsAbsoluteLatest {
		t.Errorf("1.0.0 latest flags = abs %v / stable %v, want false / false", first.IsAbsoluteLatest, first.IsLatest)
	}
	if first.PackageHashAlgorithm != "SHA512" || first.PackageHash == "" {
		t.Errorf("packageHash family = %q / %q", first.PackageHashAlgorithm, first.PackageHash)
	}
	if want := "Serilog:4.0.0:netstandard2.0"; first.Dependencies != want {
		t.Errorf("dependencies string = %q, want %q", first.Dependencies, want)
	}
	if !strings.Contains(feed.Entries[0].Content.Src, "/api/nuget/v3/ng-local/flatcontainer/feed.pkg/1.0.0/feed.pkg.1.0.0.nupkg") {
		t.Errorf("content src = %q, want the BinFlow flatcontainer URL", feed.Entries[0].Content.Src)
	}
	if first.Authors != "BinFlow Test" {
		t.Errorf("author = %q", first.Authors)
	}

	// The no-parens spelling and the URL-encoded quotes work; the bare
	// literal without an id is the 400 (the feed is meaningless).
	for _, variant := range []string{
		"/FindPackagesById?id=" + url.QueryEscape("'Feed.Pkg'"),
	} {
		if status, body, _ := s.get(apiV2Path("ng-local") + variant); status != http.StatusOK {
			t.Errorf("%s = %d (%s)", variant, status, firstLine(body))
		}
	}
	if status, _, _ := s.get(apiV2Path("ng-local") + "/FindPackagesById()"); status != http.StatusBadRequest {
		t.Errorf("bare FindPackagesById = %d, want 400", status)
	}

	// An unknown id answers the EMPTY feed (the collection semantics).
	status, body, _ = s.get(apiV2Path("ng-local") + "/FindPackagesById()?id='No.Such'")
	if status != http.StatusOK || !strings.Contains(body, "<feed") || strings.Contains(body, "<entry>") {
		t.Errorf("unknown id = (%d, %s…), want the empty feed", status, firstLine(body))
	}
}

// TestV2Metadata: the EDMX document parses and declares the entity set
// with the FindPackagesById import.
func TestV2Metadata(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	status, body, _ := s.get(apiV2Path("ng-local") + "/$metadata")
	if status != http.StatusOK {
		t.Fatalf("$metadata status = %d", status)
	}
	if !strings.Contains(body, `EntitySet Name="Packages"`) ||
		!strings.Contains(body, `FunctionImport Name="FindPackagesById"`) ||
		!strings.Contains(body, `EntityType Name="V2FeedPackage"`) {
		t.Errorf("$metadata misses the expected declarations:\n%s", body)
	}
	var probe struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal([]byte(body), &probe); err != nil {
		t.Errorf("$metadata is not XML: %v", err)
	}
}

// TestV2Push: the v2 publish shape lands through the same validation
// chain as v3 (one storage, two entrances).
func TestV2Push(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	pkg := buildNupkg(t, "Legacy.Pkg", "1.0.0", flatDeps("none"))
	status, body, _ := s.put(apiV2Path("ng-local")+"/legacy.pkg/1.0.0", pkg.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("v2 push status = %d, body %s", status, body)
	}
	status, body, _ = s.get(packagePath("ng-local", "legacy.pkg", "1.0.0", "nupkg"))
	if status != http.StatusOK || body != string(pkg.body) {
		t.Errorf("v2-pushed package GET = %d (len %d)", status, len(body))
	}
	// The official client spelling with the /package/ prefix.
	pkg2 := buildNupkg(t, "Legacy.Pkg", "1.1.0", flatDeps("none"))
	status, body, _ = s.put(apiV2Path("ng-local")+"/package/legacy.pkg/1.1.0", pkg2.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("v2 push (package/) status = %d, body %s", status, body)
	}
}

// atomFeed is the parsed v2 feed (the assertion surface).
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID         string       `xml:"id"`
	Title      string       `xml:"title"`
	Author     string       `xml:"author>name"`
	Content    atomContent  `xml:"content"`
	Properties v2Properties `xml:"properties"`
}

type atomContent struct {
	Src string `xml:"src,attr"`
}

type v2Properties struct {
	ID                   string `xml:"Id"`
	Version              string `xml:"Version"`
	Authors              string `xml:"Authors"`
	Dependencies         string `xml:"Dependencies"`
	PackageHash          string `xml:"PackageHash"`
	PackageHashAlgorithm string `xml:"PackageHashAlgorithm"`
	IsLatest             bool   `xml:"IsLatestVersion"`
	IsAbsoluteLatest     bool   `xml:"IsAbsoluteLatestVersion"`
}
