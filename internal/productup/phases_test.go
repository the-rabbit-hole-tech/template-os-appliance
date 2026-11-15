// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package productup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"example.org/appliance/internal/productspec"
	"example.org/appliance/internal/productup"
)

// The phased slot: an always-on stack (a ConfigMap), the edge, a data
// phase (a StatefulSet) and a front phase (a Deployment, with the extra service
// switch's stack before it).
const phasedProduct = `format: 2
switches:
  - {name: extra, label: The extra service, stacks: [app-extra]}
phases:
  - {name: data, label: Starting the database, stack: app-data, workloads: [app-db], timeout: 3m}
  - {name: front, label: Starting the web app, stack: app-front, workloads: [app-web, app-extra], switch_stacks: [app-extra]}
`

func workloadDoc(kind, name, phase, order string) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: %s
metadata:
  name: %s
  namespace: app
  labels: {appliance/phase: %s, appliance/phase-order: "%s"}
spec:
  template:
    metadata:
      labels: {app: %s, appliance/phase: %s, appliance/phase-order: "%s"}
    spec:
      containers:
        - {name: main, image: "example.org/%s@sha256:%s"}
`, kind, name, phase, order, name, phase, order, name, hexA)
}

func phasedSlot(t *testing.T) string {
	t.Helper()
	slot := t.TempDir()
	writeSlot(t, slot, map[string]string{
		"product.yaml":                   phasedProduct,
		"manifests/app/app.yaml":         "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: app, namespace: app}\n",
		"manifests/edge/edge.yaml":       "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: edge, namespace: edge}\nspec:\n  template:\n    spec:\n      containers: [{name: edge, image: traefik}]\n",
		"manifests/app-data/data.yaml":   workloadDoc("StatefulSet", "app-db", "data", "1"),
		"manifests/app-front/web.yaml":   workloadDoc("Deployment", "app-web", "front", "2"),
		"manifests/app-extra/extra.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: app-extra-switch, namespace: app}\n---\n" + workloadDoc("Deployment", "app-extra", "front", "2"),
	})
	if err := productspec.WriteRBAC(slot); err != nil {
		t.Fatal(err)
	}
	return slot
}

// live is a workload in the fake cluster.
type live struct {
	kind, ns, name, stack string
	labels                map[string]string
	replicas              int64
	template              map[string]any
	ready                 bool
	// reason is why its pod's container waits, when it isn't ready.
	reason string
}

func (l *live) key() string { return l.kind + " " + l.ns + "/" + l.name }

// fakeCluster is k0s with a cluster behind it: the test applies a stack
// the way k0s's applier would (apply), and the pods follow the workloads'
// replicas; a pod scaled away stops (Terminating) and is gone at the next
// ask after that.
type fakeCluster struct {
	mu        sync.Mutex
	apiUp     bool
	applied   map[string]bool
	workloads map[string]*live
	stopping  map[string]int
	calls     []string
	// keepReplicas makes apply leave a scaled-down workload's replicas, as
	// an applier that merges against what it last applied would.
	keepReplicas bool
	// foreignSkip makes apply leave an object another stack owns, as
	// k0s's applier does.
	foreignSkip bool
	// stopAsks is how many asks a stopping pod outlasts (1 when unset).
	stopAsks int
	asked    map[string]int
	// scaledWhile records, at each scale to 0, the workloads whose pods
	// were still stopping.
	scaledWhile map[string][]string
	// configMaps are the ConfigMaps k0s has applied, "<namespace>/<name>",
	// with the box's values (fqdn) in them.
	configMaps map[string]map[string]string
	fqdn       string
}

func newCluster() *fakeCluster {
	return &fakeCluster{applied: map[string]bool{}, workloads: map[string]*live{}, stopping: map[string]int{}, asked: map[string]int{}, scaledWhile: map[string][]string{}, configMaps: map[string]map[string]string{}}
}

func (c *fakeCluster) add(l *live) { c.workloads[l.key()] = l }

// apply applies the slot's stack: its workloads take the slot's template
// and labels, and their replicas unless keepReplicas holds a scaled-down
// one.
func (c *fakeCluster) apply(t *testing.T, slot, stack string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	// A stack counts as applied once k0s has applied an object of it.
	took := false
	defer func() {
		if took {
			c.applied[stack] = true
		}
	}()
	files, _ := filepath.Glob(filepath.Join(slot, "manifests", stack, "*.yaml"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if c.fqdn != "" {
			b = bytes.ReplaceAll(b, []byte(boxPlaceholder), []byte(c.fqdn))
		}
		dec := yaml.NewDecoder(bytes.NewReader(b))
		for {
			var d map[string]any
			if err := dec.Decode(&d); err != nil {
				if !errors.Is(err, io.EOF) {
					t.Fatal(err)
				}
				break
			}
			kind, _ := d["kind"].(string)
			if kind == "ConfigMap" {
				md := d["metadata"].(map[string]any)
				data := map[string]string{}
				if m, ok := d["data"].(map[string]any); ok {
					for k, v := range m {
						data[k] = fmt.Sprint(v)
					}
				}
				c.configMaps[fmt.Sprint(md["namespace"])+"/"+fmt.Sprint(md["name"])] = data
			}
			if kind != "Deployment" && kind != "StatefulSet" {
				took = true
				continue
			}
			md := d["metadata"].(map[string]any)
			labels := map[string]string{productup.StackLabel: stack}
			if l, ok := md["labels"].(map[string]any); ok {
				for k, v := range l {
					labels[k] = fmt.Sprint(v)
				}
			}
			ns, _ := md["namespace"].(string)
			l := &live{kind: kind, ns: ns, name: md["name"].(string), stack: stack, labels: labels, replicas: 1,
				template: d["spec"].(map[string]any)["template"].(map[string]any)}
			if old, ok := c.workloads[l.key()]; ok && c.foreignSkip && old.stack != stack {
				// k0s's applier leaves an object another stack owns.
				continue
			}
			took = true
			if old, ok := c.workloads[l.key()]; ok {
				l.ready, l.reason = old.ready, old.reason
				if c.keepReplicas {
					l.replicas = old.replicas
				}
			}
			c.add(l)
		}
	}
}

func (c *fakeCluster) setReady(key string, ready bool, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workloads[key].ready, c.workloads[key].reason = ready, reason
}

func (c *fakeCluster) callsMatching(prefix string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, l := range c.calls {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func (c *fakeCluster) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(args) > 3 && args[0] == "kubectl" {
		args = args[3:]
	}
	line := strings.Join(args, " ")
	c.calls = append(c.calls, line)
	switch {
	case strings.Contains(line, "/readyz"):
		if !c.apiUp {
			return nil, errors.New("connection refused")
		}
		return []byte("ok"), nil
	case strings.Contains(line, "/proxy/"):
		return []byte("ok"), nil
	case strings.HasPrefix(line, "ctr "):
		return nil, nil
	case strings.HasPrefix(line, "scale "):
		// scale --namespace <ns> <kind>/<name> --replicas=<n>
		ns, ref, n := args[2], args[3], strings.TrimPrefix(args[4], "--replicas=")
		kind, name, _ := strings.Cut(ref, "/")
		k := map[string]string{"statefulset": "StatefulSet", "deployment": "Deployment"}[kind] + " " + ns + "/" + name
		w, ok := c.workloads[k]
		if !ok {
			return nil, errors.New("not found: " + k)
		}
		var r int64
		_, _ = fmt.Sscan(n, &r)
		if r == 0 {
			for sk, left := range c.stopping {
				if left > 0 && sk != k {
					c.scaledWhile[k] = append(c.scaledWhile[k], sk)
				}
			}
		}
		if r < w.replicas {
			c.stopping[k] += int(w.replicas - r)
		}
		if r == 0 {
			w.ready = false
		}
		w.replicas = r
		return []byte("scaled"), nil
	case strings.Contains(line, productup.StackLabel+"="):
		for s, ok := range c.applied {
			if !ok || !strings.Contains(line, productup.StackLabel+"="+s+" ") {
				continue
			}
			return []byte("configmap/" + s + "\n"), nil
		}
		return nil, nil
	case strings.HasPrefix(line, "delete "):
		// delete --namespace <ns> <kind>/<name> ...
		kind, name, _ := strings.Cut(args[3], "/")
		k := map[string]string{"statefulset": "StatefulSet", "deployment": "Deployment"}[kind] + " " + args[2] + "/" + name
		if _, ok := c.workloads[k]; !ok {
			return nil, errors.New("not found: " + k)
		}
		delete(c.workloads, k)
		return []byte("deleted"), nil
	case strings.HasPrefix(line, "get configmaps "):
		return c.configMapsJSON(), nil
	case strings.HasPrefix(line, "get pods"):
		return c.podsJSON(), nil
	case strings.HasPrefix(line, "get replicasets"):
		return c.replicaSetsJSON(), nil
	case strings.HasPrefix(line, "get deployments,statefulsets,daemonsets"):
		return c.workloadsJSON(), nil
	}
	return nil, errors.New("unexpected " + line)
}

func (c *fakeCluster) configMapsJSON() []byte {
	var items []any
	for k, data := range c.configMaps {
		ns, name, _ := strings.Cut(k, "/")
		items = append(items, map[string]any{"metadata": map[string]any{"namespace": ns, "name": name}, "data": data})
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return b
}

func (c *fakeCluster) workloadsJSON() []byte {
	var items []any
	for _, w := range c.sorted() {
		ready := int64(0)
		if w.ready {
			ready = w.replicas
		}
		labels := map[string]any{}
		for k, v := range w.labels {
			labels[k] = v
		}
		items = append(items, map[string]any{
			"kind":     w.kind,
			"metadata": map[string]any{"namespace": w.ns, "name": w.name, "generation": 1, "labels": labels},
			"spec":     map[string]any{"replicas": w.replicas, "template": w.template},
			"status": map[string]any{"observedGeneration": 1, "replicas": w.replicas, "updatedReplicas": w.replicas,
				"availableReplicas": ready, "readyReplicas": ready},
		})
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return b
}

func (c *fakeCluster) sorted() []*live {
	keys := make([]string, 0, len(c.workloads))
	for k := range c.workloads {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]*live, 0, len(keys))
	for _, k := range keys {
		out = append(out, c.workloads[k])
	}
	return out
}

// owner is the pod's owner reference: a Deployment's pods belong to its
// ReplicaSet, <name>-rs; a StatefulSet's to itself.
func owner(w *live) map[string]any {
	if w.kind == "Deployment" {
		return map[string]any{"kind": "ReplicaSet", "name": w.name + "-rs"}
	}
	return map[string]any{"kind": w.kind, "name": w.name}
}

// replicaSetsJSON is each Deployment's ReplicaSet, owned by it.
func (c *fakeCluster) replicaSetsJSON() []byte {
	var items []any
	for _, w := range c.sorted() {
		if w.kind == "Deployment" {
			items = append(items, map[string]any{"metadata": map[string]any{"namespace": w.ns, "name": w.name + "-rs",
				"ownerReferences": []any{map[string]any{"kind": "Deployment", "name": w.name}}}})
		}
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return b
}

// podsJSON is a pod per replica, with the template's labels and its
// owner, and the stopping ones; a pod that was stopping is gone after
// stopAsks asks.
func (c *fakeCluster) podsJSON() []byte {
	var items []any
	for _, w := range c.sorted() {
		labels := map[string]any{}
		if md, ok := w.template["metadata"].(map[string]any); ok {
			if l, ok := md["labels"].(map[string]any); ok {
				labels = l
			}
		}
		for i := int64(0); i < w.replicas; i++ {
			st := map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": map[bool]string{true: "True", false: "False"}[w.ready]}}}
			if !w.ready && w.reason != "" {
				st["containerStatuses"] = []any{map[string]any{"name": "main", "state": map[string]any{"waiting": map[string]any{"reason": w.reason, "message": "the image isn't on the box"}}}}
			}
			items = append(items, map[string]any{"metadata": map[string]any{"namespace": w.ns, "name": fmt.Sprintf("%s-%d", w.name, i), "labels": labels, "ownerReferences": []any{owner(w)}}, "status": st})
		}
		for i := 0; i < c.stopping[w.key()]; i++ {
			items = append(items, map[string]any{"metadata": map[string]any{"namespace": w.ns, "name": fmt.Sprintf("%s-old-%d", w.name, i), "labels": labels, "deletionTimestamp": "2026-10-10T18:00:00Z", "ownerReferences": []any{owner(w)}},
				"status": map[string]any{"phase": "Running"}})
		}
		if c.stopping[w.key()] > 0 {
			c.asked[w.key()]++
			if c.asked[w.key()] >= max(c.stopAsks, 1) {
				delete(c.stopping, w.key())
				delete(c.asked, w.key())
			}
		}
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return b
}

func phasedProbe(t *testing.T, c *fakeCluster) (*productup.Probe, string) {
	t.Helper()
	e := edge(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	d := t.TempDir()
	manifests := filepath.Join(d, "manifests")
	if err := os.MkdirAll(manifests, 0o755); err != nil {
		t.Fatal(err)
	}
	return &productup.Probe{Slot: phasedSlot(t), DataDir: "/var/lib/k0s", Containerd: "/run/k0s/containerd.sock",
		Edge: strings.TrimPrefix(e.URL, "https://"), Run: c.run, HTTP: e.Client(),
		SwitchDir: filepath.Join(d, "platform"), Manifests: manifests}, manifests
}

func placed(t *testing.T, manifests string) []string {
	t.Helper()
	var out []string
	for _, st := range []string{"app-data", "app-extra", "app-front"} {
		if _, err := os.Stat(filepath.Join(manifests, st)); err == nil {
			out = append(out, st)
		}
	}
	return out
}

// The steps of a phased slot: the box's own (k0s, images, quiesce, the
// cluster), one per phase with its label, then the product's health and
// the edge.
func TestThePhasedStepsFollowTheSlotsPhases(t *testing.T) {
	p, _ := phasedProbe(t, newCluster())
	var ids, labels []string
	for _, s := range p.Steps() {
		ids = append(ids, s.ID)
		labels = append(labels, s.Label)
	}
	want := []string{productup.StepK0s, productup.StepImages, productup.StepQuiesce, productup.StepCluster, "phase:data", "phase:front", productup.StepHealth, productup.StepEdge}
	if !slices.Equal(ids, want) {
		t.Fatalf("steps %v", ids)
	}
	if labels[4] != "Starting the database" || labels[5] != "Starting the web app" {
		t.Fatalf("labels %v", labels)
	}
	if !productup.IsStep("phase:data") || !productup.IsStep(productup.StepPods) || productup.IsStep("reboot") {
		t.Fatal("IsStep")
	}
	legacy := &productup.Probe{Slot: slot(t)}
	if got := legacy.Steps(); len(got) != len(productup.Steps) || got[3].ID != productup.StepPods {
		t.Fatalf("a slot without phases: %+v", got)
	}
}

// After a power loss every workload is still in etcd and its pods come
// back at once: the box first scales the product to 0 and waits for the
// pods to go, so each phase starts fresh pods; then the cluster's own
// stacks; then each phase in order, placed only once the one before is
// Ready, its workloads scaled back to the slot's replicas even when the
// applier keeps the scaled-down count.
func TestThePhasesComeUpInOrderAfterAPowerLoss(t *testing.T) {
	c := newCluster()
	c.keepReplicas = true
	p, manifests := phasedProbe(t, c)
	ctx := context.Background()
	step := func(want string) productup.Result {
		t.Helper()
		r, err := p.Check(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if r.Step != want {
			t.Fatalf("at %s: got %+v (placed %v)", want, r, placed(t, manifests))
		}
		return r
	}
	step(productup.StepK0s)
	c.apiUp = true
	// What the power loss left: both phases' workloads running, the edge too.
	c.apply(t, p.Slot, "app-data")
	c.apply(t, p.Slot, "app-front")
	c.setReady("StatefulSet app/app-db", true, "")
	c.setReady("Deployment app/app-web", true, "")
	r := step(productup.StepQuiesce)
	if !strings.Contains(r.Detail, "Stopping") {
		t.Fatalf("quiesce detail %q", r.Detail)
	}
	// One phase at a time, the latest first: the front, then, once its
	// pods are gone, the data.
	if got := c.callsMatching("scale "); len(got) != 1 {
		t.Fatalf("scaled %v", got)
	}
	if len(placed(t, manifests)) != 0 {
		t.Fatal("a phase was placed before the product was quiesced")
	}
	if r := step(productup.StepQuiesce); !strings.Contains(r.Detail, "stopping") {
		t.Fatalf("quiesce detail %q", r.Detail)
	}
	step(productup.StepQuiesce)
	if got := c.callsMatching("scale "); len(got) != 2 {
		t.Fatalf("scaled %v", got)
	}
	// The scaled-away pods are stopping, then gone; the cluster's stacks
	// aren't applied yet.
	if r := step(productup.StepQuiesce); !strings.Contains(r.Detail, "stopping") {
		t.Fatalf("quiesce detail %q", r.Detail)
	}
	r = step(productup.StepCluster)
	if !strings.Contains(r.Detail, "app") && !strings.Contains(r.Detail, "edge") {
		t.Fatalf("cluster detail %q", r.Detail)
	}
	c.apply(t, p.Slot, "app")
	c.apply(t, p.Slot, "edge")
	c.setReady("Deployment edge/edge", true, "")
	r = step("phase:data")
	if r.Timeout != 3*time.Minute {
		t.Fatalf("the data phase's timeout %v", r.Timeout)
	}
	if got := placed(t, manifests); !slices.Equal(got, []string{"app-data"}) {
		t.Fatalf("placed %v", got)
	}
	b, err := os.ReadFile(filepath.Join(p.SwitchDir, productup.PlacedFile))
	if err != nil || string(b) != "app-data\n" {
		t.Fatalf("the placed list %q %v", b, err)
	}
	// The earlier run's objects are still there at 0 and the applier keeps
	// that count: the box scales the slot's replicas back.
	step("phase:data")
	c.apply(t, p.Slot, "app-data")
	if got := c.callsMatching("scale --namespace app statefulset/app-db --replicas=1"); len(got) != 1 {
		t.Fatalf("the data phase wasn't scaled back: %v", c.callsMatching("scale "))
	}
	r = step("phase:data")
	if !strings.Contains(r.Detail, "app/app-db") {
		t.Fatalf("data detail %q", r.Detail)
	}
	if got := placed(t, manifests); !slices.Equal(got, []string{"app-data"}) {
		t.Fatalf("the front phase was placed before the data phase was Ready: %v", got)
	}
	c.setReady("StatefulSet app/app-db", true, "")
	step("phase:front")
	if got := placed(t, manifests); !slices.Equal(got, []string{"app-data", "app-front"}) {
		t.Fatalf("placed %v: the extra service is off", got)
	}
	c.apply(t, p.Slot, "app-front")
	step("phase:front")
	c.setReady("Deployment app/app-web", true, "")
	if r := step(""); r.Step != "" {
		t.Fatalf("up: %+v", r)
	}
}

// A switch's stack in a phase is placed, and applied, before the phase's
// own stack, so the workloads that read its ConfigMap start with it.
func TestAPhasesSwitchStackGoesFirst(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	p, manifests := phasedProbe(t, c)
	if err := os.MkdirAll(p.SwitchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.SwitchDir, "switches"), []byte("extra on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"app", "edge", "app-data"} {
		c.apply(t, p.Slot, st)
	}
	c.setReady("Deployment edge/edge", true, "")
	c.setReady("StatefulSet app/app-db", true, "")
	if err := os.MkdirAll(filepath.Join(manifests, "app-data"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if r, _ := p.Check(ctx); r.Step != "phase:front" || !slices.Equal(placed(t, manifests), []string{"app-data", "app-extra"}) {
		t.Fatalf("%+v placed %v", r, placed(t, manifests))
	}
	if r, _ := p.Check(ctx); r.Step != "phase:front" || slices.Contains(placed(t, manifests), "app-front") {
		t.Fatalf("the front stack was placed before the switch's stack was applied: %+v %v", r, placed(t, manifests))
	}
	c.apply(t, p.Slot, "app-extra")
	if r, _ := p.Check(ctx); r.Step != "phase:front" || !slices.Contains(placed(t, manifests), "app-front") {
		t.Fatalf("%+v placed %v", r, placed(t, manifests))
	}
}

// boxPlaceholder stands for the box's FQDN in the slot's stacks.
const boxPlaceholder = "app.box.invalid" // scrub:allow=fqdn -- the reserved .invalid placeholder, never resolved

// hostNameSlot adds to the phased slot what a host name reaches: the
// always-on stack's sign-in URLs and the extra switch's stack's issuer, both
// in ConfigMaps. The workloads' templates don't change with the name.
func hostNameSlot(t *testing.T, slot string) {
	t.Helper()
	writeSlot(t, slot, map[string]string{
		"manifests/app/app.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: app, namespace: app}\ndata:\n  OAUTH_PUBLIC_URL: https://" + boxPlaceholder + "\n  LOG_LEVEL: error\n---\n" +
			"apiVersion: v1\nkind: ConfigMap\nmetadata: {name: identity-config, namespace: app}\ndata:\n  identity.yaml: |\n    serve:\n      public:\n        base_url: https://" + boxPlaceholder + "/\n---\n" +
			"apiVersion: v1\nkind: ConfigMap\nmetadata: {name: other, namespace: app}\ndata:\n  NOTED: by something else\n",
		"manifests/app-extra/extra.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: app-extra-switch, namespace: app}\ndata:\n  OAUTH_ISSUER: https://" + boxPlaceholder + "/oauth\n  EXTRA_URL: https://" + boxPlaceholder + "/extra\n---\n" + workloadDoc("Deployment", "app-extra", "front", "2"),
		"box-values":                     boxPlaceholder + " box.fqdn\n",
	})
}

// A host name change on a running box with the extra switch on. The same version
// comes back with only ConfigMaps changed, so the stacks' objects are all
// there from the run before and every workload's template already matches:
// no workload may start until k0s has applied the new values, or it reads
// the old name for as long as it runs.
func TestAHostNameChangeReachesTheWorkloadsThroughThePhases(t *testing.T) {
	const was, now = "box1.appliance.example.org", "box2.appliance.example.org"
	c := newCluster()
	c.apiUp, c.keepReplicas = true, true
	p, manifests := phasedProbe(t, c)
	hostNameSlot(t, p.Slot)
	if err := os.MkdirAll(p.SwitchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.SwitchDir, "switches"), []byte("extra on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The run before: every stack applied with the old name, then stopped.
	c.fqdn = was
	for _, st := range []string{"app", "edge", "app-data", "app-extra", "app-front"} {
		c.apply(t, p.Slot, st)
	}
	c.setReady("Deployment edge/edge", true, "")
	for _, k := range []string{"StatefulSet app/app-db", "Deployment app/app-extra", "Deployment app/app-web"} {
		c.workloads[k].replicas = 0
	}
	c.configMaps["app/other"]["NOTED"] = "changed in the cluster"
	// The box recorded the new name; k0s-interim placed the always-on stack,
	// and k0s hasn't got to it.
	if err := os.WriteFile(filepath.Join(p.SwitchDir, "box-values"), []byte("box.fqdn "+now+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	check := func() productup.Result {
		t.Helper()
		r, err := p.Check(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for range 3 {
		if r := check(); r.Step != productup.StepCluster || !strings.Contains(r.Detail, "app") {
			t.Fatalf("the product went on with the old name in the always-on stack: %+v", r)
		}
	}
	if got := c.callsMatching("scale --namespace app statefulset"); len(got) != 0 {
		t.Fatalf("scaled %v", got)
	}
	c.fqdn = now
	c.apply(t, p.Slot, "app")
	// The data phase comes up as ever.
	check()
	c.apply(t, p.Slot, "app-data")
	check()
	c.setReady("StatefulSet app/app-db", true, "")
	// The switch's stack is placed; k0s hasn't applied it.
	if r := check(); r.Step != "phase:front" || !slices.Contains(placed(t, manifests), "app-extra") {
		t.Fatalf("%+v placed %v", r, placed(t, manifests))
	}
	for range 3 {
		if r := check(); r.Step != "phase:front" || !strings.Contains(r.Detail, "app-extra") || slices.Contains(placed(t, manifests), "app-front") {
			t.Fatalf("the phase went on with the old issuer: %+v placed %v", r, placed(t, manifests))
		}
	}
	if got := c.callsMatching("scale --namespace app deployment"); len(got) != 0 {
		t.Fatalf("a front workload started with the old name: %v", got)
	}
	c.apply(t, p.Slot, "app-extra")
	check()
	c.apply(t, p.Slot, "app-front")
	for range 6 {
		check()
	}
	for _, k := range []string{"Deployment app/app-extra", "Deployment app/app-web"} {
		if c.workloads[k].replicas != 1 {
			t.Fatalf("%s at %d", k, c.workloads[k].replicas)
		}
		c.setReady(k, true, "")
	}
	if r := check(); r.Step != "" {
		t.Fatalf("up: %+v", r)
	}
	for cm, want := range map[string]map[string]string{
		"app/app":              {"OAUTH_PUBLIC_URL": "https://" + now},
		"app/app-extra-switch": {"OAUTH_ISSUER": "https://" + now + "/oauth", "EXTRA_URL": "https://" + now + "/extra"},
	} {
		for k, v := range want {
			if got := c.configMaps[cm][k]; got != v {
				t.Fatalf("%s %s = %q", cm, k, got)
			}
		}
	}
	if got := c.configMaps["app/identity-config"]["identity.yaml"]; !strings.Contains(got, "https://"+now+"/") || strings.Contains(got, was) {
		t.Fatalf("the identity service URLs %q", got)
	}
}

// A ConfigMap of the stack that carries no box value is k0s's business
// alone: something else writing to it never holds the product.
func TestAConfigMapWithNoBoxValueNeverHoldsThePhases(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	p, _ := phasedProbe(t, c)
	hostNameSlot(t, p.Slot)
	if err := os.MkdirAll(p.SwitchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.SwitchDir, "box-values"), []byte("box.fqdn box1.appliance.example.org\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.fqdn = "box1.appliance.example.org"
	c.apply(t, p.Slot, "app")
	c.apply(t, p.Slot, "edge")
	c.setReady("Deployment edge/edge", true, "")
	c.configMaps["app/other"]["NOTED"] = "changed in the cluster"
	if r, err := p.Check(context.Background()); err != nil || r.Step != "phase:data" {
		t.Fatalf("%+v %v", r, err)
	}
}

// A pod that can't ever start (its image isn't on the box, its config is
// wrong) fails the phase at once, naming the workload; a crash loop waits
// for the phase's timeout, since a service that retries may still come up.
func TestAPhaseFailsOnAPodThatCantStart(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	p, manifests := phasedProbe(t, c)
	for _, st := range []string{"app", "edge", "app-data"} {
		c.apply(t, p.Slot, st)
	}
	c.setReady("Deployment edge/edge", true, "")
	if err := os.MkdirAll(filepath.Join(manifests, "app-data"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c.setReady("StatefulSet app/app-db", false, "CrashLoopBackOff")
	if r, _ := p.Check(ctx); r.Step != "phase:data" || r.Failed || !strings.Contains(r.Detail, "app/app-db") || !strings.Contains(r.Detail, "CrashLoopBackOff") {
		t.Fatalf("a crash loop: %+v", r)
	}
	c.setReady("StatefulSet app/app-db", false, "ErrImageNeverPull")
	r, _ := p.Check(ctx)
	if r.Step != "phase:data" || !r.Failed || !strings.Contains(r.Detail, "app/app-db") || !strings.Contains(r.Detail, "ErrImageNeverPull") {
		t.Fatalf("an image that isn't there: %+v", r)
	}
}

// Quiesce, the k0s service's pre-stop, stops the product latest phase
// first, each phase's pods gone before the next is scaled down, so the
// database stops last, with nothing connected; what isn't phased (the
// edge) keeps running.
func TestQuiesceStopsThePhasesInReverseOrder(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	p, _ := phasedProbe(t, c)
	for _, st := range []string{"edge", "app-data", "app-front"} {
		c.apply(t, p.Slot, st)
	}
	if err := p.Quiesce(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	got := c.callsMatching("scale ")
	want := []string{"scale --namespace app deployment/app-web --replicas=0", "scale --namespace app statefulset/app-db --replicas=0"}
	if !slices.Equal(got, want) {
		t.Fatalf("scaled %v, want %v", got, want)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	webAt, dbAt, podsBetween := -1, -1, 0
	for i, l := range c.calls {
		switch {
		case l == want[0]:
			webAt = i
		case l == want[1]:
			dbAt = i
		case webAt >= 0 && dbAt < 0 && strings.HasPrefix(l, "get pods"):
			podsBetween++
		}
	}
	if podsBetween == 0 || webAt > dbAt {
		t.Fatalf("the database was scaled down before the web app's pods were gone: %v", c.calls)
	}
	if c.workloads["Deployment edge/edge"].replicas != 1 {
		t.Fatal("the edge was stopped")
	}
}

// With the cluster's API down there's nothing to stop: Quiesce returns at
// once, and the stop goes on.
func TestQuiesceWithTheAPIDownDoesNothing(t *testing.T) {
	c := newCluster()
	p, _ := phasedProbe(t, c)
	if err := p.Quiesce(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := c.callsMatching("scale "); len(got) != 0 {
		t.Fatalf("scaled %v", got)
	}
}

// An update from a product without phases: its workloads sit in the
// always-on stack, which the new slot's always-on stack no longer holds.
// They're stopped with the rest before the first phase starts, so the new
// database never starts next to the old one on the same data.
func TestAnEarlierVersionsUnphasedWorkloadsStopFirst(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	p, manifests := phasedProbe(t, c)
	c.add(&live{kind: "StatefulSet", ns: "app", name: "app-db", stack: "app", labels: map[string]string{productup.StackLabel: "app"}, replicas: 1, template: map[string]any{}, ready: true})
	ctx := context.Background()
	if r, _ := p.Check(ctx); r.Step != productup.StepQuiesce {
		t.Fatalf("%+v", r)
	}
	if got := c.callsMatching("scale "); !slices.Equal(got, []string{"scale --namespace app statefulset/app-db --replicas=0"}) {
		t.Fatalf("scaled %v", got)
	}
	if len(placed(t, manifests)) != 0 {
		t.Fatal("a phase was placed while the old database ran")
	}
}

// While an import is open the product holds after the phase import.after
// names: the later phases' workloads are scaled to 0 and the answer says
// held, at the next phase's step; once it's closed they come back.
func TestAnOpenImportHoldsAfterItsPhase(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	p, manifests := phasedProbe(t, c)
	product := strings.Replace(phasedProduct, "switches:\n", "switches:\n  - {name: import, label: Import, stacks: [app-import]}\n", 1) +
		"exposed_values:\n  - {name: setup, secret: app/s, key: K, roles: [admin], one_time: true, consumed_when: {service: app/gw:http, path: /setup/state, field: needsSetup, equals: false}}\n" +
		"import: {switch: import, job: import/job.yaml, uid: 65532, setup: setup, after: data}\n"
	writeSlot(t, p.Slot, map[string]string{"product.yaml": product, "manifests/app-import/sa.yaml": "apiVersion: v1\nkind: ServiceAccount\nmetadata: {name: migrate, namespace: app}\n"})
	if err := productspec.WriteRBAC(p.Slot); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"app", "edge", "app-data", "app-front"} {
		c.apply(t, p.Slot, st)
		if err := os.MkdirAll(filepath.Join(manifests, st), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []string{"Deployment edge/edge", "StatefulSet app/app-db", "Deployment app/app-web"} {
		c.setReady(k, true, "")
	}
	ctx := context.Background()
	if r, _ := p.Check(ctx); r.Step != "" {
		t.Fatalf("up with the import closed: %+v", r)
	}
	if err := os.MkdirAll(p.SwitchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.SwitchDir, "switches"), []byte("import on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.apply(t, p.Slot, "app-import")
	r, _ := p.Check(ctx)
	if r.Step != "phase:front" || !r.Held || r.Timeout != 0 {
		t.Fatalf("an open import: %+v", r)
	}
	if got := c.callsMatching("scale --namespace app deployment/app-web --replicas=0"); len(got) != 1 {
		t.Fatalf("the front phase wasn't stopped: %v", c.callsMatching("scale "))
	}
	if c.workloads["StatefulSet app/app-db"].replicas != 1 {
		t.Fatal("the data phase was stopped")
	}
	if err := os.WriteFile(filepath.Join(p.SwitchDir, "switches"), []byte("import off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r, _ := p.Check(ctx); r.Step != "phase:front" || r.Held {
		t.Fatalf("the import closed: %+v", r)
	}
	if got := c.callsMatching("scale --namespace app deployment/app-web --replicas=1"); len(got) != 1 {
		t.Fatalf("the front phase wasn't started again: %v", c.callsMatching("scale "))
	}
}

// The cluster step waits only for the cluster's own workloads (k0s's
// stacks: CoreDNS, kube-router) and what the always-on stacks declare
// (the edge), never for a product workload that runs in a phase's stack
// without its labels.
func TestTheClusterStepIgnoresTheProductsWorkloads(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	p, _ := phasedProbe(t, c)
	for _, st := range []string{"app", "edge"} {
		c.apply(t, p.Slot, st)
	}
	c.setReady("Deployment edge/edge", true, "")
	c.add(&live{kind: "Deployment", ns: "kube-system", name: "coredns", stack: "coredns", labels: map[string]string{productup.StackLabel: "coredns"}, replicas: 1, template: map[string]any{}, ready: false})
	ctx := context.Background()
	if r, _ := p.Check(ctx); r.Step != productup.StepCluster || !strings.Contains(r.Detail, "kube-system/coredns") {
		t.Fatalf("CoreDNS not Ready: %+v", r)
	}
	c.setReady("Deployment kube-system/coredns", true, "")
	// A product workload at 0 in a phase's stack, unlabelled, which only
	// its phase may start.
	c.add(&live{kind: "Deployment", ns: "app", name: "app-extra", stack: "app-extra", labels: map[string]string{productup.StackLabel: "app-extra"}, replicas: 0, template: map[string]any{}})
	c.workloads["Deployment app/app-extra"].replicas = 0
	if r, _ := p.Check(ctx); r.Step != "phase:data" {
		t.Fatalf("%+v", r)
	}
}

// The quiesce at the start of a boot stops one phase at a time too: the
// latest first, and the next only once its pods are gone.
func TestTheBootQuiesceStopsOnePhaseAtATime(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	c.stopAsks = 2
	p, _ := phasedProbe(t, c)
	c.apply(t, p.Slot, "app-data")
	c.apply(t, p.Slot, "app-front")
	ctx := context.Background()
	if r, _ := p.Check(ctx); r.Step != productup.StepQuiesce {
		t.Fatalf("%+v", r)
	}
	if got := c.callsMatching("scale "); !slices.Equal(got, []string{"scale --namespace app deployment/app-web --replicas=0"}) {
		t.Fatalf("the first pass scaled %v, want the front phase only", got)
	}
	for range 4 {
		if r, _ := p.Check(ctx); r.Step != productup.StepQuiesce && r.Step != productup.StepCluster {
			t.Fatalf("%+v", r)
		}
	}
	if got := c.callsMatching("scale "); len(got) != 2 || got[1] != "scale --namespace app statefulset/app-db --replicas=0" {
		t.Fatalf("scaled %v, want the data phase second", got)
	}
	for k, while := range c.scaledWhile {
		t.Errorf("%s was scaled down while %v still stopped", k, while)
	}
}

// An update from a bundle without phases: its workloads belong to its one
// stack, and k0s's applier leaves an object another stack owns, so a phase
// whose stack holds one of them would never be applied. Before it places a
// phase's stack the box deletes those (quiesced already, at 0; their data is
// on the box's own volumes), and the phase's stack makes them anew.
func TestAPhaseTakesItsWorkloadsOverFromAnotherStack(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	c.foreignSkip = true
	p, manifests := phasedProbe(t, c)
	for _, st := range []string{"app", "edge"} {
		c.apply(t, p.Slot, st)
	}
	c.setReady("Deployment edge/edge", true, "")
	// The earlier version's database, in the always-on stack, quiesced.
	c.add(&live{kind: "StatefulSet", ns: "app", name: "app-db", stack: "app", labels: map[string]string{productup.StackLabel: "app"}, replicas: 0, template: map[string]any{}})
	if err := os.MkdirAll(filepath.Join(manifests, "app-data"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := os.Remove(filepath.Join(manifests, "app-data")); err != nil {
		t.Fatal(err)
	}
	// The cluster step done, the data phase: the old object goes first.
	r, err := p.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Step != "phase:data" || !strings.Contains(r.Detail, "app/app-db") {
		t.Fatalf("%+v", r)
	}
	if got := c.callsMatching("delete "); len(got) != 1 || !strings.Contains(got[0], "statefulset/app-db") {
		t.Fatalf("deleted %v", got)
	}
	if _, err := os.Stat(filepath.Join(manifests, "app-data")); err == nil {
		t.Fatal("the stack was placed while another stack owned its workload")
	}
	if r, _ = p.Check(ctx); r.Step != "phase:data" {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(manifests, "app-data")); err != nil {
		t.Fatal("the stack wasn't placed once its workload was free")
	}
	c.apply(t, p.Slot, "app-data")
	if w := c.workloads["StatefulSet app/app-db"]; w == nil || w.stack != "app-data" {
		t.Fatalf("the data phase's stack doesn't hold app-db: %+v", w)
	}
}

// A k0s command that outlives its bound never holds the probe: it's
// killed with every process it started (one that left a child holding
// its output, here), and the probe answers within the bound, so the
// box's phase loop keeps going.
func TestACommandThatHangsNeverHoldsTheProbe(t *testing.T) {
	p, _ := phasedProbe(t, newCluster())
	p.Run = nil
	p.AskTimeout = 300 * time.Millisecond
	if err := os.WriteFile(filepath.Join(p.Slot, "k0s"), []byte("#!/bin/sh\nsleep 60 &\nsleep 60\n"), 0o755); err != nil { // #nosec G306 -- a test's fake k0s
		t.Fatal(err)
	}
	started := time.Now()
	r, err := p.Check(context.Background())
	if took := time.Since(started); took > 10*time.Second {
		t.Fatalf("the probe was held %s by a command bounded at %s", took, p.AskTimeout)
	}
	if err != nil || r.Step != productup.StepK0s {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRolledOutSaysWhereAWorkloadIs(t *testing.T) {
	c := newCluster()
	c.apiUp = true
	c.add(&live{kind: "Deployment", ns: "app", name: "app-web", replicas: 1, ready: true})
	c.add(&live{kind: "Deployment", ns: "app", name: "app-slow", replicas: 1})
	p := &productup.Probe{Run: c.run}
	ctx := context.Background()
	if why, present, err := p.RolledOut(ctx, "app", "deployment", "app-web"); err != nil || !present || why != "" {
		t.Fatalf("a workload that's up: %q %v %v", why, present, err)
	}
	if why, present, err := p.RolledOut(ctx, "app", "deployment", "app-slow"); err != nil || !present || why == "" {
		t.Fatalf("a workload that's down: %q %v %v", why, present, err)
	}
	if _, present, err := p.RolledOut(ctx, "app", "deployment", "app-off"); err != nil || present {
		t.Fatalf("a workload that isn't there: %v %v", present, err)
	}
}
