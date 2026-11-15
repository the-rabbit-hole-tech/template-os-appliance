#!/usr/bin/env bash
# Copyright 2026 The Rabbit Hole Tech, LLC
# SPDX-License-Identifier: Apache-2.0
#
# Usage: dco.sh <base> <head>
#
# Developer Certificate of Origin check (see DCO and docs/dco.md). Every
# commit in base..head that is not a merge and not a bot's must carry a
# "Signed-off-by: Name <email>" line whose address is the commit's author or
# committer address. Each offender is named with the reason; the exit status
# is 1 when there is any. A range with no commits passes.
#
# Merge commits are skipped because the person who merged signs nothing by
# merging; a bot (an author whose address contains "[bot]") is skipped because
# it cannot make the certification.
#
# Refs the repo's issue #52.
set -euo pipefail

[ "$#" -eq 2 ] || { echo "usage: dco.sh <base> <head>" >&2; exit 2; }
base="$1" head="$2"

lower() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

bad=0
while IFS= read -r sha; do
  [ -n "$sha" ] || continue
  author="$(lower "$(git log -1 --format=%ae "$sha")")"
  committer="$(lower "$(git log -1 --format=%ce "$sha")")"
  subject="$(git log -1 --format=%s "$sha")"
  case "$author" in *"[bot]"*) continue ;; esac

  found=0 matched=0
  while IFS= read -r line; do
    if [[ "$line" =~ ^Signed-off-by:[[:space:]]+[^\<]*[^[:space:]\<][[:space:]]*\<([^\<\>[:space:]]+@[^\<\>[:space:]]+)\>[[:space:]]*$ ]]; then
      found=1
      addr="$(lower "${BASH_REMATCH[1]}")"
      if [ "$addr" = "$author" ] || [ "$addr" = "$committer" ]; then matched=1; fi
    fi
  done < <(git log -1 --format=%B "$sha")

  if [ "$found" -eq 0 ]; then
    echo "${sha:0:12} $subject: no Signed-off-by: Name <email> line" >&2
    bad=1
  elif [ "$matched" -eq 0 ]; then
    echo "${sha:0:12} $subject: the sign-off address is neither the author's nor the committer's" >&2
    bad=1
  fi
done < <(git rev-list --no-merges "$base..$head")

if [ "$bad" -ne 0 ]; then
  echo "dco: sign off the commits above (git commit -s, or git rebase --signoff). See docs/dco.md." >&2
  exit 1
fi
