// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package boxsecrets_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"example.org/appliance/internal/boxsecrets"
	"example.org/appliance/internal/productspec"
)

const spec = `format: 2
box_secrets:
  - secret: app/app-bundled
    keys:
      - {key: password, generate: password}
      - {key: redis-url, value: "redis://:{password}@appliance-valkey:6379/0"}
  - secret: app/app-identity
    keys:
      - {key: dsn, value: "postgres://app:{app-bundled/password}@pg/k"}
      - {key: smtp, value: "smtp://smtp.example.org:25/"}
  - secret: app/app-box
    keys:
      - {key: KEK, generate: key32}
      - {key: SETUP_TOKEN, generate: token}
`

type secret struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	StringData map[string]string `yaml:"stringData"`
}

func slot(t *testing.T, doc string) string {
	t.Helper()
	dir := t.TempDir()
	if doc != "" {
		if err := os.WriteFile(filepath.Join(dir, productspec.File), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readStack(t *testing.T, manifests string) map[string]secret {
	t.Helper()
	p := filepath.Join(manifests, productspec.BoxSecretsStack, productspec.BoxSecretsStack+".yaml")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("the stack is %v, want 0600", fi.Mode().Perm())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]secret{}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	for {
		var s secret
		if err := dec.Decode(&s); err != nil {
			break
		}
		if s.Kind == "Secret" {
			out[s.Metadata.Namespace+"/"+s.Metadata.Name] = s
		}
	}
	return out
}

// The box makes every declared Secret once: generated values of the
// declared kind, and values built from them.
func TestEnsureGeneratesTheDeclaredSecrets(t *testing.T) {
	st := boxsecrets.Store{Dir: filepath.Join(t.TempDir(), "platform"), Manifests: t.TempDir()}
	if err := st.Ensure(slot(t, spec)); err != nil {
		t.Fatal(err)
	}
	got := readStack(t, st.Manifests)
	if len(got) != 3 {
		t.Fatalf("%d secrets", len(got))
	}
	pw := got["app/app-bundled"].StringData["password"]
	if !regexp.MustCompile(`^[A-Za-z0-9]{32}$`).MatchString(pw) {
		t.Fatalf("password %d chars", len(pw))
	}
	if got["app/app-bundled"].StringData["redis-url"] != "redis://:"+pw+"@appliance-valkey:6379/0" {
		t.Fatal("redis-url isn't built from the password")
	}
	if got["app/app-identity"].StringData["dsn"] != "postgres://app:"+pw+"@pg/k" {
		t.Fatal("dsn isn't built from the other secret's password")
	}
	if got["app/app-identity"].StringData["smtp"] != "smtp://smtp.example.org:25/" {
		t.Fatal("a literal value")
	}
	kek, err := base64.StdEncoding.DecodeString(got["app/app-box"].StringData["KEK"])
	if err != nil || len(kek) != 32 {
		t.Fatalf("key32: %d bytes, %v", len(kek), err)
	}
	tok, err := base64.RawURLEncoding.DecodeString(got["app/app-box"].StringData["SETUP_TOKEN"])
	if err != nil || len(tok) != 32 {
		t.Fatalf("token: %d bytes, %v", len(tok), err)
	}
	fi, err := os.Stat(filepath.Join(st.Dir, boxsecrets.ValuesFile))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the values file: %v %v", fi, err)
	}
}

// A value is made once: a later Ensure (an update, a revert, a restart)
// keeps every value it made, adds what a newer bundle declares and writes
// only what the slot's bundle declares.
func TestEnsureKeepsWhatItMade(t *testing.T) {
	st := boxsecrets.Store{Dir: t.TempDir(), Manifests: t.TempDir()}
	if err := st.Ensure(slot(t, spec)); err != nil {
		t.Fatal(err)
	}
	first := readStack(t, st.Manifests)
	more := spec + "  - secret: app/app-issuer\n    keys:\n      - {key: secretsSystem, generate: password}\n"
	if err := st.Ensure(slot(t, more)); err != nil {
		t.Fatal(err)
	}
	second := readStack(t, st.Manifests)
	for _, n := range []string{"app/app-bundled", "app/app-identity", "app/app-box"} {
		for k, v := range first[n].StringData {
			if second[n].StringData[k] != v {
				t.Fatalf("%s %s changed", n, k)
			}
		}
	}
	if second["app/app-issuer"].StringData["secretsSystem"] == "" {
		t.Fatal("the newly declared secret")
	}
	// Back to the first bundle: the issuer Secret leaves the stack, and
	// its value is kept for a later bundle that declares it again.
	if err := st.Ensure(slot(t, spec)); err != nil {
		t.Fatal(err)
	}
	third := readStack(t, st.Manifests)
	if _, ok := third["app/app-issuer"]; ok {
		t.Fatal("an undeclared secret is still written")
	}
	if err := st.Ensure(slot(t, more)); err != nil {
		t.Fatal(err)
	}
	if readStack(t, st.Manifests)["app/app-issuer"].StringData["secretsSystem"] != second["app/app-issuer"].StringData["secretsSystem"] {
		t.Fatal("a kept value was made again")
	}
}

// A bundle that declares no box secrets leaves no stack.
func TestEnsureWithoutBoxSecretsRemovesTheStack(t *testing.T) {
	st := boxsecrets.Store{Dir: t.TempDir(), Manifests: t.TempDir()}
	if err := st.Ensure(slot(t, spec)); err != nil {
		t.Fatal(err)
	}
	if err := st.Ensure(slot(t, "format: 2\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(st.Manifests, productspec.BoxSecretsStack)); !os.IsNotExist(err) {
		t.Fatalf("the stack is still there: %v", err)
	}
	if err := st.Ensure(slot(t, "")); err != nil {
		t.Fatal(err)
	}
}

// Two boxes never share a value.
func TestTwoBoxesGetTheirOwnValues(t *testing.T) {
	a := boxsecrets.Store{Dir: t.TempDir(), Manifests: t.TempDir()}
	b := boxsecrets.Store{Dir: t.TempDir(), Manifests: t.TempDir()}
	s := slot(t, spec)
	if err := a.Ensure(s); err != nil {
		t.Fatal(err)
	}
	if err := b.Ensure(s); err != nil {
		t.Fatal(err)
	}
	if readStack(t, a.Manifests)["app/app-box"].StringData["KEK"] == readStack(t, b.Manifests)["app/app-box"].StringData["KEK"] {
		t.Fatal("two boxes share a key")
	}
}

// On a replacement box the escrow's sealed items came over (ImportEscrow),
// and the box secrets don't exist yet: a key the bundle names under escrow
// takes the escrowed value instead of a new one, so the restored data
// opens. Keys with nothing escrowed are made as usual.
func TestEnsureRestoresEscrowedKeysBeforeMakingThem(t *testing.T) {
	withEscrow := spec + `escrow:
  - {name: data-root-key, secret: app/app-box, key: KEK}
`
	escrowed := base64.StdEncoding.EncodeToString(make([]byte, 32))
	var asked []string
	st := boxsecrets.Store{Dir: t.TempDir(), Manifests: t.TempDir(), Restore: func(name string) (string, bool, error) {
		asked = append(asked, name)
		if name == "data-root-key" {
			return escrowed, true, nil
		}
		return "", false, nil
	}}
	if err := st.Ensure(slot(t, withEscrow)); err != nil {
		t.Fatal(err)
	}
	got := readStack(t, st.Manifests)
	if got["app/app-box"].StringData["KEK"] != escrowed {
		t.Fatal("the escrowed key wasn't restored")
	}
	if got["app/app-box"].StringData["SETUP_TOKEN"] == "" || got["app/app-bundled"].StringData["password"] == "" {
		t.Fatal("keys with nothing escrowed weren't made")
	}
	if strings.Join(asked, " ") != "data-root-key" {
		t.Fatalf("asked for %v", asked)
	}
	// Kept from then on: a later Ensure doesn't ask again.
	if err := st.Ensure(slot(t, withEscrow)); err != nil || len(asked) != 1 {
		t.Fatalf("%v, asked %v", err, asked)
	}
}

// If the escrow can't be read, nothing is made: a new key would leave the
// restored data unreadable, so the apply stops instead.
func TestEnsureStopsWhenTheEscrowCantBeRead(t *testing.T) {
	withEscrow := spec + `escrow:
  - {name: data-root-key, secret: app/app-box, key: KEK}
`
	st := boxsecrets.Store{Dir: t.TempDir(), Manifests: t.TempDir(), Restore: func(string) (string, bool, error) {
		return "", false, os.ErrPermission
	}}
	if err := st.Ensure(slot(t, withEscrow)); err == nil {
		t.Fatal("Ensure made a new key with the escrow unreadable")
	}
	if _, err := os.Stat(filepath.Join(st.Dir, boxsecrets.ValuesFile)); !os.IsNotExist(err) {
		t.Fatalf("values written: %v", err)
	}
}
