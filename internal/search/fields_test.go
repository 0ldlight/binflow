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
	// The statistics family is registered-but-unsupported, one entry per
	// aql.md §2.2 field, so rejections can name them.
	for _, name := range statFields {
		f, ok := lookupField(name)
		if !ok {
			t.Errorf("stat field %q not registered", name)
			continue
		}
		if f.Unsupported == "" || f.Domain != DomainStatistics {
			t.Errorf("stat field %q = %+v, want registered-unsupported statistics", name, f)
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
	// The 12 non-items entry domains from aql.md §2.1 (decompiled RootElement
	// set minus items) each carry a rejection hint.
	want := []string{
		"builds", "build.properties", "build.promotions", "modules",
		"module.properties", "dependencies", "artifacts", "releases",
		"release_artifacts", "statistics", "properties", "item.infos",
	}
	for _, d := range want {
		if _, ok := unsupportedDomains[d]; !ok {
			t.Errorf("domain %q missing from unsupportedDomains", d)
		}
	}
}
