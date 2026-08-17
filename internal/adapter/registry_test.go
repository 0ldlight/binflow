package adapter

import (
	"net/http"
	"testing"
)

// stubHandler is a minimal SPI implementation for registry tests.
type stubHandler struct {
	proto  string
	types  []string
	secret int // distinguish two stubs with the same proto behavior
}

func (s *stubHandler) Protocol() string { return s.proto }
func (s *stubHandler) RepoTypes() []string {
	if s.types == nil {
		return []string{"local"}
	}
	return s.types
}
func (s *stubHandler) Layout(r *http.Request) (string, string, error) { return Layout(r) }
func (s *stubHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// withFreshRegistry swaps the process registry for an empty one and
// restores it after the test (the registry is package state by design;
// tests must stay hermetic around it).
func withFreshRegistry(t *testing.T) {
	t.Helper()
	saved := reg
	reg = &registry{byProto: map[string]Handler{}, byType: map[string]Handler{}}
	t.Cleanup(func() { reg = saved })
}

func TestRegisterAndAll(t *testing.T) {
	withFreshRegistry(t)
	a := &stubHandler{proto: "generic", secret: 1}
	b := &stubHandler{proto: "docker", types: []string{"remote", "virtual"}, secret: 2}
	Register(a)
	Register(b)

	all := All()
	if len(all) != 2 {
		t.Fatalf("All() = %d handlers, want 2", len(all))
	}
	if all[0].Protocol() != "generic" || all[1].Protocol() != "docker" {
		t.Fatalf("All() order = [%s, %s], want [generic, docker]", all[0].Protocol(), all[1].Protocol())
	}
	if h, ok := ForRepoType("generic"); !ok || h.Protocol() != "generic" {
		t.Fatalf("ForRepoType(generic) = %v %v", h, ok)
	}
	if h, ok := ForRepoType("remote"); !ok || h.Protocol() != "docker" {
		t.Fatalf("ForRepoType(remote) = %v %v", h, ok)
	}
	if _, ok := ForRepoType("maven"); ok {
		t.Fatal("ForRepoType(maven) should miss in this test's registry")
	}
}

func TestRegisterDuplicateProtocolPanics(t *testing.T) {
	withFreshRegistry(t)
	Register(&stubHandler{proto: "generic"})
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate protocol registration must panic at startup")
		}
	}()
	Register(&stubHandler{proto: "generic", secret: 2})
}

func TestRegisterDuplicateRepoTypePanics(t *testing.T) {
	withFreshRegistry(t)
	Register(&stubHandler{proto: "generic", types: []string{"local"}})
	defer func() {
		if recover() == nil {
			t.Fatal("two handlers claiming the same repo type must panic")
		}
	}()
	Register(&stubHandler{proto: "other", types: []string{"local", "remote"}})
}

func TestRegisterNilAndEmptyPanics(t *testing.T) {
	withFreshRegistry(t)
	for _, bad := range []func(){
		func() { Register(nil) },
		func() { Register(&stubHandler{proto: ""}) },
		func() { Register(&stubHandler{proto: "x", types: []string{}}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid registration must panic")
				}
			}()
			bad()
		}()
	}
}

func TestReservedSegments(t *testing.T) {
	for _, seg := range []string{"api", "v2"} {
		if !IsReservedSegment(seg) {
			t.Fatalf("%q must be reserved", seg)
		}
	}
	for _, seg := range []string{"apis", "v22", "local", ""} {
		if IsReservedSegment(seg) {
			t.Fatalf("%q must not be reserved", seg)
		}
	}
}
