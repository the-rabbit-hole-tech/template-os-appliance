// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package power_test

import (
	"slices"
	"testing"

	"example.org/appliance/internal/power"
)

const mounts = `/dev/mapper/root-verity / squashfs ro,relatime 0 0
proc /proc proc rw,nosuid,nodev 0 0
/dev/sda1 /run/appliance/esp vfat rw,nosuid,nodev,noexec 0 0
/dev/mapper/appliance-state /var/lib/appliance ext4 rw,relatime 0 0
/dev/mapper/appliance-state /var/lib/k0s ext4 rw,relatime 0 0
tmpfs /var/lib/k0s/kubelet/pods/a\040b/volumes tmpfs rw 0 0
/dev/mapper/appliance-backup /var/lib/appliance/backup ext4 rw,relatime 0 0
`

func TestVolumeMountsAreUnmountedDeepestFirst(t *testing.T) {
	targets, mappings := power.VolumeMounts(mounts)
	want := []string{"/var/lib/k0s/kubelet/pods/a b/volumes", "/var/lib/appliance/backup", "/var/lib/appliance", "/var/lib/k0s"}
	if !slices.Equal(targets, want) {
		t.Fatalf("targets %v", targets)
	}
	if !slices.Equal(mappings, []string{"appliance-backup", "appliance-state"}) {
		t.Fatalf("mappings %v", mappings)
	}
}
