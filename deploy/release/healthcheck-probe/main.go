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
	// The verdict is the status line alone; the body is drained only as
	// keep-alive hygiene for the server side, so drain/close failures are
	// reported on stderr but never change the exit code. Handling them here
	// is the whole point: `//nolint:errcheck` does not silence gosec G104,
	// and an ignored error in the container HEALTHCHECK is how a wedged
	// probe goes unnoticed (T-275, DEFECT-2).
	if _, derr := io.Copy(io.Discard, resp.Body); derr != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: drain: %v\n", derr)
	}
	if cerr := resp.Body.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: close: %v\n", cerr)
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		os.Exit(1)
	}
	os.Exit(0)
}
