// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package accessd

import (
	"context"

	"connectrpc.com/connect"

	accessv1 "example.org/appliance/gen/go/appliance/access/v1"
	"example.org/appliance/gen/go/appliance/access/v1/accessv1connect"
	osadminv1 "example.org/appliance/gen/go/appliance/osadmin/v1"
	"example.org/appliance/internal/access"
	"example.org/appliance/internal/codes"
)

type sshLoginH struct {
	accessv1connect.UnimplementedSshLoginServiceHandler
	s *Server
}

// VerifyTotp is a closed-shell login's first call: its TOTP code, under the
// lockout, for the key sshd authenticated.
func (h *sshLoginH) VerifyTotp(ctx context.Context, r *connect.Request[accessv1.VerifyTotpRequest]) (*connect.Response[accessv1.VerifyTotpResponse], error) {
	l, err := h.s.caller(ctx, r.Header(), accessv1connect.SshLoginServiceVerifyTotpProcedure)
	if err != nil {
		return nil, err
	}
	if l.Admin == "" {
		return nil, refuse(codes.New(codes.AccessForbidden, "the TOTP check is an SSH login's"))
	}
	login, err := h.s.api.VerifyLoginTotp(l, r.Msg.GetTotpCode())
	if err != nil {
		return nil, coded(err)
	}
	return connect.NewResponse(&accessv1.VerifyTotpResponse{Admin: login.Admin, Role: roleWire(login.Role), RootOperator: login.RootOperator, LoginId: login.ID}), nil
}

func (h *sshLoginH) EndSshLogin(ctx context.Context, r *connect.Request[accessv1.EndSshLoginRequest]) (*connect.Response[accessv1.EndSshLoginResponse], error) {
	l, err := h.s.caller(ctx, r.Header(), accessv1connect.SshLoginServiceEndSshLoginProcedure)
	if err != nil {
		return nil, err
	}
	h.s.api.EndSSHLogin(l, r.Msg.GetLoginId())
	return connect.NewResponse(&accessv1.EndSshLoginResponse{}), nil
}

func roleWire(r access.Role) osadminv1.Role {
	if r == access.RoleOwner {
		return osadminv1.Role_ROLE_OWNER
	}
	return osadminv1.Role_ROLE_ADMIN
}
