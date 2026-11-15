// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package osadmin_test

import (
	"context"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	osadminv1 "example.org/appliance/gen/go/appliance/osadmin/v1"
	"example.org/appliance/gen/go/appliance/osadmin/v1/osadminv1connect"
	"example.org/appliance/internal/osadmin"
)

const escrowYAML = valuesYAML + `escrow:
  - {name: data-root-key, secret: app/app-data-generated, key: DATA_ROOT_KEK}
  - {name: totp-key, secret: app/app-identity-generated, key: TOTP_ENC_KEY}
`

// The recovery escrow carries the installed product's keys: downloading it
// seals each under KeyCustody and writes a new escrow file when one is new
// or changed, so a restore onto another box can open the product's data.
func TestTheEscrowCarriesTheProductsKeys(t *testing.T) {
	k := &fakeKube{secrets: map[string]map[string]string{
		"app/app-data-generated":     {"DATA_ROOT_KEK": "root-key-bytes"},
		"app/app-identity-generated": {"TOTP_ENC_KEY": "totp-key-bytes"},
	}, state: `{"needsSetup": true}`}
	b := newBox(t, false, func(_ *box, o *osadmin.Options) { o.Exposed = k })
	installProduct(t, filepath.Join(b.state, "product"), escrowYAML)
	ctx := context.Background()
	alice := b.browser()
	alice.signIn("alice")
	su := osadminv1connect.NewSetupServiceClient(alice.hc, b.ts.URL)
	if _, err := su.AddRecoveryKey(ctx, connect.NewRequest(&osadminv1.AddRecoveryKeyRequest{PublicKey: newKey(t).line, Label: "safe"})); err != nil {
		t.Fatal(err)
	}
	escrowsBefore := len(b.init.escrowFor)
	b.clk.Advance(1e9)
	if _, err := su.DownloadEscrow(ctx, connect.NewRequest(&osadminv1.DownloadEscrowRequest{})); err != nil {
		t.Fatal(err)
	}
	if string(b.init.sealed["product-appliance-data-root-key"]) != "root-key-bytes" || string(b.init.sealed["product-appliance-totp-key"]) != "totp-key-bytes" {
		t.Fatalf("sealed %v", b.init.sealed)
	}
	if b.init.seals != 2 || len(b.init.escrowFor) != escrowsBefore {
		t.Fatalf("seals %d, escrow recipients %v", b.init.seals, b.init.escrowFor)
	}
	// Unchanged keys aren't sealed again.
	if _, err := su.DownloadEscrow(ctx, connect.NewRequest(&osadminv1.DownloadEscrowRequest{})); err != nil || b.init.seals != 2 {
		t.Fatalf("seals %d %v", b.init.seals, err)
	}
	// A changed key is sealed again, with a new escrow.
	k.mu.Lock()
	k.secrets["app/app-data-generated"]["DATA_ROOT_KEK"] = "rotated"
	k.mu.Unlock()
	if _, err := su.DownloadEscrow(ctx, connect.NewRequest(&osadminv1.DownloadEscrowRequest{})); err != nil || string(b.init.sealed["product-appliance-data-root-key"]) != "rotated" {
		t.Fatalf("%v %v", b.init.sealed, err)
	}
}
