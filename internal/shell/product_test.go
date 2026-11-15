// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package shell_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"example.org/appliance/internal/codes"
	"example.org/appliance/internal/productinfo"
	"example.org/appliance/internal/shell"
)

var appliance = productinfo.Info{Name: "appliance", Title: "Appliance", Version: "0.2.0"}

func runProduct(t *testing.T, p productinfo.Info, line string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	e := &shell.Env{Origin: shell.OriginSSH, Backend: &shell.Services{}, In: strings.NewReader(""), Out: &out, Err: &errb, Product: p}
	err := shell.Run(context.Background(), e, line)
	return out.String(), errb.String(), err
}

// runProductIn is runProduct with stdin lines queued up for a command that
// reads a confirm and a code (reset, reinstall).
func runProductIn(t *testing.T, p productinfo.Info, in, line string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	e := &shell.Env{Origin: shell.OriginSSH, Backend: &shell.Services{}, In: strings.NewReader(in), Out: &out, Err: &errb, Product: p}
	err := shell.Run(context.Background(), e, line)
	return out.String(), errb.String(), err
}

// With no product installed, the base shell offers nothing of a product's:
// no reset command, and nothing in help mentions it.
func TestTheBaseShellHasNoProductCommands(t *testing.T) {
	for _, line := range []string{"reset", "appliance reset"} {
		_, _, err := runProduct(t, productinfo.Info{}, line)
		assertCode(t, err, "SHELL_UNKNOWN")
	}
	out, _, err := runProduct(t, productinfo.Info{}, "help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "appliance reset") || strings.Contains(out, "(the installed product)") {
		t.Fatalf("base help mentions the product:\n%s", out)
	}
	for _, o := range []shell.Origin{shell.OriginSSH, shell.OriginConsole} {
		for _, n := range shell.NamesFor(o, productinfo.Info{}) {
			if n == "reset" || n == "appliance reset" {
				t.Errorf("%s offers %q with no product", o, n)
			}
		}
	}
	if got := shell.CompleteFor(shell.OriginSSH, productinfo.Info{}, "re"); got != "re" {
		t.Fatalf("completes %q with no product", got)
	}
}

// An installed product's commands sit in a group named after it.
func TestAnInstalledProductAddsItsOwnGroup(t *testing.T) {
	_, stderr, err := runProductIn(t, appliance, "appliance\n000000\n", "appliance reset")
	if !codes.Is(err, codes.NotAvailable) || strings.Contains(stderr, "Not available in this release") || !strings.Contains(stderr, "unavailable") {
		t.Fatalf("appliance reset: %v %q", err, stderr)
	}
	_, _, err = runProduct(t, appliance, "reset")
	assertCode(t, err, "SHELL_UNKNOWN")
	out, _, err := runProduct(t, appliance, "help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "appliance") || !strings.Contains(out, "Appliance") {
		t.Fatalf("help doesn't list the product group:\n%s", out)
	}
	out, _, err = runProduct(t, appliance, "help appliance")
	if err != nil || !strings.Contains(out, "reset") {
		t.Fatalf("help appliance: %v\n%s", err, out)
	}
	names := shell.NamesFor(shell.OriginSSH, appliance)
	if !slices.Contains(names, "appliance reset") || slices.Contains(names, "reset") {
		t.Fatalf("names %v", names)
	}
	for typed, want := range map[string]string{"appl": "appliance ", "appliance res": "appliance reset ", "appliance rei": "appliance reinstall ", "re": "re"} {
		if got := shell.CompleteFor(shell.OriginSSH, appliance, typed); got != want {
			t.Errorf("CompleteFor(%q) = %q, want %q", typed, got, want)
		}
	}
}

// The base names stay the base: Names and Complete never include a
// product's commands.
func TestTheBaseNamesLeaveTheProductOut(t *testing.T) {
	if slices.ContainsFunc(shell.Names(shell.OriginSSH), func(n string) bool { return n == "reset" || n == "appliance reset" }) {
		t.Fatal("the base names list reset")
	}
	if !slices.Equal(shell.Names(shell.OriginSSH), shell.NamesFor(shell.OriginSSH, productinfo.Info{})) {
		t.Fatal("Names differs from NamesFor with no product")
	}
}
