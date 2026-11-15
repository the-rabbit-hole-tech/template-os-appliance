// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package bundle_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	orasoci "oras.land/oras-go/v2/content/oci"

	"example.org/appliance/internal/bundle"
	"example.org/appliance/internal/codes"
	"example.org/appliance/test/kit/fixtures"
)

// built writes a single-platform image as an OCI layout in dir, the way
// the release's images job leaves each service image it built.
func built(t *testing.T, dir string) (digest.Digest, digest.Digest) {
	t.Helper()
	ctx := context.Background()
	store, err := orasoci.NewWithContext(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	put := func(mt string, b []byte) ocispec.Descriptor {
		d := ocispec.Descriptor{MediaType: mt, Digest: digest.FromBytes(b), Size: int64(len(b))}
		if err := store.Push(ctx, d, bytes.NewReader(b)); err != nil {
			t.Fatal(err)
		}
		return d
	}
	layer := make([]byte, 1024)
	_, _ = rand.Read(layer)
	ld := put(ocispec.MediaTypeImageLayer, layer)
	cfg := put(ocispec.MediaTypeImageConfig, []byte(`{"architecture":"amd64","os":"linux"}`))
	m, err := json.Marshal(ocispec.Manifest{Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest, Config: cfg, Layers: []ocispec.Descriptor{ld}})
	if err != nil {
		t.Fatal(err)
	}
	md := put(ocispec.MediaTypeImageManifest, m)
	if err := store.Tag(ctx, md, "built"); err != nil {
		t.Fatal(err)
	}
	return md.Digest, ld.Digest
}

// builtFrom is built, recording the commit the image was built from, as
// build/release/images.sh does.
func builtFrom(t *testing.T, dir, commit string) digest.Digest {
	t.Helper()
	d, _ := built(t, dir)
	if err := os.WriteFile(filepath.Join(dir, bundle.BuildCommitFile), []byte(commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

const toFill = `# The pinned release (a comment the fill keeps).
apiVersion: appliance/v1alpha1
kind: Release
metadata:
  version: 0.1.0
spec:
  services:
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
    web-front:
      image: registry.example.org/appliance-web-front
      digest: sha256:TBD-at-release
      build:
        repository: example-owner/appliance-web
        commit: 9c24045aaa1d4a1288176934d9ee9557aa250b32
        dockerfile: Dockerfile
        context: .
        args:
          APP: front
  jobs:
    migrate:
      image: registry.example.org/appliance-migrate
      digest: sha256:TBD-at-release
      build:
        repository: example-owner/appliance-release
        commit: 59056d56b316b32d768bebd608399d0a97b269ee
        dockerfile: migrate/Dockerfile
        context: .
  thirdParty:
    postgres:
      image: docker.io/library/postgres
      digest: sha256:5a5a84b19854a9ffaa54082c166ff4ec27473a361e496e5ea167f298f2da9722
`

// The release's build job puts each service image it built in place of its
// placeholder digest, and keeps the rest of release.yaml as it is.
func TestFillPutsTheBuiltDigestsInPlace(t *testing.T) {
	layouts := t.TempDir()
	data := builtFrom(t, filepath.Join(layouts, "data"), "2644629a50c138764f89d897bcc4b2e6c78e2924")
	front := builtFrom(t, filepath.Join(layouts, "web-front"), "9c24045aaa1d4a1288176934d9ee9557aa250b32")
	migrate := builtFrom(t, filepath.Join(layouts, "migrate"), "59056d56b316b32d768bebd608399d0a97b269ee")
	out, err := bundle.Fill([]byte(toFill), layouts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "TBD") {
		t.Fatalf("a placeholder is left:\n%s", out)
	}
	for _, want := range []string{"# The pinned release (a comment the fill keeps).", "digest: " + data.String(), "digest: " + front.String(), "digest: " + migrate.String(),
		"commit: 2644629a50c138764f89d897bcc4b2e6c78e2924", "APP: front", "sha256:5a5a84b19854a9ffaa54082c166ff4ec27473a361e496e5ea167f298f2da9722"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	rel, err := bundle.ParseRelease(out)
	if err != nil {
		t.Fatal(err)
	}
	images, err := rel.Images()
	if err != nil {
		t.Fatal(err)
	}
	if images[migrate.Encoded()] != "registry.example.org/appliance-migrate" {
		t.Fatal("the bundle doesn't carry the migrate Job image")
	}
	if b := rel.BuiltImages(); len(b) != 3 || b["migrate"].Build == nil {
		t.Fatalf("the images the release builds %v", b)
	}
	if b := rel.Spec.Services["web-front"].Build; b == nil || b.Repository != "example-owner/appliance-web" || b.Args["APP"] != "front" || b.Target != "" {
		t.Fatalf("the build block %+v", b)
	}
}

func TestFillRefuses(t *testing.T) {
	layouts := t.TempDir()
	builtFrom(t, filepath.Join(layouts, "data"), "2644629a50c138764f89d897bcc4b2e6c78e2924")
	// No image built for a placeholder.
	if _, err := bundle.Fill([]byte(toFill), layouts); !codes.Is(err, codes.KitBundleMismatch) || !strings.Contains(err.Error(), "services.web-front") {
		t.Fatalf("a placeholder with no built image: %v", err)
	}
	builtFrom(t, filepath.Join(layouts, "web-front"), "9c24045aaa1d4a1288176934d9ee9557aa250b32")
	builtFrom(t, filepath.Join(layouts, "migrate"), "59056d56b316b32d768bebd608399d0a97b269ee")
	// A pinned digest the built image doesn't have.
	pinned := strings.Replace(toFill, "sha256:TBD-at-release", "sha256:"+strings.Repeat("ab", 32), 1)
	if _, err := bundle.Fill([]byte(pinned), layouts); !codes.Is(err, codes.KitBundleMismatch) || !strings.Contains(err.Error(), "services.data") {
		t.Fatalf("a built image that isn't the pinned digest: %v", err)
	}
	// An image built from another commit than its build block names.
	stale := t.TempDir()
	builtFrom(t, filepath.Join(stale, "data"), strings.Repeat("0", 40))
	builtFrom(t, filepath.Join(stale, "web-front"), "9c24045aaa1d4a1288176934d9ee9557aa250b32")
	builtFrom(t, filepath.Join(stale, "migrate"), "59056d56b316b32d768bebd608399d0a97b269ee")
	if _, err := bundle.Fill([]byte(toFill), stale); !codes.Is(err, codes.KitBundleMismatch) || !strings.Contains(err.Error(), "services.data") || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("an image built from another commit: %v", err)
	}
	// A built image release.yaml doesn't name.
	built(t, filepath.Join(layouts, "stray"))
	if _, err := bundle.Fill([]byte(toFill), layouts); !codes.Is(err, codes.KitBundleMismatch) || !strings.Contains(err.Error(), "stray") {
		t.Fatalf("a built image release.yaml doesn't name: %v", err)
	}
}

// The bundle takes a service image the build made from its layout, never
// from the registry it'll be pushed to; the others still come from theirs.
func TestPullTakesBuiltImagesFromTheirLayouts(t *testing.T) {
	k := fixtures.LabKeys(t)
	layouts := t.TempDir()
	d, layer := built(t, filepath.Join(layouts, "data"))
	_, multi := lab(t)
	local := pushed{ref: "registry.example.org/appliance-data", digest: d}
	rel := releaseFor(t, local, multi)
	sigs := t.TempDir()
	sign(t, sigs, k, false, local, multi)
	out := filepath.Join(t.TempDir(), "images")
	o := pullOpts(t, rel, sigs, out, k)
	o.Layouts = layouts
	if err := bundle.Pull(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if n := archiveNames(t, filepath.Join(out, d.Encoded()+".tar")); !n["blobs/sha256/"+layer.Encoded()] {
		t.Fatalf("the built image's layer isn't in its archive: %v", n)
	}
	b, err := bundle.LocalManifest(layouts, d.String())
	if err != nil || digest.FromBytes(b) != d {
		t.Fatalf("the built image's manifest: %v", err)
	}
	if _, err := bundle.LocalManifest(layouts, "sha256:"+strings.Repeat("cd", 32)); !codes.Is(err, codes.KitBundleMismatch) {
		t.Fatalf("a digest no layout holds: %v", err)
	}
}

// The publish job pushes each built image by its digest (and the version
// tag) to the image release.yaml names.
func TestPushPutsTheBuiltImageInTheRegistry(t *testing.T) {
	reg := fixtures.StartRegistry(t)
	layouts := t.TempDir()
	d, _ := built(t, filepath.Join(layouts, "data"))
	image := reg.Host + "/appliance/appliance-data"
	if err := bundle.PushBuilt(context.Background(), bundle.PushOptions{Layouts: layouts, Image: image, Digest: d.String(), Tag: "0.1.0-rc.1", PlainHTTP: true}); err != nil {
		t.Fatal(err)
	}
	b, err := bundle.FetchManifest(context.Background(), image, d.String(), true)
	if err != nil || digest.FromBytes(b) != d {
		t.Fatalf("the pushed manifest: %v", err)
	}
	if err := bundle.PushBuilt(context.Background(), bundle.PushOptions{Layouts: layouts, Image: image, Digest: "sha256:" + strings.Repeat("ef", 32), Tag: "x", PlainHTTP: true}); !codes.Is(err, codes.KitBundleMismatch) {
		t.Fatalf("a digest no layout holds: %v", err)
	}
}
