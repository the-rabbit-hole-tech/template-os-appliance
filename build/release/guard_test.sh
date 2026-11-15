#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# The pin guard and its reader, against throwaway local repos standing in
# for the pinned remotes (GUARD_REMOTE_<KEY>): a good pins file passes;
# a missing required pin, a short or non-hex commit, a pin with no repo,
# a commit the remote never had, a bad tag and a tag that isn't the pins
# file's release all fail, each named.
# shellcheck disable=SC2015 # ok && bad pairs: ok never fails
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fail=0
ok() { echo "ok   $*"; }
bad() {
  echo "FAIL $*" >&2
  fail=1
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# mkrepo <dir>: a repo with one commit; prints that commit. It commits as
# whoever git is set up as; only a runner with no identity at all gets a
# throwaway one, so a workstation's own commit hooks still see its own.
mkrepo() {
  local who=()
  git config user.email >/dev/null 2>&1 || who=(-c user.name=ci -c user.email=ci@example.org)
  git init -q "$1"
  git -C "$1" ${who[@]+"${who[@]}"} commit -q --allow-empty -m first >/dev/null
  git -C "$1" rev-parse HEAD
}
rel="$(mkrepo "$work/release")"
web="$(mkrepo "$work/web")"
export GUARD_REMOTE_RELEASE="file://$work/release" GUARD_REMOTE_APP_WEB="file://$work/web"

pins() {
  cat >"$work/pins.yaml" <<EOF
version: 1
release: v0.2.0-rc.1
repos:
  release: {repo: example/appliance-release, commit: $1}
  app-web:     {repo: example/appliance-web, commit: $2}
EOF
}

# run <name> <want exit> <want text> [env...]: run the guard, check both.
run() {
  local name="$1" want="$2" text="$3"
  shift 3
  local out rc=0
  out="$(env "$@" bash "$here/guard.sh" "$work/pins.yaml" 2>&1)" || rc=$?
  if [ "$rc" != "$want" ]; then
    bad "$name: exit $rc, want $want: $out"
  elif [[ "$out" != *"$text"* ]]; then
    bad "$name: no '$text' in: $out"
  else
    ok "$name"
  fi
}

pins "$rel" "$web"
run "good pins" 0 "guard: OK (release v0.2.0-rc.1)" GUARD_REQUIRED="release app-web"
run "a required pin missing" 1 "missing pin: docs" GUARD_REQUIRED="release docs"
run "a good tag" 0 "guard: OK" GUARD_EVENT=tag GUARD_TAG=v0.2.0-rc.1
run "a tag that isn't semver" 1 "is not vX.Y.Z or vX.Y.Z-rc.N" GUARD_EVENT=tag GUARD_TAG=v0.2
run "a tag for another release" 1 "doesn't match the pins file's release: v0.2.0-rc.1" GUARD_EVENT=tag GUARD_TAG=v0.2.0
run "a branch run skips the tag check" 0 "guard: OK" GITHUB_REF_TYPE=branch GITHUB_REF_NAME=main

pins "${rel:0:12}" "$web"
run "a short commit" 1 "pin for release is not a full 40-hex commit: ${rel:0:12}"
pins "$(printf '%s' "$rel" | tr 'a-f' 'A-F')" "$web"
run "an upper-case commit" 1 "pin for release is not a full 40-hex commit"
pins "$rel" "0123456789abcdef0123456789abcdef01234567"
run "a commit the remote never had" 1 "unreachable commit for app-web: 0123456789abcdef0123456789abcdef01234567"

cat >"$work/pins.yaml" <<EOF
release: v0.2.0
repos:
  release: {commit: $rel}
EOF
run "a pin with no repo" 1 "pin for release names no owner/name repo"

cat >"$work/pins.yaml" <<EOF
release: v0.2.0
repos:
EOF
run "no pins at all" 1 "no repos are pinned"
out="$(bash "$here/guard.sh" "$work/nothing.yaml" 2>&1)" && bad "a missing pins file passed" || ok "a missing pins file"
[[ "$out" == *"pins file not found"* ]] || bad "a missing pins file: $out"

# The reader.
pins "$rel" "$web"
[ "$(bash "$here/pins.sh" commit app-web "$work/pins.yaml")" = "$web" ] && ok "pins.sh commit" || bad "pins.sh commit"
[ "$(bash "$here/pins.sh" repo release "$work/pins.yaml")" = example/appliance-release ] && ok "pins.sh repo" || bad "pins.sh repo"
[ "$(bash "$here/pins.sh" keys "$work/pins.yaml" | tr '\n' ' ')" = "release app-web " ] && ok "pins.sh keys" || bad "pins.sh keys"
[ "$(bash "$here/pins.sh" release "$work/pins.yaml")" = v0.2.0-rc.1 ] && ok "pins.sh release" || bad "pins.sh release"
if bash "$here/pins.sh" commit docs "$work/pins.yaml" >/dev/null 2>&1; then bad "pins.sh found an unpinned key"; else ok "pins.sh refuses an unpinned key"; fi
cat >"$work/other.yaml" <<EOF
repos:
  release: {repo: a/b, commit: $rel}
spec:
  web: {repo: c/d, commit: $web}
EOF
if bash "$here/pins.sh" commit web "$work/other.yaml" >/dev/null 2>&1; then bad "pins.sh read a key outside repos:"; else ok "pins.sh reads repos: only"; fi

[ "$fail" = 0 ] || exit 1
echo "guard_test: all passed"
