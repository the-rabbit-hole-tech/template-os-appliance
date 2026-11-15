// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

//go:build image

// Package reduced_test is the image suite's run without Secure Boot and
// without a TPM: the Secure Boot choice, the typed "no secure boot", first
// boot in reduced-protection mode with key-file custody, and the same on
// the next boot without asking again.
package reduced_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"example.org/appliance/internal/consoleui/screens"
	"example.org/appliance/test/image/harness"
)

func TestNoSecureBootNoTPMBootsReduced(t *testing.T) {
	img, keys := os.Getenv("APPLIANCE_IMAGE"), os.Getenv("APPLIANCE_KEYS")
	harness.Need(t, nil, img, keys)
	cases := map[string]struct {
		sb harness.SecureBoot
		// setupMode: Enter alone keeps Secure Boot; outside it, Enter alone
		// does nothing.
		setupMode bool
	}{
		"setup-mode": {harness.Off, true},
		// A VMware VM with Secure Boot off in its settings: vendor keys
		// present, nothing enforced.
		"keys-enrolled-sb-off": {harness.OffWithKeys, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			vm := harness.Boot(t, harness.Options{SecureBoot: c.sb, Keys: keys, Disks: []harness.Disk{{Image: img}}})
			vm.Expect(`appliance-init: phase=enrol`, 5*time.Minute)
			vm.Expect(`type "`+screens.TypedNoSecureBoot+`" and press Enter`, time.Minute)
			if !c.setupMode {
				vm.Type("\r")
				deadline := time.Now().Add(time.Minute)
				for strings.Count(vm.Console(), "Enter alone does nothing") < 2 {
					if time.Now().After(deadline) {
						t.Fatal("Enter alone didn't show the choice again")
					}
					time.Sleep(500 * time.Millisecond)
				}
				if strings.Contains(vm.Console(), "Secure Boot choice on") {
					t.Fatal("Enter alone chose Secure Boot outside Setup Mode")
				}
			}
			vm.Type(screens.TypedNoSecureBoot + "\r")
			vm.Expect(`Secure Boot choice off recorded on the ESP`, time.Minute)
			vm.Expect(`This box has no TPM`, time.Minute)
			vm.Type("\r")
			vm.Expect(`custody fixed \(key file\); state and backup formatted and mounted`, 3*time.Minute)
			vm.Expect(`services: entering phase phase=firstboot`, time.Minute)
			vm.Stop()

			next := harness.Boot(t, harness.Options{SecureBoot: c.sb, Keys: keys, Disks: []harness.Disk{{Image: vm.Disk(0)}}})
			next.Expect(`appliance-init: phase=firstboot protection=reduced \(Secure Boot off\)`, 5*time.Minute)
			next.Expect(`services: entering phase phase=firstboot`, time.Minute)
			if c := next.Console(); strings.Contains(c, "phase=enrol") || strings.Contains(c, "This box has no TPM") {
				t.Fatal("the next boot asked for the Secure Boot or custody choice again")
			}
		})
	}
}
