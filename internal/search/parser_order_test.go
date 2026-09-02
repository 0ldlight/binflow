package search

import "testing"

// D-T421-1: JSON member order inside a criteria object is free — an
// operator object followed by more members used to die as a syntax error.
func TestParseCriteriaMemberOrderFree(t *testing.T) {
	queries := []string{
		// operator object first, scalar after (the L21 acceptance shape)
		`items.find({"repo":{"$match":"m8-perf-local*"},"type":"file"}).limit(1)`,
		// two operator objects back to back
		`items.find({"repo":{"$match":"libs-*"},"path":{"$match":"org/*"}})`,
		// operator object before a $or compound
		`items.find({"repo":{"$eq":"libs-release-local"},"$or":[{"type":"file"},{"name":{"$match":"*.jar"}}]})`,
		// scalar, operator object, scalar interleaved
		`items.find({"type":"file","repo":{"$match":"libs-*"},"name":{"$match":"*.jar"}})`,
	}
	for _, q := range queries {
		if _, err := Parse(q); err != nil {
			t.Errorf("Parse(%s) = %v, want nil (member order must be free)", q, err)
		}
	}
	// The one-comparator-one-operator rule still holds: a second $op inside
	// the SAME comparator object stays a syntax error.
	if _, err := Parse(`items.find({"repo":{"$match":"libs-*","$ne":"libs-remote"}})`); err == nil {
		t.Error("two operators inside one comparator object must stay a syntax error")
	}
}
