// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var acme = Values{
	Module:   "github.com/acme-example/acme-appliance",
	Registry: "ghcr.io/acme-example/",
	Owner:    "acme-example",
	Holder:   "The Acme Authors",
}

func TestFillReplacesEachPlaceholderAndNothingNear(t *testing.T) {
	in := strings.Join([]string{
		`import "example.org/appliance/internal/release"`,
		`image: registry.example.org/appliance-data`,
		`image: example.org/appliance-migrate@sha256:00`,
		`mirror: https://mirror.example.org/appliance/`,
		`host: box1.appliance.example.org`,
		`slug: example-owner/appliance-release`,
		`// Copyright 2026 The Example Authors`,
		`module example.org/appliance`,
		`go build -ldflags "-X example.org/appliance/internal/release.Channel=lab"`,
	}, "\n")
	want := strings.Join([]string{
		`import "github.com/acme-example/acme-appliance/internal/release"`,
		`image: ghcr.io/acme-example/appliance-data`,
		`image: example.org/appliance-migrate@sha256:00`,
		`mirror: https://mirror.example.org/appliance/`,
		`host: box1.appliance.example.org`,
		`slug: acme-example/appliance-release`,
		`// Copyright 2026 The Acme Authors`,
		`module github.com/acme-example/acme-appliance`,
		`go build -ldflags "-X github.com/acme-example/acme-appliance/internal/release.Channel=lab"`,
	}, "\n")
	if got := fill(in, acme); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFillDoesNotRewriteAValueItJustWrote(t *testing.T) {
	// A product whose module path itself contains a placeholder-looking
	// segment must not be rewritten twice.
	v := Values{Module: "git.example.net/example-owner/app", Registry: "ghcr.io/x/", Owner: "x", Holder: "X"}
	got := fill("example.org/appliance example-owner", v)
	if want := "git.example.net/example-owner/app x"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]Values{
		"module without a host":       {Module: "appliance", Registry: acme.Registry, Owner: acme.Owner, Holder: acme.Holder},
		"module is the placeholder":   {Module: "example.org/appliance", Registry: acme.Registry, Owner: acme.Owner, Holder: acme.Holder},
		"module with a quote":         {Module: `a.io/"x`, Registry: acme.Registry, Owner: acme.Owner, Holder: acme.Holder},
		"registry is the placeholder": {Module: acme.Module, Registry: "registry.example.org/", Owner: acme.Owner, Holder: acme.Holder},
		"registry with a scheme":      {Module: acme.Module, Registry: "https://ghcr.io/acme/", Owner: acme.Owner, Holder: acme.Holder},
		"owner is the placeholder":    {Module: acme.Module, Registry: acme.Registry, Owner: "example-owner", Holder: acme.Holder},
		"owner with a slash":          {Module: acme.Module, Registry: acme.Registry, Owner: "a/b", Holder: acme.Holder},
		"holder with a quote":         {Module: acme.Module, Registry: acme.Registry, Owner: acme.Owner, Holder: `The "Acme" Authors`},
		"holder is the placeholder":   {Module: acme.Module, Registry: acme.Registry, Owner: acme.Owner, Holder: "The Example Authors"},
		"holder empty":                {Module: acme.Module, Registry: acme.Registry, Owner: acme.Owner},
	}
	for name, v := range bad {
		if len(v.validate()) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	if p := acme.validate(); len(p) != 0 {
		t.Fatalf("the fixture was refused: %v", p)
	}
	noSlash := acme
	noSlash.Registry = "ghcr.io/acme-example"
	if p := noSlash.validate(); len(p) != 0 {
		t.Fatalf("a registry without its trailing slash was refused: %v", p)
	}
}

// tree writes files (path -> content) under a new directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunFillsTheTreeAndLeavesTheKeptFilesAlone(t *testing.T) {
	root := tree(t, map[string]string{
		"go.mod":                       "module example.org/appliance\n",
		"a/a.go":                       "package a\n\nimport (\n\t\"fmt\"\n\n\t\"example.org/appliance/internal/x\"\n\t\"github.com/a/a\"\n)\n\nvar _, _ = fmt.Sprint, x.X\n",
		"Taskfile.yaml":                "COMMON_LIC: \"2026 The Example Authors\"\n",
		"NOTICE":                       "Copyright 2026 The Rabbit Hole Tech, LLC\n",
		"gen/go/x.pb.go":               "package x // example.org/appliance\n",
		"TEMPLATE.md":                  "the table names example.org/appliance on purpose\n",
		"build/tools/initproduct/m.go": "package main // example.org/appliance\n",
		"img.png":                      "\x89PNG\x00example.org/appliance\n",
		"go.sum":                       "example.org/appliance h1:x\n",
	})
	if err := run(root, acme, options{skipGenerate: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "go.mod"); got != "module github.com/acme-example/acme-appliance\n" {
		t.Fatalf("go.mod %q", got)
	}
	if got := read(t, root, "Taskfile.yaml"); got != "COMMON_LIC: \"2026 The Acme Authors\"\n" {
		t.Fatalf("Taskfile %q", got)
	}
	// gofmt put the rewritten imports back in order: github.com/acme-example
	// now sorts after github.com/a.
	if got := read(t, root, "a/a.go"); !strings.Contains(got, "\t\"github.com/acme-example/acme-appliance/internal/x\"\n") ||
		strings.Index(got, "github.com/a/a") > strings.Index(got, "github.com/acme-example") {
		t.Fatalf("a.go imports not sorted:\n%s", got)
	}
	for _, kept := range []string{"NOTICE", "gen/go/x.pb.go", "TEMPLATE.md", "build/tools/initproduct/m.go", "img.png", "go.sum"} {
		if strings.Contains(read(t, root, kept), "acme") || strings.Contains(read(t, root, kept), "Acme") {
			t.Errorf("%s was rewritten", kept)
		}
	}
}

func TestRunDryRunWritesNothing(t *testing.T) {
	root := tree(t, map[string]string{"go.mod": "module example.org/appliance\n"})
	if err := run(root, acme, options{dryRun: true, skipGenerate: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "go.mod"); got != "module example.org/appliance\n" {
		t.Fatalf("a dry run changed go.mod: %q", got)
	}
}

func TestRunRefusesBadValuesBeforeWritingAnything(t *testing.T) {
	root := tree(t, map[string]string{"go.mod": "module example.org/appliance\n"})
	bad := acme
	bad.Owner = "a/b"
	if err := run(root, bad, options{skipGenerate: true}); err == nil {
		t.Fatal("bad values were accepted")
	}
	if got := read(t, root, "go.mod"); got != "module example.org/appliance\n" {
		t.Fatalf("go.mod changed: %q", got)
	}
}

func TestCheckNamesEveryLeftoverPlaceholder(t *testing.T) {
	root := tree(t, map[string]string{
		"ok.go":       "package ok\n",
		"left/a.go":   "package left // registry.example.org/appliance-data\n",
		"left/b.md":   "see example-owner/appliance-release\n",
		"gen/go/g.go": "package g // example.org/appliance/gen\n",
		"TEMPLATE.md": "example.org/appliance on purpose\n",
	})
	left, err := leftovers(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(left, "\n")
	for _, want := range []string{"left/a.go", "left/b.md", "gen/go/g.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s not named in:\n%s", want, got)
		}
	}
	for _, not := range []string{"ok.go", "TEMPLATE.md"} {
		if strings.Contains(got, not) {
			t.Errorf("%s named but fine:\n%s", not, got)
		}
	}
}
