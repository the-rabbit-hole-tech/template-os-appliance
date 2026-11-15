// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const release = `apiVersion: appliance/v1alpha1
kind: Release
spec:
  services:
    gateway:
      image: registry.example.org/appliance-gateway
      digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
    extra:
      image: registry.example.org/appliance-extra
      digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
  thirdParty:
    postgres:
      image: docker.io/library/postgres
      digest: sha256:3333333333333333333333333333333333333333333333333333333333333333
`

const productYAML = `format: 2
box_secrets:
  - secret: app/app-bundled
    keys: [{key: password, generate: password}]
  - secret: app/app-box
    keys: [{key: KEK, generate: key32}]
`

const emailProductYAML = productYAML + `  - secret: app/app-mail
    keys: [{key: SMTP_PASS, setting: email.password}]
email: {}
box_settings:
  - configmap: app/app-email
    keys: [{key: SMTP_HOST, setting: email.host}]
  - configmap: app/app-other
    keys: [{key: X, setting: email.port}]
`

// off is what helm renders with the switch off: a Deployment reading a
// box secret, the chart's own generated Secret, a PostgreSQL StatefulSet
// with a volume claim, an Ingress with a host and TLS, and the gateway's
// ConfigMap.
const off = `apiVersion: v1
kind: Secret
metadata: {name: app-bundled}
data: {password: c2VjcmV0}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: app-gateway}
data: {EXTRA_ENABLED: "false", LOG_LEVEL: error}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: data}
spec: {}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: app-gateway}
spec:
  template:
    spec:
      containers:
        - name: gateway
          image: "registry.example.org/appliance-gateway:0.1.0"
          env:
            - name: PGPASSWORD
              valueFrom: {secretKeyRef: {name: app-bundled, key: password}}
            - name: KEK
              valueFrom: {secretKeyRef: {name: app-box, key: KEK}}
---
apiVersion: apps/v1
kind: StatefulSet
metadata: {name: app-postgres}
spec:
  template:
    spec:
      securityContext: {runAsUser: 999}
      containers:
        - name: postgres
          image: "postgres:18.6@sha256:3333333333333333333333333333333333333333333333333333333333333333"
  volumeClaimTemplates:
    - metadata: {name: data}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: app-gateway}
spec:
  tls: [{hosts: [appliance.example.org], secretName: tls}]
  rules:
    - host: appliance.example.org
      http: {paths: []}
`

// on adds the extra service and changes the gateway's extra settings.
const on = off + `---
apiVersion: apps/v1
kind: Deployment
metadata: {name: app-extra}
spec:
  template:
    spec:
      containers:
        - name: extra
          image: registry.example.org/appliance-extra:0.1.0
