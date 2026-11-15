// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package productspec_test

import (
	"slices"
	"strings"
	"testing"

	"example.org/appliance/internal/codes"
	"example.org/appliance/internal/productspec"
)

const withEmail = `format: 2
email:
  label: Password resets and one-time codes
  restart: [app/deployment/app-identity]
box_settings:
  - configmap: app/app-email
    keys:
      - {key: SMTP_HOST, setting: email.host}
      - {key: SMTP_PORT, setting: email.port}
      - {key: SMTP_TLS_MODE, setting: email.tls}
      - {key: SMTP_CA_PEM, setting: email.ca_pem}
box_secrets:
  - secret: app/app-email
    keys:
      - {key: SMTP_PASS, setting: email.password}
  - secret: app/app-identity
    keys:
      - {key: smtpConnectionURI, setting: email.uri}
      - {key: cipher, generate: password}
`

// A product declares the email settings it reads: the non-secret ones as
// box settings (a ConfigMap the box writes), the password and the URI it's
// in as box secrets, and the workloads that restart when they change.
func TestEmailSettingsParse(t *testing.T) {
	s, err := productspec.Parse([]byte(withEmail))
	if err != nil {
		t.Fatal(err)
	}
	if s.Email == nil || s.Email.Label == "" || len(s.Email.Restart) != 1 {
		t.Fatalf("email = %+v", s.Email)
	}
	if ns, kind, name, ok := s.Email.RestartRef(0); !ok || ns != "app" || kind != "deployment" || name != "app-identity" {
		t.Fatalf("restart = %s %s %s %v", ns, kind, name, ok)
	}
	if len(s.BoxSettings) != 1 || s.BoxSettings[0].Namespace() != "app" || s.BoxSettings[0].ConfigMapName() != "app-email" || len(s.BoxSettings[0].Keys) != 4 {
		t.Fatalf("box settings = %+v", s.BoxSettings)
	}
	if k := s.BoxSecrets[0].Keys[0]; k.Setting != productspec.SettingEmailPassword {
		t.Fatalf("the password key = %+v", k)
	}
	if !s.ReadsEmail() {
		t.Fatal("ReadsEmail = false")
	}
	if (productspec.Spec{}).ReadsEmail() {
		t.Fatal("a product with no email section reads email")
	}
}

