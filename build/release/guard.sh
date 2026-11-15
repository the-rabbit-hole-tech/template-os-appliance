#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# guard.sh: the pin guard a product's release runs before its long build
# (docs/release.md, "Release guards"). It reads the release pins file
# (build/release/pins.sh has its shape) and refuses the release unless:
#   - every repo the release requires has a pin (GUARD_REQUIRED);
#   - every pin names an owner/name repo and a full 40-hex commit;
#   - every pinned commit is reachable at that repo's remote, so the build
#     can't start from a commit that was never pushed or was force-pushed
#     away;
#   - on a tag run, the tag is vX.Y.Z or vX.Y.Z-rc.N and equals the pins
#     file's release: version, so a tag can't ship another release's pins.
#
# Usage: build/release/guard.sh [pins-file]   (default release/pins.yaml)
#   GUARD_REQUIRED          space-separated keys that must be pinned
#   GUARD_REMOTE_<KEY>      a repo's remote URL instead of
#                           https://github.com/<owner/name>.git; KEY is the
#                           pins key upper-cased, '-' turned into '_'
#   GUARD_EVENT             "tag" for a tag run (default: tag when
#                           GITHUB_REF_TYPE is tag, else none)
#   GUARD_TAG               the tag being released (default: GITHUB_REF_NAME)
# Exits 0 with "guard: OK", or 1 listing every problem it found.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
pins_file="${1:-$root/release/pins.yaml}"
pins="$here/pins.sh"

[ -f "$pins_file" ] || { echo "guard: pins file not found: $pins_file" >&2; exit 1; }

errors=()
fail() { errors+=("$1"); }

remote_for() {
  local key="$1" repo="$2" var
  var="GUARD_REMOTE_$(printf '%s' "$key" | tr '[:lower:]-' '[:upper:]_')"
  if [ -n "${!var:-}" ]; then
    printf '%s' "${!var}"
  else
    printf 'https://github.com/%s.git' "$repo"
  fi
}

# reachable <url> <commit>: fetch just that commit into a throwaway repo.
reachable() {
  local tmp ok=1
  tmp="$(mktemp -d)"
  git init -q "$tmp"
  if git -C "$tmp" fetch -q --depth 1 "$1" "$2" >/dev/null 2>&1 &&
    git -C "$tmp" cat-file -e "$2^{commit}" 2>/dev/null; then
    ok=0
  fi
  rm -rf "$tmp"
  return "$ok"
}

keys="$("$pins" keys "$pins_file")"
[ -n "$keys" ] || fail "no repos are pinned in $pins_file"

for want in ${GUARD_REQUIRED:-}; do
  printf '%s\n' "$keys" | grep -qx -- "$want" || fail "missing pin: $want"
done

for key in $keys; do
  repo="$("$pins" repo "$key" "$pins_file" 2>/dev/null || true)"
  commit="$("$pins" commit "$key" "$pins_file" 2>/dev/null || true)"
  if ! [[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
    fail "pin for $key names no owner/name repo: ${repo:-none}"
    continue
  fi
  if ! [[ "$commit" =~ ^[0-9a-f]{40}$ ]]; then
    fail "pin for $key is not a full 40-hex commit: ${commit:-none}"
    continue
  fi
  url="$(remote_for "$key" "$repo")"
  reachable "$url" "$commit" || fail "unreachable commit for $key: $commit ($url)"
done

event="${GUARD_EVENT:-}"
if [ -z "$event" ] && [ "${GITHUB_REF_TYPE:-}" = tag ]; then
  event=tag
fi
if [ "$event" = tag ]; then
  tag="${GUARD_TAG:-${GITHUB_REF_NAME:-}}"
  release="$("$pins" release "$pins_file")"
  if ! [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]]; then
    fail "tag ${tag:-none} is not vX.Y.Z or vX.Y.Z-rc.N"
  elif [ "$tag" != "$release" ]; then
    fail "tag $tag doesn't match the pins file's release: ${release:-none}"
  fi
fi

if [ "${#errors[@]}" -gt 0 ]; then
  echo "guard: FAILED" >&2
  for e in "${errors[@]}"; do
    echo "  - $e" >&2
  done
  exit 1
fi
echo "guard: OK (release $("$pins" release "$pins_file"))"
