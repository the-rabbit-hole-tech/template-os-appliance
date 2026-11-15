// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package accessd

import (
	"context"
	"time"

	log "github.com/Bugs5382/go-log"

	osadminv1 "example.org/appliance/gen/go/appliance/osadmin/v1"
	"example.org/appliance/gen/go/appliance/osadmin/v1/osadminv1connect"
	"example.org/appliance/internal/accessapi"
	"example.org/appliance/internal/osadmin"
)

// saveStatus keeps st for the shell and appliance-osadmin.
func (s *Server) saveStatus(st *osadminv1.GetStatusResponse) {
	if s.o.StatusFile == "" || st == nil {
		return
	}
	if err := accessapi.WriteStatusCache(s.o.StatusFile, st, time.Now()); err != nil {
		s.o.Logger.Warn("accessd: status cache not written", log.F("error", err.Error()))
	}
}

// RefreshStatus computes the status as the console and keeps it, so the
// cache is fresh even when nobody asks.
func (s *Server) RefreshStatus(ctx context.Context) {
	st, err := runAs(ctx, s, osadmin.Local{Source: "accessd"}, osadminv1connect.StatusServiceGetStatusProcedure, s.h.Status.GetStatus, &osadminv1.GetStatusRequest{})
	if err != nil {
		s.o.Logger.Warn("accessd: status refresh failed", log.F("error", err.Error()))
		return
	}
	s.saveStatus(st)
}
