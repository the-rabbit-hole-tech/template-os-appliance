// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package k0s_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"example.org/appliance/internal/productswitch"
)

func switchedOff(t *testing.T, slot, stack, state string) bool {
	t.Helper()
	b, err := os.ReadFile("k0s-interim")
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?ms)^switched_off\(\) \{\n.*?^\}\n`).Find(b)
	if fn == nil {
		t.Fatal("k0s-interim has no switched_off function")
	}
	script := "set -eu\n" + string(fn) + `if switched_off "$1" "$2" "$3"; then echo off; else echo on; fi` + "\n"
	out, err := exec.Command("sh", "-c", script, "sh", slot, stack, state).CombinedOutput() // #nosec G204 -- the repo's own script
	if err != nil {
		t.Fatalf("switched_off: %v: %s", err, out)
	}
	return string(out) == "off\n"
}

// k0s-interim leaves out a stack whose switch is off, from the state the
// admin set or else the slot's default, the same way productswitch and the
// product's progress do; a stack no switch gates always goes in.
func TestK0sInterimLeavesOutASwitchedOffStack(t *testing.T) {
	dir := t.TempDir()
	slot, state := filepath.Join(dir, "slot"), filepath.Join(dir, "platform")
	if err := os.MkdirAll(slot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slot, "switch-stacks"), []byte("extra appliance-extra off\napi appliance-api on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(state, productswitch.StateFile)
	check := func(when string, want map[string]bool) {
		t.Helper()
		var off []string
		for _, st := range []string{"appliance-extra", "appliance-api", "appliance"} {
			if switchedOff(t, slot, st, stateFile) {
				off = append(off, st)
			}
			if got := switchedOff(t, slot, st, stateFile); got != want[st] {
				t.Errorf("%s: %s off=%v", when, st, got)
			}
		}
		if go_ := productswitch.OffStacks(slot, state); !slices.Equal(go_, off) {
			t.Errorf("%s: productswitch says %v, k0s-interim %v", when, go_, off)
		}
	}
	check("the defaults", map[string]bool{"appliance-extra": true})
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte("api off\nextra on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	check("set on :8443", map[string]bool{"appliance-api": true})
}
