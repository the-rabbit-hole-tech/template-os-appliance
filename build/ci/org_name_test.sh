#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# The template names no real organisation in the module path, the image paths
# or the release-repo slugs: those are fixed placeholders (example.org/appliance,
# registry.example.org/, example-owner) that the init script fills for a
# product. This fails when the real organisation's name is left in a tracked
# file, except in the files that describe the real repository and its own issue
# link.
#
# Refs the repo's issue #56.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"

# The real organisation, built from two halves so this file does not trip
# itself.
org="the-rabbit-hole""-tech"

# check <dir>: names every offending line under dir (a git work tree), exits 1
# if there is any.
check() {
  local dir="$1" out
  out="$(git -C "$dir" grep -n -I -e "$org" -- . \
    ':!.github' ':!CLAUDE.md' ':!NOTICE' ':!build/ci/org_name_test.sh' \
    | grep -v 'template-os-appliance/issues/72' || true)"
  if [ -n "$out" ]; then
    echo "FAIL: the real organisation is named outside the real-repo files:" >&2
    echo "$out" >&2
    return 1
  fi
}

# The check itself: a fixture with the name in code and in a real-repo file.
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
git -C "$work" init -q .
mkdir -p "$work/.github"
printf 'module example.org/appliance\n' > "$work/go.mod"
printf '* @%s\n' "$org" > "$work/.github/CODEOWNERS"
git -C "$work" add -A
check "$work" || { echo "FAIL: a clean fixture was refused" >&2; exit 1; }
printf 'module github.com/%s/x\n' "$org" > "$work/go.mod"
git -C "$work" add -A
if check "$work" 2>/dev/null; then echo "FAIL: the organisation in go.mod passed" >&2; exit 1; fi

check "$root" || exit 1
echo "PASS: the real organisation is named only in the real-repo files"
