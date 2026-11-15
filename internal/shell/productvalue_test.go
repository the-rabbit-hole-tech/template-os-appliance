// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package shell_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"

	accessv1 "example.org/appliance/gen/go/appliance/access/v1"
	"example.org/appliance/gen/go/appliance/access/v1/accessv1connect"
	osadminv1 "example.org/appliance/gen/go/appliance/osadmin/v1"
	"example.org/appliance/internal/productspec"
	"example.org/appliance/internal/shell"
)

type valueAccessd struct {
	fakeAccessd
	answer *osadminv1.GetExposedValueResponse
	asked  []string
}

func (f *valueAccessd) GetExposedValue(_ context.Context, r *connect.Request[accessv1.GetExposedValueRequest]) (*connect.Response[accessv1.GetExposedValueResponse], error) {
	f.saw(r.Header())
	f.asked = append(f.asked, r.Msg.GetName())
	return connect.NewResponse(&accessv1.GetExposedValueResponse{Value: f.answer}), nil
}

var spec = productspec.Spec{Format: 2, ExposedValues: []productspec.ExposedValue{
	{Name: "setup-token", Secret: "appliance/appliance-setup-token", Key: "SETUP_TOKEN", Roles: []string{"owner", "admin"}, Label: "Appliance setup token"},
	{Name: "support-id", Secret: "appliance/appliance-support", Key: "SUPPORT_ID", Roles: []string{"owner"}},
}}

// The values a product exposes are its own commands, "<product> <name>",
// offered only to the roles the bundle lists; each one asks accessd.
func TestAProductsExposedValuesAreItsCommands(t *testing.T) {
	admin := shell.ValuesFor(spec, "admin")
	names := shell.NamesFor(shell.OriginSSH, appliance, admin...)
	if !slices.Contains(names, "appliance setup-token") || slices.Contains(names, "appliance support-id") {
		t.Fatalf("an admin's names %v", names)
	}
	owner := shell.ValuesFor(spec, "owner")
	if !slices.Contains(shell.NamesFor(shell.OriginSSH, appliance, owner...), "appliance support-id") {
		t.Fatal("an owner isn't offered support-id")
	}
	if got := shell.CompleteFor(shell.OriginSSH, appliance, "appliance s", admin...); got != "appliance setup-token " {
		t.Fatalf("completes %q", got)
	}
	if slices.Contains(shell.NamesFor(shell.OriginConsole, appliance, owner...), "appliance setup-token") {
		t.Fatal("the console offers a product value")
	}

	f := &valueAccessd{answer: &osadminv1.GetExposedValueResponse{ProductTitle: "Appliance", Value: "stp_value",
		Entry: &osadminv1.ExposedValue{Name: "setup-token", Label: "Appliance setup token", OneTime: true, Link: "https://box1.appliance.example.org/admin/setup"}}}
	out, err := runValue(t, f, admin, "appliance setup-token")
	if err != nil || !strings.Contains(out, "stp_value") || !strings.Contains(out, "https://box1.appliance.example.org/admin/setup") || !strings.Contains(out, "Appliance setup token") {
		t.Fatalf("%v\n%s", err, out)
	}
	f.answer = &osadminv1.GetExposedValueResponse{ProductTitle: "Appliance", Entry: &osadminv1.ExposedValue{Name: "setup-token", Label: "Appliance setup token", OneTime: true, Consumed: true}}
	out, err = runValue(t, f, admin, "appliance setup-token")
	if err != nil || !strings.Contains(out, "already used") || strings.Contains(out, "stp_") || strings.Contains(out, "https://") {
		t.Fatalf("after it was used: %v\n%s", err, out)
	}
	if !slices.Equal(f.asked, []string{"setup-token", "setup-token"}) {
		t.Fatalf("accessd was asked %v", f.asked)
	}
	// Nothing undeclared is a command: no generic read reaches accessd.
	for _, line := range []string{"appliance support-id", "appliance SETUP_TOKEN", "appliance setup-token OTHER"} {
		_, _ = runValue(t, f, admin, line)
	}
	_, _ = runValue(t, f, owner, "appliance appliance-db")
	if len(f.asked) != 2 {
		t.Fatalf("accessd was asked %v", f.asked)
	}
}

func runValue(t *testing.T, f *valueAccessd, values []shell.Value, line string) (string, error) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(accessv1connect.NewAccessServiceHandler(f))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s := &shell.Services{Session: shell.Session{Admin: "alice", KeyFingerprint: "SHA256:abc", Source: "192.0.2.60"}}
	s.UseAccessd(http.DefaultClient, srv.URL)
	var out, errb bytes.Buffer
	e := &shell.Env{Origin: shell.OriginSSH, Backend: s, In: strings.NewReader(""), Out: &out, Err: &errb, Product: appliance, Values: values}
	err := shell.Run(context.Background(), e, line)
	return out.String() + errb.String(), err
}
