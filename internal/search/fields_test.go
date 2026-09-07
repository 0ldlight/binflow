package search

import "testing"

// Registry invariants: the closed set is the honest-rejection contract
// (ADR-0043 pt 2) — these tests pin its shape so T-411 can rely on it.

func TestRegistryInvariants(t *testing.T) {
	// Every default output field (aql.md §3.3) must be registered.
	for _, name := range defaultItemOutput {
		if _, ok := lookupField(name); !ok {
			t.Errorf("default output field %q not registered", name)
		}
	}
	// The statistics family (open since T-440, aql.md §14.1): one entry
	// per §2.2 field — the column-backed trio and the constant-zero stubs
	// are usable citizens of the statistics domain, the two internal ids
	// keep the honest-unsupported refusal.
	for _, name := range statFields {
		f, ok := lookupField(name)
		if !ok {
			t.Errorf("stat field %q not registered", name)
			continue
		}
		if f.Domain != DomainStatistics {
			t.Errorf("stat field %q = %+v, want statistics domain", name, f)
		}
		internal := name == "stat.id" || name == "stat.remote_id"
		if internal != (f.Unsupported != "") {
			t.Errorf("stat field %q unsupported = %q, want the internal-ids-only refusal", name, f.Unsupported)
		}
		if internal {
			continue
		}
		if !f.Projectable || f.Unsupported != "" {
			t.Errorf("stat field %q = %+v, want projectable and supported", name, f)
		}
		if f.Sortable != !isStatStubField(f.ID) {
			t.Errorf("stat field %q sortable = %t, want true only for column-backed members", name, f.Sortable)
		}
	}
	for name, f := range fieldRegistry {
		if f.Name == "" || string(f.ID) == "" {
			t.Errorf("field %q missing Name/ID: %+v", name, f)
		}
		if f.Unsupported == "" {
			// Supported fields are projectable; exactly one (virtual_repos)
			// is output-only with an empty operator set.
			if !f.Projectable {
				t.Errorf("supported field %q is not projectable", name)
			}
			if len(f.Ops) == 0 && name != "virtual_repos" {
				t.Errorf("supported field %q has no operators", name)
			}
		} else {
			// Unsupported fields reject everywhere: no ops, no projection.
			if len(f.Ops) != 0 || f.Sortable || f.Projectable {
				t.Errorf("unsupported field %q must not be usable: %+v", name, f)
			}
		}
	}
}

func TestFieldOperatorSets(t *testing.T) {
	// aql.md §2.4: $match/$nmatch string-only; $last/$before date-only;
	// the type enum is $eq/$ne only.
	name := fieldRegistry["name"]
	if !opAllowedFor(OpMatch, name.Ops) || !opAllowedFor(OpNmatch, name.Ops) {
		t.Error("name must allow $match/$nmatch")
	}
	if opAllowedFor(OpLast, name.Ops) {
		t.Error("name must not allow $last")
	}
	created := fieldRegistry["created"]
	if !opAllowedFor(OpLast, created.Ops) || !opAllowedFor(OpBefore, created.Ops) {
		t.Error("created must allow $last/$before")
	}
	if opAllowedFor(OpMatch, created.Ops) {
		t.Error("created must not allow $match")
	}
	typ := fieldRegistry["type"]
	if len(typ.Ops) != 2 || !opAllowedFor(OpEq, typ.Ops) || !opAllowedFor(OpNe, typ.Ops) {
		t.Errorf("type ops = %v, want exactly [$eq $ne]", typ.Ops)
	}
	// Property long forms behave like string fields but are not sortable.
	for _, n := range []string{"property.key", "property.value"} {
		pf := fieldRegistry[n]
		if !opAllowedFor(OpMatch, pf.Ops) || pf.Sortable {
			t.Errorf("%s: ops/sortable wrong: %+v", n, pf)
		}
	}
}

func TestLookupDottedPaths(t *testing.T) {
	for _, name := range []string{"property.key", "property.value", "stat.downloads", "repo_path_checksum"} {
		if _, ok := lookupField(name); !ok {
			t.Errorf("lookupField(%q) failed", name)
		}
	}
	if _, ok := lookupField("name.repo"); ok {
		t.Error("lookupField(name.repo) should fail")
	}
}

func TestUnsupportedDomainsCatalog(t *testing.T) {
	// The non-items entry domains from aql.md §2.1 (decompiled RootElement
	// set minus items) each carry a rejection hint. T-511 assertion
	// inversion ⑥ (aql.md §15.3): builds/modules/dependencies LEFT the
	// catalog — the three entries query green; the remaining nine keep
	// their 400 with the flip point named (sensitive joins as the
	// field-level-domain spelling the reference carries).
	want := []string{
		"build.properties", "build.promotions",
		"module.properties", "artifacts", "releases",
		"release_artifacts", "sensitive", "statistics", "properties", "item.infos",
	}
	for _, d := range want {
		if _, ok := unsupportedDomains[d]; !ok {
			t.Errorf("domain %q missing from unsupportedDomains", d)
		}
	}
	for _, d := range []string{"builds", "modules", "dependencies"} {
		if _, ok := unsupportedDomains[d]; ok {
			t.Errorf("domain %q must not be in unsupportedDomains (T-511 opened the entry)", d)
		}
	}
}
