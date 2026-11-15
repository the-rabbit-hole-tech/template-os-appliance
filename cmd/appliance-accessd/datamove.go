// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"

	log "github.com/Bugs5382/go-log"

	"example.org/appliance/internal/datamove"
	"example.org/appliance/internal/diskguard"
	"example.org/appliance/internal/elevated"
	"example.org/appliance/internal/keycustody"
	"example.org/appliance/internal/osadmin"
	"example.org/appliance/internal/osaudit"
	"example.org/appliance/internal/product"
	"example.org/appliance/internal/productspec"
	"example.org/appliance/internal/productup"
)

// followingProbe is the probe of the one process that follows the product:
// it can run the data move a slot declares (docs/data-move.md). k0s's
// pre-stop and the product reset ask the plain one.
func followingProbe(c config, lg log.Logger, audit *osaudit.Log, bk *osadmin.Backups) *productup.Probe {
	p := productProbe(c)
	p.Move = dataMover(c, lg, audit)
	p.Restore = bk.Restore
	return p
}

func moveDumps() string { return filepath.Join(keycustody.BackupTarget, "data-move") }

func dataMover(c config, lg log.Logger, audit *osaudit.Log) *datamove.Runner {
	slot := filepath.Join(product.Dir, "current")
	return &datamove.Runner{
		Logger: lg,
		New: func(spec productspec.DataMove, svc datamove.Services, progress func(string)) *datamove.Move {
			return &datamove.Move{
				Spec: spec, Slot: slot,
				BackupDir: moveDumps(),
				Journal:   filepath.Join(c.state, "platform", datamove.JournalFile),
				Volumes:   productspec.DataRoot,
				Engine: &datamove.PG{Logger: lg, Shell: datamove.Exec{
					Prefix: datamove.KubePrefix(filepath.Join(slot, "k0s"), elevated.DefaultKubeconfig), Logger: lg,
				}},
				Services: svc, Disk: diskUsage, Progress: progress, Logger: lg,
				Audit: func(action string, detail map[string]string) {
					detail["surface"] = osaudit.SurfaceAdmin
					if err := audit.Append(osaudit.Entry{Actor: "osadmin", Action: action, Target: "product", Outcome: "ok", Detail: detail}); err != nil {
						lg.Error(err, "accessd: a data move's audit entry wasn't written", log.F("action", action))
					}
				},
			}
		},
	}
}

func diskUsage(p string) (datamove.Usage, error) {
	u, err := diskguard.Statfs(p)
	return datamove.Usage{Avail: u.Avail, Device: u.Device}, err
}
