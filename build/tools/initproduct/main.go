// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

// Command initproduct fills the template's four placeholders with a
// product's own values, in place, in one pass (TEMPLATE.md lists where each
// one lands). Run it once, in a new product's clone of the template, then
// commit what it changed.
//
//	go run ./build/tools/initproduct \
//	    --module=github.com/acme/acme-appliance \
//	    --registry=ghcr.io/acme/ \
//	    --owner=acme \
//	    --holder="The Acme Authors" [--root=.] [--dry-run]
//
//	go run ./build/tools/initproduct --fixture [--root=.]
//
// The placeholders are valid Go and valid paths, so the template builds as it
// stands; this tool swaps them and then proves none is left. --fixture fills
// in a fictional product, Acme, which is what CI runs before building, so the
// filled template is checked the way a product uses it.
//
// gen/ is not rewritten: the protobuf descriptors embed the module path with
// its length, so the folder is rebuilt by scripts/proto-generate.sh (which
// needs buf, protoc-gen-go and protoc-gen-connect-go on PATH) after the fill.
// --skip-generate leaves gen/ stale and says so.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The fixed placeholder text in the tree. The module path and the registry
// share a prefix with the image paths, so they are matched in one pass (see
// fill) and never one after the other.
const (
	phModule   = "example.org/appliance"
	phRegistry = "registry.example.org/"
	phOwner    = "example-owner"
	phHolder   = "The Example Authors"
)

// Values are what a product puts in place of the placeholders.
type Values struct {
	Module   string // the Go module path, e.g. github.com/acme/acme-appliance
	Registry string // the image registry and owner, e.g. ghcr.io/acme/
	Owner    string // the GitHub owner of the release repos, e.g. acme
	Holder   string // the copyright holder in the file headers, e.g. The Acme Authors
}

// fixture is the fictional product CI fills the template with.
var fixture = Values{
	Module:   "github.com/acme-example/acme-appliance",
	Registry: "ghcr.io/acme-example/",
	Owner:    "acme-example",
	Holder:   "The Acme Authors",
}

var (
	moduleRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)+/[A-Za-z0-9._~/-]+$`)
	registryRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*(:[0-9]+)?(/[a-z0-9._-]+)*/$`)
	ownerRE    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	holderRE   = regexp.MustCompile(`^[\pL\pN][\pL\pN .,&'()-]*[\pL\pN.)]$`)
)

// normalised returns v with the registry's trailing slash added.
func (v Values) normalised() Values {
	if v.Registry != "" && !strings.HasSuffix(v.Registry, "/") {
		v.Registry += "/"
	}
	return v
}

// validate names every value that cannot be used. The values land in Go
// imports, YAML, shell and Markdown, so the character sets are narrow.
func (v Values) validate() []string {
	v = v.normalised()
	var p []string
	switch {
	case v.Module == "":
		p = append(p, "--module is missing")
	case !moduleRE.MatchString(v.Module):
		p = append(p, fmt.Sprintf("--module %q must be a Go module path with a host, like github.com/acme/acme-appliance", v.Module))
	case strings.HasPrefix(v.Module, phModule):
		p = append(p, fmt.Sprintf("--module %q is the placeholder itself", v.Module))
	}
	switch {
	case v.Registry == "":
		p = append(p, "--registry is missing")
	case !registryRE.MatchString(v.Registry):
		p = append(p, fmt.Sprintf("--registry %q must be a registry host and owner with no scheme, like ghcr.io/acme/", v.Registry))
	case v.Registry == phRegistry:
		p = append(p, fmt.Sprintf("--registry %q is the placeholder itself", v.Registry))
	}
	switch {
	case v.Owner == "":
		p = append(p, "--owner is missing")
	case !ownerRE.MatchString(v.Owner):
		p = append(p, fmt.Sprintf("--owner %q must be a GitHub user or organisation name", v.Owner))
	case v.Owner == phOwner:
		p = append(p, fmt.Sprintf("--owner %q is the placeholder itself", v.Owner))
	}
	switch {
	case v.Holder == "":
		p = append(p, "--holder is missing")
	case !holderRE.MatchString(v.Holder):
		p = append(p, fmt.Sprintf("--holder %q may hold letters, digits, spaces and . , & ' ( ) - only", v.Holder))
	case v.Holder == phHolder:
		p = append(p, fmt.Sprintf("--holder %q is the placeholder itself", v.Holder))
	}
	return p
}

// tokens matches the placeholders in one scan, so a value that was just
// written is never scanned again (a module path may itself hold "example-owner").
// registry.example.org/ is listed first and starts earlier than the
// example.org/appliance it contains, so it wins that overlap.
var tokens = regexp.MustCompile(regexp.QuoteMeta(phRegistry) + "|" + regexp.QuoteMeta(phModule) + "|" + regexp.QuoteMeta(phOwner) + "|" + regexp.QuoteMeta(phHolder))

// wordish reports whether b continues a name: a placeholder that is only the
// start or end of a longer name (mirror.example.org/appliance/,
// example.org/appliance-migrate) is somebody else's text, not ours.
func wordish(b byte, dot bool) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || dot && b == '.'
}

