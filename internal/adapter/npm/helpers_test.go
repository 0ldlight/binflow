package npm

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// withTestPrincipal boxes p into the adapter context seam the way httpapi's
// dispatch does.
func withTestPrincipal(ctx context.Context, p *Principal) context.Context {
	return adapter.WithPrincipal(ctx, p)
}

func base64RawURL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func base64Std(b []byte) string    { return base64.StdEncoding.EncodeToString(b) }
func hexEncode(b []byte) string    { return hex.EncodeToString(b) }

// mustJSON renders v (test fixture builder).
func mustJSON(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(out)
}
