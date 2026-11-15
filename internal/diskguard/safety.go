// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package diskguard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// BoxProtected is the box's safety list: what the cleanup never touches,
// whatever a step asks for. Product data, the secrets and keys, the
// backups and the escrow, both base releases' Base Web slots and both
// product slots (the running release and its revert target), key custody
// and the sealed files, the access store, k0s's data (images go only
// through containerd) and the ESP with the boot entries. The OS audit log
// isn't under an allowed root either: only its own code compresses and
// moves it, keeping the chain.
var BoxProtected = []string{
	"/var/lib/appliance-data",
	"/var/lib/appliance/backup",
	"/var/lib/appliance/access",
	"/var/lib/appliance/ssh",
	"/var/lib/appliance/sealed",
	"/var/lib/appliance/osadmin",
	"/var/lib/appliance/osadmin-api/tls",
	"/var/lib/appliance/platform",
	"/var/lib/appliance/setup",
	"/var/lib/appliance/settings",
	"/var/lib/appliance/netd",
	"/var/lib/appliance/product",
	"/var/lib/appliance/web",
	"/var/lib/appliance/import",
	"/var/lib/appliance/k0s",
	"/var/lib/appliance/box-name",
	"/var/lib/appliance/machine-id",
	"/var/lib/k0s",
	"/run/appliance/esp",
}

// ErrProtected is a removal the safety list or the allowed roots refuse.
var ErrProtected = errors.New("diskguard: refused")

// Safety decides what the cleanup may remove: only what is inside one of
// the Allowed roots (never a root itself), and never a protected path,
// anything inside one, or anything that holds one. Paths are checked as
// given and with their directory's links resolved.
type Safety struct {
	Allowed   []string
	Protected []string
}

// Check refuses p unless the cleanup may remove it.
func (s Safety) Check(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return fmt.Errorf("%w: %s isn't a clean absolute path", ErrProtected, p)
	}
	paths := []string{p}
	if real := resolve(p); real != p {
		paths = append(paths, real)
	}
	for _, q := range paths {
		if err := s.check(q); err != nil {
			return err
		}
	}
	return nil
}

func (s Safety) check(p string) error {
	for _, q := range s.Protected {
		if within(p, q) || within(q, p) {
			return fmt.Errorf("%w: %s is on the safety list (%s)", ErrProtected, p, q)
		}
	}
	for _, root := range s.Allowed {
		if p != root && within(p, root) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is outside the cleanup's roots", ErrProtected, p)
}

// resolve is p with the links in its directories resolved, as far as
// they exist; p itself, when it's a link, is the link.
func resolve(p string) string {
	dir, rest := filepath.Dir(p), filepath.Base(p)
	for {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		dir, rest = parent, filepath.Join(filepath.Base(dir), rest)
	}
}

// within reports whether p is base or inside it.
func within(p, base string) bool {
	return p == base || strings.HasPrefix(p, strings.TrimSuffix(base, "/")+"/")
}

// Remove removes p (a file, a link, or a directory and what it holds)
// once Check allows it, and returns the bytes that gave back.
func (s Safety) Remove(p string) (int64, error) {
	if err := s.Check(p); err != nil {
		return 0, err
	}
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("diskguard: %w", err)
	}
	var n int64
	if fi.IsDir() {
		_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if i, ierr := d.Info(); ierr == nil {
					n += allocated(i)
				}
			}
			return nil
		})
		err = os.RemoveAll(p)
	} else {
		n = allocated(fi)
		err = os.Remove(p)
	}
	if err != nil {
		return 0, fmt.Errorf("diskguard: %w", err)
	}
	return n, nil
}

// allocated is the space a file takes on disk, which for a sparse file is
// less than its size.
func allocated(fi fs.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && fi.Mode().IsRegular() {
		return st.Blocks * 512
	}
	if fi.Mode().IsRegular() {
		return fi.Size()
	}
	return 0
}