// fill replaces each placeholder in text with the matching value.
func fill(text string, v Values) string {
	v = v.normalised()
	var out strings.Builder
	last := 0
	for _, m := range tokens.FindAllStringIndex(text, -1) {
		start, end := m[0], m[1]
		tok := text[start:end]
		if tok != phHolder {
			if start > 0 && wordish(text[start-1], true) {
				continue
			}
			if tok != phRegistry && end < len(text) && wordish(text[end], false) {
				continue
			}
		}
		out.WriteString(text[last:start])
		switch tok {
		case phRegistry:
			out.WriteString(v.Registry)
		case phModule:
			out.WriteString(v.Module)
		case phOwner:
			out.WriteString(v.Owner)
		case phHolder:
			out.WriteString(v.Holder)
		}
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

// hasPlaceholder reports whether text still holds one, by the same rules as fill.
func hasPlaceholder(text string) bool {
	return fill(text, Values{Module: "\x00", Registry: "\x00/", Owner: "\x00", Holder: "\x00"}) != text
}

// Kept as they are: the folder the protobuf generator owns, this tool (its
// constants are the placeholders), the guide that documents them, the check
// that names the real organisation, and the files that carry attribution or
// hashes.
var (
	keptDirs  = []string{".git", "gen", "build/tools/initproduct"}
	keptFiles = map[string]bool{
		"TEMPLATE.md":               true,
		"go.sum":                    true,
		"NOTICE":                    true,
		"LICENSE":                   true,
		"build/ci/org_name_test.sh": true,
	}
)

func kept(rel string, includeGen bool) bool {
	if keptFiles[rel] {
		return true
	}
	for _, d := range keptDirs {
		if includeGen && d == "gen" {
			continue
		}
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

// textFiles lists the files under root a fill or a check looks at: not kept,
// not binary.
func textFiles(root string, includeGen bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if kept(rel, includeGen) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path) // #nosec G122 G304 -- a build tool reading the tree it was pointed at
		if err != nil {
			return err
		}
		if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return files, err
}

// leftovers names every file under root that still holds a placeholder,
// gen/ included.
func leftovers(root string) ([]string, error) {
	files, err := textFiles(root, true)
	if err != nil {
		return nil, err
	}
	var left []string
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 -- a build tool reading the tree it was pointed at
		if err != nil {
			return nil, err
		}
		if hasPlaceholder(string(b)) {
			left = append(left, rel)
		}
	}
	return left, nil
}

type options struct {
	dryRun       bool
	skipGenerate bool
}

// run validates v, fills the tree under root, rebuilds gen/ and checks that
// no placeholder is left. Nothing is written unless every value is valid.
func run(root string, v Values, opt options) error {
	if p := v.validate(); len(p) > 0 {
		return errors.New(strings.Join(p, "\n"))
	}
	v = v.normalised()
	files, err := textFiles(root, false)
	if err != nil {
		return err
	}
	changed := 0
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		old, err := os.ReadFile(path) // #nosec G304 -- a build tool reading the tree it was pointed at
		if err != nil {
			return err
		}
		next := []byte(fill(string(old), v))
		if bytes.Equal(old, next) {
			continue
		}
		if strings.HasSuffix(rel, ".go") {
			// The new import paths sort differently from the placeholders.
			if next, err = format.Source(next); err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
		}
		changed++
		if opt.dryRun {
			fmt.Println("would change", rel)
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, next, info.Mode().Perm()); err != nil { // #nosec G703 -- the file just read
			return err
		}
	}
	fmt.Printf("%d files changed\n", changed)
	if opt.dryRun {
		return nil
	}
	if opt.skipGenerate {
		fmt.Println("gen/ was not rebuilt (--skip-generate): run scripts/proto-generate.sh before building")
	} else {
		cmd := exec.Command("bash", "scripts/proto-generate.sh") // #nosec G204 -- a fixed repository script
		cmd.Dir = root
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("rebuilding gen/ with scripts/proto-generate.sh (needs buf, protoc-gen-go and protoc-gen-connect-go on PATH): %w", err)
		}
	}
	left, err := leftovers(root)
	if err != nil {
		return err
	}
	if opt.skipGenerate {
		keep := left[:0]
		for _, rel := range left {
			if !strings.HasPrefix(rel, "gen/") {
				keep = append(keep, rel)
			}
		}
		left = keep
	}
	if len(left) > 0 {
		return fmt.Errorf("placeholders are still left in:\n  %s", strings.Join(left, "\n  "))
	}
	return nil
}

func main() {
	var (
		v    Values
		opt  options
		root = flag.String("root", ".", "the template's root directory")
		fix  = flag.Bool("fixture", false, "fill in a fictional product (Acme) instead of --module, --registry, --owner and --holder")
	)
	flag.StringVar(&v.Module, "module", "", "the product's Go module path, e.g. github.com/acme/acme-appliance")
	flag.StringVar(&v.Registry, "registry", "", "the product's image registry and owner, e.g. ghcr.io/acme/")
	flag.StringVar(&v.Owner, "owner", "", "the GitHub owner of the product's release repos, e.g. acme")
	flag.StringVar(&v.Holder, "holder", "", "the copyright holder for the file headers, e.g. \"The Acme Authors\"")
	flag.BoolVar(&opt.dryRun, "dry-run", false, "name the files that would change and write nothing")
	flag.BoolVar(&opt.skipGenerate, "skip-generate", false, "do not rebuild gen/ (it is left holding placeholders)")
	flag.Parse()
	if *fix {
		v = fixture
	}
	if err := run(*root, v, opt); err != nil {
		fmt.Fprintln(os.Stderr, "initproduct:", err)
		os.Exit(1)
	}
}
