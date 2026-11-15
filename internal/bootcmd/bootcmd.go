// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

// Package bootcmd reads the appliance's parameters from a kernel command
// line: the UKI's .cmdline section on amd64, cmdline.txt on arm64.
package bootcmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Params are the appliance.* parameters.
type Params struct {
	RootHash   string // 64 lowercase hex
	HashOffset int64
	Version    string
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Parse reads appliance.roothash, appliance.hashoffset and appliance.version.
// Each must appear exactly once; the root hash and offset are required.
func Parse(cmdline string) (Params, error) {
	var p Params
	seen := map[string]bool{}
	for _, f := range strings.Fields(strings.TrimRight(cmdline, "\x00\n")) {
		k, v, ok := strings.Cut(f, "=")
		if !ok || !strings.HasPrefix(k, "appliance.") {
			continue
		}
		if seen[k] {
			return Params{}, fmt.Errorf("bootcmd: %s appears twice", k)
		}
		seen[k] = true
		switch k {
		case "appliance.roothash":
			if !hex64.MatchString(v) {
				return Params{}, fmt.Errorf("bootcmd: appliance.roothash %q isn't 64 lowercase hex", v)
			}
			p.RootHash = v
		case "appliance.hashoffset":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n <= 0 || n%4096 != 0 {
				return Params{}, fmt.Errorf("bootcmd: appliance.hashoffset %q isn't a positive multiple of 4096", v)
			}
			p.HashOffset = n
		case "appliance.version":
			p.Version = v
		}
	}
	if p.RootHash == "" || p.HashOffset == 0 {
		return Params{}, fmt.Errorf("bootcmd: the command line has no appliance.roothash and appliance.hashoffset")
	}
	return p, nil
}

// SlotGUID is the PARTUUID of the root slot holding the release with root
// hash rootHash: its first 128 bits in GUID text form. One signed UKI finds
// its root in either slot this way.
func SlotGUID(rootHash string) (string, error) {
	if !hex64.MatchString(rootHash) {
		return "", fmt.Errorf("bootcmd: root hash %q isn't 64 lowercase hex", rootHash)
	}
	h := rootHash[:32]
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}
