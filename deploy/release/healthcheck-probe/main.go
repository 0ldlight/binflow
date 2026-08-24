// Command healthcheck-probe is the HEALTHCHECK agent of the distroless
// release image (T-132 AC G07). T-270 hoisted it from the inline heredoc of
// Dockerfile.distroless into a source file so the prebuilt multi-arch chain
// (deploy/release/build-release.sh) can cross-compile it per target
// architecture on the build host — distroless runtime stages must stay
// RUN-free to assemble without qemu/binfmt.
//
// distroless ships no shell and no wget, so the alpine variant's
// `wget http://127.0.0.1:8080/readyz` probe cannot run there; this static
// pure-Go binary performs the same unauthenticated GET and exits 0 only on
// HTTP 200.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	resp, err := http.Get("http://127.0.0.1:8080/readyz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close() //nolint:errcheck // the probe exits right after
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck // drain only
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		os.Exit(1)
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck // drain only
	os.Exit(0)
}
