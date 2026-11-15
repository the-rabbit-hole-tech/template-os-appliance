#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# The reproducible-build check: two clean clones of one commit, a build in
# each, every output compared by SHA-256. The product's real build is
# replaced by a small REPRO_BUILD; the clone, the clean-tree check, the
# hashing, the comparison and the report all run for real. A same-bytes
# build passes; a build that differs, makes a file in one clone only or
# makes nothing fails; a dirty tree is refused before anything is built; no
# REPRO_BUILD is refused; and a signing key the caller exported never
# reaches the build.
# shellcheck disable=SC2016 # the builds are run by the script under test, not here
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/reproducible.sh"
fail=0
ok() { echo "ok   $*"; }
bad() {
  echo "FAIL $*" >&2
  fail=1
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Commits as whoever git is set up as; only a runner with no identity at
# all gets a throwaway one.
who=()
git config user.email >/dev/null 2>&1 || who=(-c user.name=ci -c user.email=ci@example.org)
src="$work/src"
git init -q "$src"
printf 'the source\n' >"$src/input.txt"
git -C "$src" add input.txt
git -C "$src" ${who[@]+"${who[@]}"} commit -q -m source >/dev/null

# run <name> <want exit> <want text> <build> [env...]
run() {
  local name="$1" want="$2" text="$3" build="$4"
  shift 4
  local out rc=0
  out="$(env REPRO_SOURCE="$src" REPRO_WORK="$work/ws" REPRO_BUILD="$build" "$@" bash "$script" 2>&1)" || rc=$?
  if [ "$rc" != "$want" ]; then
    bad "$name: exit $rc, want $want: $out"
  elif [[ "$out" != *"$text"* ]]; then
    bad "$name: no '$text' in: $out"
  else
    ok "$name"
  fi
}

same='mkdir -p build/out/sub && cp input.txt build/out/a.img && printf fixed >build/out/sub/b.efi'
run "a build that makes the same bytes" 0 "reproducible: 2 files identical" "$same"
run "the report names each file" 0 "same     sub/b.efi" "$same"

differs='mkdir -p build/out && od -An -N16 -tx1 /dev/urandom >build/out/a.img && cp input.txt build/out/c.img'
run "a build that differs" 1 "DIFFERS  a.img" "$differs"
run "the count of what differs" 1 "reproducible: 1 of 2 files differ" "$differs"

once='mkdir -p build/out && cp input.txt build/out/a.img && if [ "$(basename "$PWD")" = one ]; then cp input.txt build/out/extra.img; fi'
run "a file built in one clone only" 1 "MISSING  extra.img" "$once"

run "a build that makes nothing" 1 "no outputs in build/out" 'true'
run "another output directory" 0 "1 files identical" 'mkdir -p out && cp input.txt out/a.img' REPRO_OUT=out
run "no REPRO_BUILD" 2 "set REPRO_BUILD" ''

leak='mkdir -p build/out && printf "%s" "${SB_KEY:-none}" >build/out/key.txt'
run "a signing key never reaches the build" 0 "1 files identical" "$leak" SB_KEY=/secret/db.key REPRO_KEEP=1
if [ "$(cat "$work/ws/one/build/out/key.txt" 2>/dev/null)" = none ]; then ok "SB_KEY was unset"; else bad "SB_KEY reached the build"; fi

printf 'changed\n' >>"$src/input.txt"
run "a dirty tree" 1 "uncommitted changes" "$same"

[ "$fail" = 0 ] || exit 1
echo "reproducible_test: all passed"
