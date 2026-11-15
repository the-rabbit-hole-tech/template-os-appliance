#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# pins.sh: read a product's release pins file, the one place its release
# names what it's built from (docs/release.md, "Release guards"). The file
# has a fixed two-level shape, and this is a reader for that shape, not a
# general YAML parser:
#
#   version: 1
#   release: v0.1.0-rc.1
#   repos:
#     release: {repo: example/appliance-release, commit: <40-hex>}
#     web:     {repo: example/appliance-web, commit: <40-hex>}
#
# Usage:
#   pins.sh commit <key> [pins-file]   print that repo's pinned commit
#   pins.sh repo <key> [pins-file]     print that repo's owner/name
#   pins.sh keys [pins-file]           print every key, one per line
#   pins.sh release [pins-file]        print the release: version
# The pins file defaults to release/pins.yaml at the repo root. A key that
# isn't pinned exits 1.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"

usage() { echo "pins: usage: pins.sh commit|repo <key> [pins-file] | keys [pins-file] | release [pins-file]" >&2; exit 2; }

what="${1:-}"
case "$what" in
  commit | repo)
    key="${2:-}"
    [ -n "$key" ] || usage
    pins_file="${3:-$root/release/pins.yaml}"
    ;;
  keys | release) pins_file="${2:-$root/release/pins.yaml}" ;;
  *) usage ;;
esac
[ -f "$pins_file" ] || { echo "pins: no such pins file: $pins_file" >&2; exit 1; }

# entries prints "<key> <repo> <commit>" for each line of the repos: block;
# a field the line lacks prints as "-".
entries() {
  local section="" line k body r c
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      repos:*) section=repos; continue ;;
      [a-zA-Z]*:*) section="" ;;
    esac
    [ "$section" = repos ] || continue
    if [[ "$line" =~ ^[[:space:]]+([a-zA-Z0-9_-]+):[[:space:]]*\{(.*)\}[[:space:]]*$ ]]; then
      k="${BASH_REMATCH[1]}" body="${BASH_REMATCH[2]}" r=- c=-
      [[ "$body" =~ (^|[,[:space:]])repo:[[:space:]]*([^,}[:space:]]+) ]] && r="${BASH_REMATCH[2]}"
      [[ "$body" =~ (^|[,[:space:]])commit:[[:space:]]*([^,}[:space:]]+) ]] && c="${BASH_REMATCH[2]}"
      printf '%s %s %s\n' "$k" "$r" "$c"
    fi
  done <"$pins_file"
}

case "$what" in
  keys) entries | awk '{print $1}' ;;
  release) sed -n -E 's/^release:[[:space:]]*([^[:space:]#]+).*/\1/p' "$pins_file" | head -n 1 ;;
  commit | repo)
    col=3
    [ "$what" = repo ] && col=2
    v="$(entries | awk -v k="$key" -v c="$col" '$1 == k { print $c; exit }')"
    if [ -z "$v" ] || [ "$v" = - ]; then
      echo "pins: no $what pinned for $key in $pins_file" >&2
      exit 1
    fi
    printf '%s\n' "$v"
    ;;
esac
