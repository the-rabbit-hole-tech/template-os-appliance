// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package bootcmd_test

import (
	"strings"
	"testing"

	"example.org/appliance/internal/bootcmd"
)

var root = strings.Repeat("ab", 32)

func TestParse(t *testing.T) {
	p, err := bootcmd.Parse("appliance.roothash=" + root + " appliance.hashoffset=8192 appliance.version=0.1.0 quiet console=tty0 console=ttyS0 panic=10 lockdown=integrity\x00")
	if err != nil || p.RootHash != root || p.HashOffset != 8192 || p.Version != "0.1.0" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestParseRefusals(t *testing.T) {
	for name, line := range map[string]string{
		"no root hash": "appliance.hashoffset=4096",
		"no offset":    "appliance.roothash=" + root,
		"short hash":   "appliance.roothash=abcd appliance.hashoffset=4096",
		"upper hash":   "appliance.roothash=" + strings.ToUpper(root) + " appliance.hashoffset=4096",
		"odd offset":   "appliance.roothash=" + root + " appliance.hashoffset=4000",
		"twice":        "appliance.roothash=" + root + " appliance.roothash=" + root + " appliance.hashoffset=4096",
		"negative":     "appliance.roothash=" + root + " appliance.hashoffset=-4096",
		"not a number": "appliance.roothash=" + root + " appliance.hashoffset=4k",
	} {
		if _, err := bootcmd.Parse(line); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSlotGUID(t *testing.T) {
	g, err := bootcmd.SlotGUID("00112233445566778899aabbccddeeff" + strings.Repeat("0", 32))
	if err != nil || g != "00112233-4455-6677-8899-aabbccddeeff" {
		t.Fatalf("%s %v", g, err)
	}
	if _, err := bootcmd.SlotGUID("abc"); err == nil {
		t.Fatal("short hash accepted")
	}
}
