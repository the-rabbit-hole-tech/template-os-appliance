// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"path/filepath"
	"testing"

	"example.org/appliance/internal/verify"
	"example.org/appliance/test/kit/fixtures"
)

func TestPushPublishesTheArtifactWithItsSignature(t *testing.T) {
	reg := fixtures.StartRegistry(t)
	dir, _ := fixtures.Build(t, fixtures.Options{})
	ref := reg.Ref("appliance-os")
	got, err := runCmd(t, "push", "--layout", dir, "--repo", ref, "--tag", fixtures.Version, "--plain-http")
	if err != nil {
		t.Fatal(err)
	}
	src := verify.Registry(ref+":"+fixtures.Version, verify.RegistryOptions{PlainHTTP: true})
	d, err := src.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != d.String() {
		t.Fatalf("push printed %s; the registry has %s", got, d)
	}
	l, err := src.Fetch(context.Background(), d, filepath.Join(t.TempDir(), "copy"))
	if err != nil {
		t.Fatal(err)
	}
	refs, err := l.Referrers(d, verify.SignatureArtifactType)
	if err != nil || len(refs) == 0 {
		t.Fatalf("the pushed artifact has no signature: %v", err)
	}
}