`

func run(t *testing.T, offDoc, onDoc, product string) (string, string, error) {
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
		Off: w("off.yaml", offDoc), On: w("on.yaml", strings.Replace(onDoc, `EXTRA_ENABLED: "false"`, `EXTRA_ENABLED: "true"`, 1)),
		Out: filepath.Join(dir, "stacks"), Namespace: "app", Stack: "app", SwitchStack: "app-extra",
		SwitchConfigMap: "app-extra-switch", SwitchFrom: []string{"app-gateway"}, Data: "/var/lib/appliance-data",
	}
	var log bytes.Buffer
	if err := render(o, &log); err != nil {
		return "", "", err
	}
	main, err := os.ReadFile(filepath.Join(o.Out, "app", "app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sw, err := os.ReadFile(filepath.Join(o.Out, "app-extra", "app-extra.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(main), string(sw), nil
}

func docs(t *testing.T, s string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	dec := yaml.NewDecoder(strings.NewReader(s))
	for {
		var d map[string]any
		if err := dec.Decode(&d); err != nil {
			break
		}
		md := d["metadata"].(map[string]any)
		out[d["kind"].(string)+"/"+md["name"].(string)] = d
	}
	return out
}

func TestRenderMakesTheBoxStacks(t *testing.T) {
	main, sw, err := run(t, off, on, productYAML)
	if err != nil {
		t.Fatal(err)
	}
	m := docs(t, main)
	if _, ok := m["Secret/app-bundled"]; ok || strings.Contains(main, "c2VjcmV0") {
		t.Fatal("the chart's generated Secret is in the stack; the box makes it")
	}
	if _, ok := m["PersistentVolumeClaim/data"]; ok {
		t.Fatal("a PersistentVolumeClaim: the box has no provisioner")
	}
	if _, ok := m["Namespace/app"]; !ok {
		t.Fatal("no Namespace")
	}
	gw := m["Deployment/app-gateway"]
	if gw["metadata"].(map[string]any)["namespace"] != "app" {
		t.Fatal("no namespace on the Deployment")
	}
	c := gw["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	if c["image"] != "registry.example.org/appliance-gateway@sha256:1111111111111111111111111111111111111111111111111111111111111111" || c["imagePullPolicy"] != "Never" {
		t.Fatalf("the gateway image %v %v", c["image"], c["imagePullPolicy"])
	}
	pg := m["StatefulSet/app-postgres"]["spec"].(map[string]any)
	if _, ok := pg["volumeClaimTemplates"]; ok {
		t.Fatal("volumeClaimTemplates are kept")
	}
	ps := pg["template"].(map[string]any)["spec"].(map[string]any)
	v := ps["volumes"].([]any)[0].(map[string]any)
	if v["name"] != "data" || v["hostPath"].(map[string]any)["path"] != "/var/lib/appliance-data/postgres" {
		t.Fatalf("the volume %v", v)
	}
	ic := ps["initContainers"].([]any)[0].(map[string]any)
	if !strings.Contains(strings.Join(toStrings(ic["command"]), " "), "chown 999:999 /data") || ic["image"] != "docker.io/library/postgres@sha256:3333333333333333333333333333333333333333333333333333333333333333" {
		t.Fatalf("the owner init %v", ic)
	}
	ing := m["Ingress/app-gateway"]["spec"].(map[string]any)
	if _, ok := ing["tls"]; ok || strings.Contains(main, "appliance.example.org") {
		t.Fatal("the Ingress keeps its host or TLS; the box serves every name on 443")
	}
	if _, ok := m["Deployment/app-extra"]; ok {
		t.Fatal("the extra service is in the always-on stack")
	}
	s := docs(t, sw)
	if _, ok := s["Deployment/app-extra"]; !ok {
		t.Fatal("the extra service isn't in the switch stack")
	}
	cm := s["ConfigMap/app-extra-switch"]["data"].(map[string]any)
	if len(cm) != 1 || cm["EXTRA_ENABLED"] != "true" {
		t.Fatalf("the switch ConfigMap %v", cm)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestRenderRefuses(t *testing.T) {
	for name, tc := range map[string]struct{ off, product, want string }{
		"a Secret no box makes":             {off + "---\napiVersion: v1\nkind: Secret\nmetadata: {name: app-lab}\ndata: {a: Yg==}\n", productYAML, "Secret app-lab"},
		"an unpinned image":                 {strings.Replace(off, "postgres:18.6@", "redis:7@", 1), productYAML, "redis"},
		"a Secret read that no box makes":   {strings.Replace(off, "name: app-box, key: KEK", "name: app-lab-secrets, key: KEK", 1), productYAML, "app-lab-secrets"},
		"a box secret key not declared":     {strings.Replace(off, "name: app-box, key: KEK", "name: app-box, key: TOTP", 1), productYAML, "app-box"},
		"the relay password in a ConfigMap": {off + "---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: appliance-identity}\ndata: {SMTP_PASS: x}\n", emailProductYAML, "SMTP_PASS"},
		"a pod repeating a container name":  {off + "---\napiVersion: apps/v1\nkind: Deployment\nmetadata: {name: app-dup}\nspec:\n  template:\n    spec:\n      initContainers:\n        - {name: dup-step, image: \"postgres:18.6\"}\n      containers:\n        - {name: dup-step, image: \"postgres:18.6\"}\n", productYAML, "dup-step"},
		"a ConfigMap the box writes":        {off + "---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: app-email}\ndata: {SMTP_HOST: smtp.example.org}\n", emailProductYAML, "app-email"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := run(t, tc.off, tc.off+strings.TrimPrefix(on, off), tc.product)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want a refusal naming %s", err, tc.want)
			}
		})
	}
	// Two switch ConfigMaps that disagree on one setting.
	twice := off + "---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: app-web-front}\ndata: {EXTRA_ENABLED: \"false\"}\n"
	onTwice := strings.Replace(twice+strings.TrimPrefix(on, off), `EXTRA_ENABLED: "false"}`, `EXTRA_ENABLED: "maybe"}`, 1)
	dir := t.TempDir()
	w := func(name, body string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	o := options{Release: w("r", release), ProductYAML: w("p", productYAML), Off: w("off", twice),
		On:  w("on", strings.Replace(onTwice, `EXTRA_ENABLED: "false", LOG_LEVEL`, `EXTRA_ENABLED: "true", LOG_LEVEL`, 1)),
		Out: filepath.Join(dir, "out"), Namespace: "app", Stack: "app", SwitchStack: "app-extra", SwitchConfigMap: "app-extra-switch",
		SwitchFrom: []string{"app-gateway", "app-web-front"}, Data: "/var/lib/appliance-data"}
	if err := render(o, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "EXTRA_ENABLED") {
		t.Fatalf("two switch settings that disagree: %v", err)
	}
}

// A product without a switch (no --switch-stack) has one render and gets
// the always-on stack only.
func TestRenderWithoutASwitch(t *testing.T) {
	dir := t.TempDir()
	w := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	o := options{Release: w("release.yaml", release), ProductYAML: w("product.yaml", productYAML), Off: w("off.yaml", off),
		Out: filepath.Join(dir, "stacks"), Namespace: "app", Stack: "app", Data: "/var/lib/appliance-data"}
	if err := render(o, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(o.Out)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "app" {
		t.Fatalf("the stacks %v; want the always-on one only", ents)
	}
	b, err := os.ReadFile(filepath.Join(o.Out, "app", "app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := docs(t, string(b))
	if _, ok := m["StatefulSet/app-postgres"]; !ok {
		t.Fatal("the always-on stack has no PostgreSQL")
	}
	if _, ok := m["ConfigMap/"]; ok {
		t.Fatal("a switch ConfigMap without a switch")
	}
}

// A claim the product declares for its backups is the backup partition's,
// whichever workload mounts it, and every other claim stays on the data
// directory.
func TestTheBackupClaimGoesToTheBackupPartition(t *testing.T) {
	pins := map[string]string{"docker.io/library/postgres": "sha256:3333333333333333333333333333333333333333333333333333333333333333"}
	const src = `apiVersion: batch/v1
