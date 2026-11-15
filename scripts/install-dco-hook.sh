#!/usr/bin/env bash
# Copyright 2026 The Rabbit Hole Tech, LLC
# SPDX-License-Identifier: Apache-2.0
#
# Installs scripts/hooks/prepare-commit-msg into this clone, so every commit
# you make is signed off for the DCO (docs/dco.md). It is its own hook name, so
# it sits beside the governance hooks (.claude/hooks/install.sh) without
# touching them. An existing prepare-commit-msg that is not ours is left alone.
#
# Refs the repo's issue #52.
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
dest="$(git rev-parse --git-path hooks/prepare-commit-msg)"
case "$dest" in /*) ;; *) dest="$root/$dest" ;; esac

if [ -e "$dest" ] && ! cmp -s "$root/scripts/hooks/prepare-commit-msg" "$dest"; then
  echo "install-dco-hook: $dest exists and is not this hook; leaving it alone." >&2
  exit 1
fi
mkdir -p "$(dirname "$dest")"
cp "$root/scripts/hooks/prepare-commit-msg" "$dest"
chmod +x "$dest"
echo "Installed the DCO sign-off hook at $dest"
