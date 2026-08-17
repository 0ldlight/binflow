package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no args prints usage", args: nil},
		{name: "long help flag", args: []string{"--help"}},
		{name: "short help flag", args: []string{"-h"}},
		{name: "help command", args: []string{"help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(tt.args, &stdout, &stderr); err != nil {
				t.Fatalf("run(%v) error = %v, want nil", tt.args, err)
			}
			if got := stdout.String(); !strings.Contains(got, "Usage:") {
				t.Errorf("stdout = %q, want it to contain usage", got)
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(--version) error = %v, want nil", err)
	}
	if got := stdout.String(); !strings.Contains(got, "binflow-server ") {
		t.Errorf("stdout = %q, want it to contain the version banner", got)
	}
}

func TestRunServePlaceholder(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"serve", "-c", "binflow.yaml"}, &stdout, &stderr)
	if !errors.Is(err, errNotImplemented) {
		t.Fatalf("run(serve) error = %v, want notImplemented", err)
	}
	if got := stderr.String(); !strings.Contains(got, "config=binflow.yaml") {
		t.Errorf("stderr = %q, want it to echo the config path", got)
	}
}

func TestRunGCPlaceholder(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"gc"}, &stdout, &stderr)
	if !errors.Is(err, errNotImplemented) {
		t.Fatalf("run(gc) error = %v, want notImplemented", err)
	}
	if got := stderr.String(); !strings.Contains(got, "apply=false") {
		t.Errorf("stderr = %q, want it to default to dry-run", got)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"frobnicate"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("run(frobnicate) error = %v, want unknown command", err)
	}
}
