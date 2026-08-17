// Command binflow-server is the single BinFlow binary. Subcommands: serve
// (default) and gc. Wiring and lifecycle land in T-16; this scaffold only
// parses arguments and reports placeholder errors.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// version is the binary version reported by future /api/system/version and
// `--version`. It is a placeholder until release stamping (M5, Q4: honest
// versions, dev builds report "dev").
const version = "dev"

const usage = `binflow-server is the BinFlow artifact repository server.

Usage:

	binflow-server <command> [flags]

Commands:

	serve    Run the HTTP server (default when no command is given)
	gc       Garbage-collect unreferenced blobs (dry-run by default)

Flags for serve:

	-c string    Path to binflow.yaml (default "binflow.yaml")

Flags for gc:

	--apply    Actually delete blobs (default is a dry-run listing)

Common flags:

	--help      Show this usage text
	--version   Show the binary version
`

// errNotImplemented is returned by subcommands whose wiring is not in place yet.
var errNotImplemented = errors.New("not implemented yet (wiring lands in T-16)")

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(os.Stderr, "binflow-server: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage) //nolint:errcheck // usage printing has no fallback if it fails
		return nil
	}

	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage) //nolint:errcheck // usage printing has no fallback if it fails
		return nil
	case "--version":
		fmt.Fprintf(stdout, "binflow-server %s\n", version) //nolint:errcheck // usage printing has no fallback if it fails
		return nil
	case "serve":
		return runServe(args[1:], stderr)
	case "gc":
		return runGC(args[1:], stderr)
	default:
		return fmt.Errorf("unknown command %q, see --help for usage", args[0])
	}
}

func runServe(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "binflow.yaml", "path to binflow.yaml")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing serve flags: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q, see --help for usage", fs.Arg(0))
	}
	fmt.Fprintf(stderr, "serve: config=%s: %v\n", *configPath, errNotImplemented) //nolint:errcheck // best-effort notice before a non-zero exit
	return errNotImplemented
}

func runGC(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apply := fs.Bool("apply", false, "delete blobs instead of dry-run listing")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing gc flags: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q, see --help for usage", fs.Arg(0))
	}
	fmt.Fprintf(stderr, "gc: apply=%t: %v\n", *apply, errNotImplemented) //nolint:errcheck // best-effort notice before a non-zero exit
	return errNotImplemented
}
