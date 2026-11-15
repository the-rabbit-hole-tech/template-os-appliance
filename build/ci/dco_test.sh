#!/usr/bin/env bash
# Copyright 2026 The Rabbit Hole Tech, LLC
# SPDX-License-Identifier: Apache-2.0
#
# build/ci/dco.sh names every commit in a range that lacks a valid DCO
# sign-off. This builds a throwaway repository with one commit of each kind
# and checks that exactly the unsigned ones are named.
#
# Refs the repo's issue #52.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
dco="$here/dco.sh"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$work"
git init -q -b main .
tree="$(git mktree </dev/null)"

# mk <author email> <committer email> <message> <parent>...: writes a commit
# with commit-tree (plumbing, so no commit hook runs on the made-up
# identities) and prints its id.
mk() {
  local author="$1" committer="$2" msg="$3" parents=() p
  shift 3
  for p in "$@"; do parents+=(-p "$p"); done
  GIT_AUTHOR_NAME="Someone" GIT_AUTHOR_EMAIL="$author" GIT_AUTHOR_DATE="2026-01-01T00:00:00Z" \
  GIT_COMMITTER_NAME="Someone" GIT_COMMITTER_EMAIL="$committer" GIT_COMMITTER_DATE="2026-01-01T00:00:00Z" \
    git commit-tree "$tree" ${parents[@]+"${parents[@]}"} -m "$msg"
}
ada="ada@example.org"
signed_ada="Signed-off-by: Ada Dev <ada@example.org>"

base="$(mk "$ada" "$ada" "chore: base")"
c1="$(mk "$ada" "$ada" $'feat: signed\n\n'"$signed_ada" "$base")"
c2="$(mk "$ada" "$ada" "feat: unsigned" "$c1")"
c3="$(mk "$ada" "$ada" $'feat: signed as someone else\n\nSigned-off-by: Bob Other <bob@example.org>' "$c2")"
c4="$(mk "$ada" "$ada" $'feat: sign-off without an address\n\nSigned-off-by: Ada Dev' "$c3")"
c5="$(mk "49699333+dependabot[bot]@users.noreply.github.com" "$ada" "build(deps): bump a thing" "$c4")"
c6="$(mk "cai@example.org" "$ada" $'feat: author differs, committer signs\n\n'"$signed_ada" "$c5")"
side="$(mk "$ada" "$ada" $'feat: on a side branch\n\n'"$signed_ada" "$base")"
head="$(mk "$ada" "$ada" "Merge branch 'side'" "$c6" "$side")"

fail=0
out="$(bash "$dco" "$base" "$head" 2>&1)" && rc=0 || rc=$?
[ "$rc" -ne 0 ] || { echo "FAIL: unsigned commits passed" >&2; fail=1; }

for must in "feat: unsigned" "signed as someone else" "sign-off without an address"; do
  grep -qF "$must" <<<"$out" || { echo "FAIL: '$must' was not named" >&2; fail=1; }
done
for must_not in "feat: signed:" "build(deps)" "author differs" "on a side branch" "Merge branch"; do
  if grep -qE "$must_not" <<<"$out"; then echo "FAIL: '$must_not' was named but is fine" >&2; fail=1; fi
done

# An empty range passes.
bash "$dco" "$base" "$base" >/dev/null 2>&1 || { echo "FAIL: an empty range was refused" >&2; fail=1; }

[ "$fail" -eq 0 ] || { echo "$out" >&2; exit 1; }
echo "PASS: the DCO check names exactly the unsigned commits"
