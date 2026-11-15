// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package netdapi_test

import (
	"fmt"
	"testing"

	"example.org/appliance/internal/netdapi"
)

func TestBindable(t *testing.T) {
	got := fmt.Sprint(netdapi.Bindable([]string{"192.0.2.10/24", "2001:db8::10", "fe80::1/64", "not an address"}))
	if got != "[192.0.2.10 2001:db8::10]" {
		t.Fatalf("bindable %s", got)
	}
}
