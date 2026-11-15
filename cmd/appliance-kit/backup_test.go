// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"

	"example.org/appliance/internal/release"
)

func TestTheBackupGroupIsThereAndRunsNothing(t *testing.T) {
	var out bytes.Buffer
	root := newRoot(release.Pins{}, &out, &bytes.Buffer{})
	root.SetArgs([]string{"backup"})
	if err := root.Execute(); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !strings.Contains(out.String(), "appliance-kit backup") {
		t.Fatalf("backup printed %q, want its usage", out.String())
	}
}