kind: CronJob
metadata: {name: appliance-pgha-backup-verify}
spec:
  jobTemplate:
    spec:
      template:
        spec:
          securityContext: {runAsUser: 999}
          containers:
            - name: verify
              image: "postgres:18.6"
          volumes:
            - name: backup
              persistentVolumeClaim: {claimName: appliance-pgha-backup, readOnly: true}
            - name: other
              persistentVolumeClaim: {claimName: another}
`
	var d doc
	if err := yaml.Unmarshal([]byte(src), &d); err != nil {
		t.Fatal(err)
	}
	o := options{Namespace: "appliance", Data: "/var/lib/appliance-data", backupClaim: "appliance-pgha-backup", backupDir: "/var/lib/appliance/backup/postgres"}
	if err := pod(o, d, "CronJob", "appliance-pgha-backup-verify", pins, nil, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	ps := dig(d, "spec", "jobTemplate", "spec", "template", "spec")
	vols := ps["volumes"].([]any)
	if got := dig(vols[0].(doc), "hostPath")["path"]; got != "/var/lib/appliance/backup/postgres" {
		t.Fatalf("the backup claim went to %v", got)
	}
	if got := dig(vols[1].(doc), "hostPath")["path"]; got != "/var/lib/appliance-data/pgha-backup-verify" {
		t.Fatalf("another claim went to %v", got)
	}
	if len(ps["initContainers"].([]any)) != 2 {
		t.Fatalf("each volume needs its owner set: %v", ps["initContainers"])
	}
}

// A pod with two claims gets an owner step for each, and each step has its
// own name: the API server refuses a pod whose containers share one, so k0s
// never creates the workload.
func TestEachOwnerStepHasItsOwnName(t *testing.T) {
	pins := map[string]string{"docker.io/library/postgres": "sha256:3333333333333333333333333333333333333333333333333333333333333333"}
	const src = `apiVersion: apps/v1
kind: StatefulSet
metadata: {name: appliance-pgha}
spec:
  template:
    spec:
      securityContext: {runAsUser: 999}
      initContainers:
        - name: tools
          image: "postgres:18.6"
      containers:
        - name: postgres
          image: "postgres:18.6"
      volumes:
        - name: backup
          persistentVolumeClaim: {claimName: appliance-pgha-backup}
  volumeClaimTemplates:
    - metadata: {name: data}
`
	var d doc
	if err := yaml.Unmarshal([]byte(src), &d); err != nil {
		t.Fatal(err)
	}
	o := options{Namespace: "appliance", Data: "/var/lib/appliance-data", backupClaim: "appliance-pgha-backup", backupDir: "/var/lib/appliance/backup/postgres"}
	if err := pod(o, d, "StatefulSet", "appliance-pgha", pins, nil, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	mounts := map[string]bool{}
	for _, c := range dig(d, "spec", "template", "spec")["initContainers"].([]any) {
		cm := c.(doc)
		name, _ := cm["name"].(string)
		if seen[name] {
			t.Fatalf("two init containers are named %s", name)
		}
		seen[name] = true
		if name == "tools" {
			continue
		}
		mounts[cm["volumeMounts"].([]any)[0].(doc)["name"].(string)] = true
	}
	if !mounts["data"] || !mounts["backup"] {
		t.Fatalf("each claim needs its owner step: %v", mounts)
	}
}
