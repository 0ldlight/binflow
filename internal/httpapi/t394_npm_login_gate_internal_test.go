package httpapi

import "testing"

// TestT394IsCouchUserDocument pins the K60-1 exemption predicate itself
// (internal test: isCouchUserDocument is unexported), table driven on the
// escaped repo-relative tail — the grammar mirror of the npm adapter's
// parseServiceRoute user arms. The strict rows are the leak guards: every
// spelling outside the two couch user-document shapes must answer false so
// the write gate keeps standing for it.
func TestT394IsCouchUserDocument(t *testing.T) {
	tests := []struct {
		rel  string
		want bool
	}{
		{"-/user/org.couchdb.user:alice", true},
		{"-/user/org.couchdb.user:alice/", true},                 // one trailing slash tolerated (parseRoute posture)
		{"-/user/org.couchdb.user:org.couchdb.user:weird", true}, // the id is opaque past the prefix
		{"-/user/org.couchdb.user:alic%2Fx", true},               // escaped name stays one segment
		{"-/user/org.couchdb.user:alice/-rev/2-b0b", true},       // K60-6 retry spelling
		{"-/user/org.couchdb.user:alice/-rev/2-b0b/", true},

		{"-/user/org.couchdb.user:alice/-rev/", false},    // empty rev
		{"-/user/org.couchdb.user:alice/-rev", false},     // dangling -rev
		{"-/user/org.couchdb.user:", false},               // empty name
		{"-/user/notcouch:alice", false},                  // non-couch id
		{"-/user/org.couchdb.user", false},                // bare prefix, no colon segment
		{"-/user/org.couchdb.user:alice/stranger", false}, // stranger tail
		{"-/user/org.couchdb.user:alice/-rev/2/extra", false},
		{"-/whoami", false},
		{"-/ping", false},
		{"-/package/x/dist-tags", false},
		{"-/v1/login", false},
		{"some-pkg", false},
		{"some-pkg/-rev/1", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isCouchUserDocument(tc.rel); got != tc.want {
			t.Errorf("isCouchUserDocument(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}
