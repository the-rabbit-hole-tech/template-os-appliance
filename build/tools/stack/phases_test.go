// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.org/appliance/internal/bundle"
)

const phasedProductYAML = productYAML + `switches:
  - {name: extra, label: The extra service, stacks: [app-extra]}
phases:
  - {name: data, label: The database, stack: app-data, workloads: [app-postgres]}
  - {name: front, label: The gateway, stack: app-front, workloads: [app-gateway, app-extra], switch_stacks: [app-extra]}
`

func renderPhased(t *testing.T, product string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	w := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	o := options{
		Release: w("release.yaml", release), ProductYAML: w("product.yaml", product),
		Off: w("off.yaml", off), On: w("on.yaml", strings.Replace(on, `EXTRA_ENABLED: "false"`, `EXTRA_ENABLED: "true"`, 1)),
		Out: filepath.Join(dir, "stacks"), Namespace: "app", Stack: "app", SwitchStack: "app-extra",
		SwitchConfigMap: "app-extra-switch", SwitchFrom: []string{"app-gateway"}, Data: "/var/lib/app-data",
	}
	return o.Out, render(o, &bytes.Buffer{})
}

func readStack(t *testing.T, out, stack string) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, stack, stack+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return docs(t, string(b))
}

func labels(d map[string]any, path ...string) map[string]any {
	for _, k := range path {
		next, _ := d[k].(map[string]any)
		if next == nil {
			return nil
		}
		d = next
	}
	l, _ := d["labels"].(map[string]any)
	return l
}

// With phases, each phase's workloads go in the phase's own stack, labelled
// with the phase and its place in the order on the workload and its pods;
// everything else (the Namespace, Services, ConfigMaps, Ingresses) stays in
// the always-on stack, and the switch's workloads stay in the switch's
// stack with their phase's labels.
func TestRenderSplitsTheWorkloadsByPhase(t *testing.T) {
	out, err := renderPhased(t, phasedProductYAML)
	if err != nil {
		t.Fatal(err)
	}
	main := readStack(t, out, "app")
	for _, k := range []string{"Deployment/app-gateway", "StatefulSet/app-postgres"} {
		if _, ok := main[k]; ok {
			t.Errorf("%s is in the always-on stack", k)
		}
	}
	for _, k := range []string{"Namespace/app", "ConfigMap/app-gateway", "Ingress/app-gateway"} {
		if _, ok := main[k]; !ok {
			t.Errorf("%s isn't in the always-on stack", k)
		}
	}
	for stack, want := range map[string]struct{ key, phase, order string }{
		"app-data":  {"StatefulSet/app-postgres", "data", "1"},
		"app-front": {"Deployment/app-gateway", "front", "2"},
		"app-extra": {"Deployment/app-extra", "front", "2"},
	} {
		s := readStack(t, out, stack)
		d, ok := s[want.key]
		if !ok {
			t.Errorf("%s isn't in %s: %v", want.key, stack, s)
			continue
		}
		if d["metadata"].(map[string]any)["namespace"] != "app" {
			t.Errorf("%s has no namespace", want.key)
		}
		for _, l := range []map[string]any{labels(d, "metadata"), labels(d, "spec", "template", "metadata")} {
			if l["appliance/phase"] != want.phase || l["appliance/phase-order"] != want.order {
				t.Errorf("%s in %s: labels %v", want.key, stack, l)
			}
		}
	}
	if _, ok := readStack(t, out, "app-extra")["ConfigMap/app-extra-switch"]; !ok {
		t.Error("the switch's ConfigMap left the switch's stack")
	}
}

// A workload no phase names, or a switch workload whose phase doesn't
// place the switch's stack, is refused: nothing slips in unordered.
func TestRenderRefusesAnUnphasedWorkload(t *testing.T) {
	for name, tc := range map[string]struct{ product, want string }{
		"a workload in no phase": {strings.Replace(phasedProductYAML, "workloads: [app-postgres]", "workloads: [app-valkey]", 1), "app-postgres"},
		"a switch workload whose phase doesn't place the switch stack": {
			strings.Replace(strings.Replace(phasedProductYAML, ", switch_stacks: [app-extra]", "", 1), "workloads: [app-gateway, app-extra]", "workloads: [app-gateway]", 1) +
				"  - {name: late, label: Late, stack: app-late, workloads: [app-extra]}\n",
			"app-extra",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := renderPhased(t, tc.product); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a refusal naming %s", err, tc.want)
			}
		})
	}
}

