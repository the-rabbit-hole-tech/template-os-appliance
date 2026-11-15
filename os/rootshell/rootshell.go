// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

// Package rootshell is the root shell's start file (rc.sh), which
// appliance-elevated hands the interactive ash through ENV.
package rootshell

import _ "embed"

// RC is rc.sh.
//
//go:embed rc.sh
var RC string
