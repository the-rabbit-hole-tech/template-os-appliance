// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const pinned = `apiVersion: appliance/v1alpha1
kind: Release
spec:
  services:
    data:
      image: registry.example.org/appliance-data
      digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  thirdParty:
    postgres:
      image: docker.io/library/postgres
      digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  tools:
    curl:
      image: docker.io/curlimages/curl
      digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
  kubernetes:
    k0s:
      images:
        - image: quay.io/k0sproject/pause
          digest: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
`

func writeRelease(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "release.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImagesListsEveryBundledImageSorted(t *testing.T) {
	var out bytes.Buffer
	if err := images([]string{"--release", writeRelease(t, pinned)}, &out); err != nil {
		t.Fatal(err)
	}
	// Sorted by the code under test, so the expected order is sorted here too:
	// where the product's registry falls among docker.io and quay.io is the
	// product's choice, not this test's.
	lines := []string{
		"docker.io/library/postgres sha256:" + strings.Repeat("a", 64),
		"registry.example.org/appliance-data sha256:" + strings.Repeat("b", 64),
		"quay.io/k0sproject/pause sha256:" + strings.Repeat("d", 64),
	}
	sort.Strings(lines)
	want := strings.Join(lines, "\n") + "\n"
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestImagesRefusesAPlaceholderDigest(t *testing.T) {
	body := strings.Replace(pinned, "sha256:"+strings.Repeat("b", 64), "sha256:TBD-at-release", 1)
	var out bytes.Buffer
	err := images([]string{"--release", writeRelease(t, body)}, &out)
	if err == nil || !strings.Contains(err.Error(), "services.data") {
		t.Fatalf("want a refusal naming services.data, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote %q before refusing", out.String())
	}
}

const withBuilds = `apiVersion: appliance/v1alpha1
kind: Release
spec:
  services:
    web-front:
      image: registry.example.org/appliance-web-front
      version: 0.1.0
      digest: sha256:TBD-at-release
      build:
        repository: example-owner/appliance-web
        commit: 9c24045aaa1d4a1288176934d9ee9557aa250b32
        dockerfile: Dockerfile
        context: .
        args:
          APP: front
          EDGE: live
    data:
      image: registry.example.org/appliance-data
      version: 0.1.0
      digest: sha256:TBD-at-release
      build:
        repository: example-owner/appliance-data
        commit: 2644629a50c138764f89d897bcc4b2e6c78e2924
        dockerfile: Dockerfile
        context: .
        target: data
  jobs:
    migrate:
      image: registry.example.org/appliance-migrate
      version: 0.1.0
      digest: sha256:TBD-at-release
      build:
        repository: example-owner/appliance-release
        commit: 59056d56b316b32d768bebd608399d0a97b269ee
        dockerfile: migrate/Dockerfile
        context: .
  thirdParty:
    postgres:
      image: docker.io/library/postgres
      digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`

// sources lists every service image the release builds, one line each,
// for build/release/images.sh.
func TestSourcesListsEveryServiceBuild(t *testing.T) {
	var out bytes.Buffer
	if err := sources([]string{"--release", writeRelease(t, withBuilds)}, &out); err != nil {
		t.Fatal(err)
	}
	want := "data registry.example.org/appliance-data 0.1.0 example-owner/appliance-data 2644629a50c138764f89d897bcc4b2e6c78e2924 Dockerfile . data -\n" +
		"migrate registry.example.org/appliance-migrate 0.1.0 example-owner/appliance-release 59056d56b316b32d768bebd608399d0a97b269ee migrate/Dockerfile . - -\n" +
		"web-front registry.example.org/appliance-web-front 0.1.0 example-owner/appliance-web 9c24045aaa1d4a1288176934d9ee9557aa250b32 Dockerfile . - APP=front,EDGE=live\n"
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestSourcesRefusesAServiceWithoutABuild(t *testing.T) {
	for name, body := range map[string]string{
		"no build":       strings.Replace(withBuilds, "      build:\n        repository: example-owner/appliance-data\n        commit: 2644629a50c138764f89d897bcc4b2e6c78e2924\n        dockerfile: Dockerfile\n        context: .\n        target: data\n", "", 1),
		"a short commit": strings.Replace(withBuilds, "2644629a50c138764f89d897bcc4b2e6c78e2924", "2644629", 1),
		"a path outside": strings.Replace(withBuilds, "dockerfile: Dockerfile\n        context: .\n        target", "dockerfile: ../Dockerfile\n        context: .\n        target", 1),
		"a spaced arg":   strings.Replace(withBuilds, "EDGE: live", "EDGE: live mock", 1),
		"no version":     strings.Replace(withBuilds, "      version: 0.1.0\n", "", 1),
		"another repo":   strings.Replace(withBuilds, "repository: example-owner/appliance-data", "repository: example/appliance-data x", 1),
		"no image":       strings.Replace(withBuilds, "      image: registry.example.org/appliance-data\n", "", 1),
	} {
		var out bytes.Buffer
		if err := sources([]string{"--release", writeRelease(t, body)}, &out); err == nil {
			t.Errorf("%s: listed\n%s", name, out.String())
		}
	}
}

// version prints the release's own version, the product version the
// bundle's stacks hand the product.
func TestVersionPrintsTheReleasesVersion(t *testing.T) {
	var out bytes.Buffer
	body := strings.Replace(pinned, "kind: Release\n", "kind: Release\nmetadata:\n  version: 0.1.0\n", 1)
	if err := version([]string{"--release", writeRelease(t, body)}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "0.1.0\n" {
		t.Fatalf("got %q", out.String())
	}
	for name, v := range map[string]string{"none": "", "not a version": "0.1.0 lab", "a path": "../0.1.0"} {
		body := strings.Replace(pinned, "kind: Release\n", "kind: Release\nmetadata:\n  version: \""+v+"\"\n", 1)
		if err := version([]string{"--release", writeRelease(t, body)}, &bytes.Buffer{}); err == nil {
			t.Errorf("%s: a release version %q was printed", name, v)
		}
	}
}

// fill --version makes the tag the release's version, bare, for bundle
// version and the stacks.
func TestFillSetsTheReleasesVersion(t *testing.T) {
	body := strings.Replace(pinned, "kind: Release\n", "kind: Release\nmetadata:\n  version: 0.1.0\n", 1)
	for _, tag := range []string{"0.1.0-rc.6", "v0.1.0-rc.6"} {
		var filled bytes.Buffer
		if err := fill([]string{"--release", writeRelease(t, body), "--layouts", t.TempDir(), "--version", tag}, &filled); err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		var out bytes.Buffer
		if err := version([]string{"--release", writeRelease(t, filled.String())}, &out); err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if out.String() != "0.1.0-rc.6\n" {
			t.Fatalf("%s: got %q", tag, out.String())
		}
	}
	var kept bytes.Buffer
	if err := fill([]string{"--release", writeRelease(t, body), "--layouts", t.TempDir()}, &kept); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(kept.String(), "version: 0.1.0\n") {
		t.Fatalf("no --version changed the release's version:\n%s", kept.String())
	}
	for name, v := range map[string]string{"not a version": "0.1.0 lab", "a path": "../0.1.0"} {
		if err := fill([]string{"--release", writeRelease(t, body), "--layouts", t.TempDir(), "--version", v}, &bytes.Buffer{}); err == nil {
			t.Errorf("%s: filled with the version %q", name, v)
		}
	}
	if err := fill([]string{"--release", writeRelease(t, pinned), "--layouts", t.TempDir(), "--version", "0.1.0-rc.6"}, &bytes.Buffer{}); err == nil {
		t.Error("filled a release.yaml with no metadata.version")
	}
}

func TestIndexNeedsAnOutAndTwoArchitectures(t *testing.T) {
	if err := run([]string{"index", t.TempDir(), t.TempDir()}); err == nil || !strings.Contains(err.Error(), "--out") {
		t.Fatalf("no --out: %v", err)
	}
	if err := run([]string{"index", "--out", filepath.Join(t.TempDir(), "out"), t.TempDir()}); err == nil || !strings.Contains(err.Error(), "two") {
		t.Fatalf("one architecture: %v", err)
	}
}