// Without phases the render is what it was: every workload in the
// always-on stack, unlabelled.
func TestRenderWithoutPhasesKeepsOneStack(t *testing.T) {
	main, _, err := run(t, off, on, productYAML)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(main, "appliance/phase") {
		t.Fatal("an unphased render carries phase labels")
	}
}

const phasedRelease = release + `  jobs:
    migrate:
      image: registry.example.org/appliance-migrate
      digest: sha256:4444444444444444444444444444444444444444444444444444444444444444
`

// Every phased workload starts with the wait init container: the wait
// image (the release's migrate Job image) resolves the cluster's DNS,
// then connects to each Service its phase says it needs, so a pod the
// kubelet starts out of order (after a power loss) waits instead of
// crashing.
func TestRenderGivesEachPhasedWorkloadItsWait(t *testing.T) {
	product := strings.Replace(phasedProductYAML, "switch_stacks: [app-extra]}", "switch_stacks: [app-extra], needs: {app-gateway: [app-postgres:5432]}}", 1)
	dir := t.TempDir()
	w := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	o := options{
		Release: w("release.yaml", phasedRelease), ProductYAML: w("product.yaml", product),
		Off: w("off.yaml", off), On: w("on.yaml", strings.Replace(on, `EXTRA_ENABLED: "false"`, `EXTRA_ENABLED: "true"`, 1)),
		Out: filepath.Join(dir, "stacks"), Namespace: "app", Stack: "app", SwitchStack: "app-extra",
		SwitchConfigMap: "app-extra-switch", SwitchFrom: []string{"app-gateway"}, Data: "/var/lib/app-data", WaitJob: "migrate",
	}
	if err := render(o, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	wantImage := "registry.example.org/appliance-migrate@sha256:4444444444444444444444444444444444444444444444444444444444444444"
	for stack, want := range map[string]struct{ key, args string }{
		"app-front": {"Deployment/app-gateway", "wait --dns kubernetes.default.svc.cluster.local --tcp app-postgres.app.svc:5432 --every 2s"},
		"app-data":  {"StatefulSet/app-postgres", "wait --dns kubernetes.default.svc.cluster.local --every 2s"},
		"app-extra": {"Deployment/app-extra", "wait --dns kubernetes.default.svc.cluster.local --every 2s"},
	} {
		d := readStack(t, out(o), stack)[want.key]
		inits := d["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["initContainers"].([]any)
		first := inits[0].(map[string]any)
		if first["name"] != "wait-phase" || first["image"] != wantImage || first["imagePullPolicy"] != "Never" {
			t.Fatalf("%s's first init container %v", want.key, first)
		}
		if got := strings.Join(toStrings(first["args"]), " "); got != want.args {
			t.Errorf("%s waits with %q, want %q", want.key, got, want.args)
		}
		if err := bundle.CheckWaitArgs(toStrings(first["args"])); err != nil {
			t.Errorf("%s's wait is one the release's wait command refuses: %v", want.key, err)
		}
		sc := first["securityContext"].(map[string]any)
		if sc["runAsNonRoot"] != true || sc["readOnlyRootFilesystem"] != true || sc["allowPrivilegeEscalation"] != false {
			t.Errorf("%s's wait runs with %v", want.key, sc)
		}
	}
	main := readStack(t, out(o), "app")
	if strings.Contains(fmt.Sprint(main), "wait-phase") {
		t.Fatal("the always-on stack has a wait")
	}
}

// A phased render needs the wait image: a release without the Job image
// it names is refused.
func TestRenderRefusesAPhasedProductWithoutItsWaitImage(t *testing.T) {
	dir := t.TempDir()
	w := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	o := options{
		Release: w("release.yaml", release), ProductYAML: w("product.yaml", phasedProductYAML),
		Off: w("off.yaml", off), On: w("on.yaml", on),
		Out: filepath.Join(dir, "stacks"), Namespace: "app", Stack: "app", SwitchStack: "app-extra",
		SwitchConfigMap: "app-extra-switch", SwitchFrom: []string{"app-gateway"}, Data: "/var/lib/app-data", WaitJob: "migrate",
	}
	if err := render(o, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("got %v, want a refusal naming the wait Job image", err)
	}
}

func out(o options) string { return o.Out }
