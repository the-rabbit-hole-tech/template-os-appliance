#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# check-unsigned.sh: check a directory of unsigned proof-build outputs
# before anything in it is published or handed on (docs/release.md,
# "Release guards"). An unsigned build carries no key of any kind, so:
#   - every UKI in it (*.efi) must read as unsigned: sbverify --list has to
#     say "No signature table present", and a file it can't read fails too;
#   - no signature, certificate, key or attestation file may be present;
#   - no file may hold private key material (a PEM private key or an age
#     identity), whatever its name;
#   - SHA256SUMS must exist, verify, and list every other file.
# Prints each problem and exits 1 if there is any.
#
# Usage: build/release/check-unsigned.sh <dir>
#   SBVERIFY   the sbverify to run (default: sbverify on PATH)
set -euo pipefail

dir="${1:?usage: check-unsigned.sh <dir>}"
[ -d "$dir" ] || { echo "check-unsigned: no such directory: $dir" >&2; exit 1; }
dir="$(cd "$dir" && pwd)"
sbverify="${SBVERIFY:-sbverify}"

problems=0
problem() {
  echo "check-unsigned: $*" >&2
  problems=$((problems + 1))
}

sha256() { shasum -a 256 "$1" | awk '{print $1}'; }

ukis=0
for f in "$dir"/*.efi; do
  [ -e "$f" ] || continue
  ukis=$((ukis + 1))
  name="$(basename "$f")"
  if ! out="$("$sbverify" --list "$f" 2>&1)"; then
    case "$out" in
      *"No signature table present"*) continue ;;
    esac
    problem "$name: sbverify can't read it as a PE image"
    continue
  fi
  case "$out" in
    *"No signature table present"*) ;;
    *"signature "*) problem "$name carries an Authenticode signature" ;;
    *) problem "$name: sbverify --list gave no clear answer: $out" ;;
  esac
done
[ "$ukis" -gt 0 ] || problem "no UKI (*.efi) in $dir"

for f in "$dir"/*; do
  [ -f "$f" ] || continue
  name="$(basename "$f")"
  case "$name" in
    *.sig | *.asc | *.gpg | *.sigstore | *.sigstore.json | *.bundle | *.intoto.jsonl | *.att | *.cosign* | \
      *.crt | *.cer | *.pem | *.der | *.p7s | *.p7b | *.p12 | *.pfx | *.key | *.age)
      problem "$name is a signature, certificate, key or attestation file; an unsigned build has none"
      ;;
  esac
  if LC_ALL=C grep -a -q -E -e '-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY-----' -e 'AGE-SECRET-KEY-1[0-9A-Z]+' "$f"; then
    problem "$name holds private key material"
  fi
done

if [ ! -f "$dir/SHA256SUMS" ]; then
  problem "no SHA256SUMS"
else
  while read -r sum name; do
    [ -n "${name:-}" ] || continue
    name="${name#\*}"
    if [ ! -f "$dir/$name" ]; then
      problem "SHA256SUMS lists $name, which isn't there"
    elif [ "$(sha256 "$dir/$name")" != "$sum" ]; then
      problem "SHA256SUMS doesn't verify for $name"
    fi
  done <"$dir/SHA256SUMS"
  for f in "$dir"/*; do
    [ -f "$f" ] || continue
    name="$(basename "$f")"
    [ "$name" = SHA256SUMS ] && continue
    awk -v n="$name" '{ sub(/^\*/, "", $2) } $2 == n { found = 1 } END { exit !found }' "$dir/SHA256SUMS" ||
      problem "$name isn't in SHA256SUMS"
  done
fi

if [ "$problems" -ne 0 ]; then
  echo "check-unsigned: $problems problem(s); nothing here may be published" >&2
  exit 1
fi
echo "check-unsigned: $ukis UKI(s), no signature and no key, SHA256SUMS covers every file"
