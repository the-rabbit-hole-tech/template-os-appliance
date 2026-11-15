#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# reproducible.sh: build the unsigned outputs twice from one commit, each
# in a fresh clone with its own Go build cache, and compare every file by
# SHA-256 (docs/release.md, "Release guards"). It's the check behind
# "rebuild the tag and compare the hash": anyone who builds the same commit
# must get the same unsigned bytes the release lists in SHA256SUMS. The
# signing steps come after this and are left out of it, since they add
# keys a rebuild doesn't have.
#
# Exits 0 when every output is identical; 1 when any differs or exists in
# only one build (each one is named), when a build makes no output, and on
# a dirty source tree, since the clones are taken from HEAD and wouldn't
# match it.
#
# Usage: build/release/reproducible.sh
#   REPRO_BUILD    the product's unsigned build, a shell command run at the
#                  root of each clone (required: the template has no
#                  product build of its own)
#   REPRO_OUT      where the build writes, relative to the clone (default
#                  build/out)
#   REPRO_SOURCE   the repo to clone (default: this checkout)
#   REPRO_WORK     where the two clones go (default: a temp dir)
#   REPRO_KEEP=1   keep both clones afterwards, for diffoscope
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
src="${REPRO_SOURCE:-$root}"
out="${REPRO_OUT:-build/out}"

say() { printf 'reproducible: %s\n' "$*" >&2; }

[ -n "${REPRO_BUILD:-}" ] || { say "set REPRO_BUILD to the product's unsigned build command"; exit 2; }
if ! git -C "$src" diff --quiet HEAD -- || [ -n "$(git -C "$src" status --porcelain --untracked-files=no)" ]; then
  say "the source tree has uncommitted changes; commit them first"
  exit 1
fi
commit="$(git -C "$src" rev-parse HEAD)"

work="${REPRO_WORK:-}"
made=0
if [ -z "$work" ]; then
  work="$(mktemp -d)"
  made=1
fi
mkdir -p "$work"
work="$(cd "$work" && pwd)"

cleanup() {
  if [ "${REPRO_KEEP:-}" = 1 ]; then
    say "kept the clones in $work"
    return 0
  fi
  rm -rf "$work/one" "$work/two"
  [ "$made" = 1 ] && rm -rf "$work"
  return 0
}
trap cleanup EXIT

build() {
  local ws="$1"
  rm -rf "$ws"
  git clone -q --no-local "$src" "$ws"
  git -C "$ws" checkout -q --detach "$commit"
  say "building in $(basename "$ws")"
  # No signing input the caller exported reaches the build.
  (cd "$ws" && env -u SB_KEY -u SB_CERT -u RELEASE_COSIGN_KEY -u UPDATE_AGE_KEY \
    GOCACHE="$ws/.gocache" bash -c "$REPRO_BUILD")
}

# hashes <clone>: "<sha256>  <path>" for every output file, sorted by path.
hashes() {
  local d="$1/$out"
  [ -d "$d" ] || return 0
  (cd "$d" && find . -type f | LC_ALL=C sort | while IFS= read -r f; do
    printf '%s  %s\n' "$(shasum -a 256 "$f" | awk '{print $1}')" "${f#./}"
  done)
}

say "commit $commit"
build "$work/one"
build "$work/two"
hashes "$work/one" >"$work/one.sums"
hashes "$work/two" >"$work/two.sums"
if [ ! -s "$work/one.sums" ] && [ ! -s "$work/two.sums" ]; then
  say "no outputs in $out; nothing to compare"
  exit 1
fi

echo "commit $commit"
# Both lists are sorted by path; awk pairs them up by path.
awk '
  NR == FNR { a[$2] = $1; names[$2] = 1; next }
  { b[$2] = $1; names[$2] = 1 }
  END {
    bad = 0; same = 0
    n = 0
    for (k in names) list[++n] = k
    for (i = 2; i <= n; i++) { v = list[i]; j = i - 1; while (j > 0 && list[j] > v) { list[j + 1] = list[j]; j-- } list[j + 1] = v }
    for (i = 1; i <= n; i++) {
      k = list[i]
      if (!(k in a) || !(k in b)) { printf "MISSING  %s (built once only)\n", k; bad++ }
      else if (a[k] == b[k]) { printf "same     %s  %s\n", k, a[k]; same++ }
      else { printf "DIFFERS  %s  %s  %s\n", k, a[k], b[k]; bad++ }
    }
    if (bad) { printf "reproducible: %d of %d files differ\n", bad, same + bad; exit 1 }
    printf "reproducible: %d files identical\n", same
  }
' "$work/one.sums" "$work/two.sums"