// The password, and the URI it's in, never go into a ConfigMap; a setting
// the box doesn't offer, a key that's two kinds at once, and settings with
// no email section to restart their readers are refused.
func TestEmailSettingsAreRefusedWhenTheyBreakARule(t *testing.T) {
	for name, doc := range map[string]string{
		"the password in a ConfigMap": "format: 2\nemail: {}\nbox_settings:\n  - configmap: appliance/e\n    keys:\n      - {key: SMTP_PASS, setting: email.password}\n",
		"the URI in a ConfigMap":      "format: 2\nemail: {}\nbox_settings:\n  - configmap: appliance/e\n    keys:\n      - {key: URI, setting: email.uri}\n",
		"an unknown setting":          "format: 2\nemail: {}\nbox_settings:\n  - configmap: appliance/e\n    keys:\n      - {key: X, setting: email.colour}\n",
		"an unknown secret setting":   "format: 2\nemail: {}\nbox_secrets:\n  - secret: appliance/e\n    keys:\n      - {key: X, setting: box.fqdn}\n",
		"a setting and a value":       "format: 2\nemail: {}\nbox_secrets:\n  - secret: appliance/e\n    keys:\n      - {key: X, setting: email.password, value: x}\n",
		"a setting and a generator":   "format: 2\nemail: {}\nbox_secrets:\n  - secret: appliance/e\n    keys:\n      - {key: X, setting: email.password, generate: password}\n",
		"no email section":            "format: 2\nbox_settings:\n  - configmap: appliance/e\n    keys:\n      - {key: SMTP_HOST, setting: email.host}\n",
		"no email section, a secret":  "format: 2\nbox_secrets:\n  - secret: appliance/e\n    keys:\n      - {key: SMTP_PASS, setting: email.password}\n",
		"a bad configmap":             "format: 2\nemail: {}\nbox_settings:\n  - configmap: e\n    keys:\n      - {key: SMTP_HOST, setting: email.host}\n",
		"the appliance's namespace":   "format: 2\nemail: {}\nbox_settings:\n  - configmap: appliance/e\n    keys:\n      - {key: SMTP_HOST, setting: email.host}\n",
		"a configmap twice":           "format: 2\nemail: {}\nbox_settings:\n  - configmap: appliance/e\n    keys:\n      - {key: A, setting: email.host}\n  - configmap: appliance/e\n    keys:\n      - {key: B, setting: email.port}\n",
		"a key twice":                 "format: 2\nemail: {}\nbox_settings:\n  - configmap: appliance/e\n    keys:\n      - {key: A, setting: email.host}\n      - {key: A, setting: email.port}\n",
		"no keys":                     "format: 2\nemail: {}\nbox_settings:\n  - configmap: appliance/e\n",
		"a bad restart":               "format: 2\nemail:\n  restart: [appliance/pod/x]\n",
	} {
		if _, err := productspec.Parse([]byte(doc)); !codes.Is(err, codes.KitBundleMismatch) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestATelemetrySettingNeedsTheTelemetrySection(t *testing.T) {
	for name, doc := range map[string]string{
		"in a ConfigMap": "format: 2\nbox_settings:\n  - configmap: app/t\n    keys:\n      - {key: A, setting: telemetry.logs}\n",
		"in a Secret":    "format: 2\nbox_secrets:\n  - secret: app/t\n    keys:\n      - {key: A, setting: telemetry.logs}\n",
	} {
		if _, err := productspec.Parse([]byte(doc)); !codes.Is(err, codes.KitBundleMismatch) || !strings.Contains(err.Error(), "telemetry") {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Email's section doesn't stand in for telemetry's, nor the other way round.
	if _, err := productspec.Parse([]byte("format: 2\nemail: {}\nbox_settings:\n  - configmap: app/t\n    keys:\n      - {key: A, setting: telemetry.logs}\n")); err == nil {
		t.Error("the email section let a telemetry setting through")
	}
	if _, err := productspec.Parse([]byte("format: 2\ntelemetry: {}\nbox_settings:\n  - configmap: app/e\n    keys:\n      - {key: A, setting: email.host}\n")); err == nil {
		t.Error("the telemetry section let an email setting through")
	}
}

const withTelemetryPhases = `format: 2
telemetry:
  label: Traces
  status: appliance/appliance-gateway:9100
  egress: [appliance/appliance-extra]
  restart:
    - appliance/deployment/appliance-gateway
    - appliance/deployment/appliance-data
    - appliance/deployment/appliance-identity
    - appliance/deployment/appliance-audit
phases:
  - {name: data, label: D, stack: s-data, workloads: [appliance-postgres]}
  - {name: identity, label: I, stack: s-identity, workloads: [appliance-identity, appliance-data]}
  - {name: services, label: S, stack: s-services, workloads: [appliance-audit]}
  - {name: front, label: F, stack: s-front, workloads: [appliance-gateway]}
`

func TestTheTelemetryRestartFollowsThePhases(t *testing.T) {
	s, err := productspec.Parse([]byte(withTelemetryPhases))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, g := range s.Telemetry.Groups(s) {
		got = append(got, g.Phase+":"+strings.Join(g.Restart, ","))
	}
	want := []string{
		"identity:appliance/deployment/appliance-identity,appliance/deployment/appliance-data",
		"services:appliance/deployment/appliance-audit",
		"front:appliance/deployment/appliance-gateway",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the order: %q", got)
	}
	if ns, svc, ok := s.Telemetry.StatusRef(); !ok || ns != "appliance" || svc != "appliance-gateway:9100" {
		t.Fatalf("status: %q %q %v", ns, svc, ok)
	}
}

func TestATelemetryRestartWithNoPhasesIsOneGroup(t *testing.T) {
	s, err := productspec.Parse([]byte("format: 2\ntelemetry:\n  restart: [appliance/deployment/a, appliance/deployment/b]\n"))
	if err != nil {
		t.Fatal(err)
	}
	g := s.Telemetry.Groups(s)
	if len(g) != 1 || g[0].Phase != "" || len(g[0].Restart) != 2 {
		t.Fatalf("groups: %+v", g)
	}
}

func TestTelemetrySectionsAreRefusedWhenTheyBreakARule(t *testing.T) {
	for name, doc := range map[string]string{
		"a bad restart":   "format: 2\ntelemetry:\n  restart: [appliance/pod/x]\n",
		"no phase for it": strings.Replace(withTelemetryPhases, "appliance/deployment/appliance-audit", "appliance/deployment/appliance-other", 1),
		"a bad status":    "format: 2\ntelemetry:\n  status: gateway\n",
		"a bad egress":    "format: 2\ntelemetry:\n  egress: [extra]\n",
	} {
		if _, err := productspec.Parse([]byte(doc)); !codes.Is(err, codes.KitBundleMismatch) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheAppliancesAccountMayReadTheTelemetryStatus(t *testing.T) {
	s, err := productspec.Parse([]byte(withTelemetryPhases))
	if err != nil {
		t.Fatal(err)
	}
	if rbac := string(productspec.RBAC(s)); !strings.Contains(rbac, "services/proxy") || !strings.Contains(rbac, `"appliance-gateway:9100"`) {
		t.Fatalf("rbac:\n%s", rbac)
	}
}

func TestAProductThatReadsTelemetryNeedsABaseThatDoes(t *testing.T) {
	s, err := productspec.Parse([]byte("format: 2\ntelemetry:\n  label: Traces\n"))
	if err != nil {
		t.Fatal(err)
	}
	floor, why := s.BaseFloor()
	if floor != "0.1.0-rc.7" || !strings.Contains(why, "telemetry") {
		t.Fatalf("the floor: %q %q", floor, why)
	}
	plain, err := productspec.Parse([]byte("format: 2\nemail: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if floor, why := plain.BaseFloor(); floor != "" || why != "" {
		t.Fatalf("a product without telemetry has a floor: %q %q", floor, why)
	}
}
