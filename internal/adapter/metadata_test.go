package adapter

import "testing"

// stubProvider is a minimal MetadataProvider for registry tests.
type stubProvider struct {
	proto string
}

func (s *stubProvider) Protocol() string                  { return s.proto }
func (s *stubProvider) Classify(string) MetadataKind      { return KindContent }
func (s *stubProvider) PackageName(string) (string, bool) { return "", false }
func (s *stubProvider) Versions() VersionComparator       { return nil }

func TestMetadataRegistry(t *testing.T) {
	withFreshRegistry(t)
	a := &stubProvider{proto: "maven"}
	b := &stubProvider{proto: "npm"}
	RegisterMetadata(a)
	RegisterMetadata(b)

	if p, ok := ForProtocol("maven"); !ok || p != a {
		t.Fatalf("ForProtocol(maven) = %v %v", p, ok)
	}
	if p, ok := ForProtocol("npm"); !ok || p != b {
		t.Fatalf("ForProtocol(npm) = %v %v", p, ok)
	}
	if _, ok := ForProtocol("generic"); ok {
		t.Fatal("ForProtocol(generic) must miss: no provider registered")
	}
	got := MetadataProtocols()
	if len(got) != 2 || got[0] != "maven" || got[1] != "npm" {
		t.Fatalf("MetadataProtocols() = %v, want [maven npm]", got)
	}
}

func TestRegisterMetadataPanics(t *testing.T) {
	withFreshRegistry(t)
	for _, bad := range []struct {
		name string
		reg  func()
	}{
		{"nil provider", func() { RegisterMetadata(nil) }},
		{"empty protocol", func() { RegisterMetadata(&stubProvider{proto: ""}) }},
		{"duplicate protocol", func() {
			RegisterMetadata(&stubProvider{proto: "pypi"})
			RegisterMetadata(&stubProvider{proto: "pypi"})
		}},
	} {
		t.Run(bad.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s must panic at startup", bad.name)
				}
			}()
			bad.reg()
		})
	}
}
