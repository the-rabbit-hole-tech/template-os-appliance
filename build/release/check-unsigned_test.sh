#!/usr/bin/env bash
# Copyright 2026 The Example Authors
# SPDX-License-Identifier: Apache-2.0
#
# The unsigned-build check: a directory of unsigned UKIs and disk images
# with a matching SHA256SUMS passes; a signed UKI, a UKI sbverify can't
# read, a signature, certificate or key file, private key material inside
# any file, and a SHA256SUMS that's missing, stale or incomplete all fail.
#
# sbverify is a stand-in (SBVERIFY) that answers the way the real one does
# for an unsigned, a signed and an unreadable image, so this runs on any
# host. With sbsign, sbverify, openssl and systemd's EFI stub installed it
# also runs the real tools once over a real unsigned and signed PE.
# shellcheck disable=SC2094 # SHA256SUMS is skipped in its own listing
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/check-unsigned.sh"
fail=0
ok() { echo "ok   $*"; }
bad() {
  echo "FAIL $*" >&2
  fail=1
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cat >"$work/sbverify" <<'EOF'
#!/usr/bin/env bash
[ "$1" = --list ] || exit 2
case "$(head -c 8 "$2")" in
  UNSIGNED) echo "No signature table present"; exit 1 ;;
  SIGNED..) printf 'signature 1\nimage signature issuers:\n - /CN=test\n'; exit 0 ;;
  *) echo "Can't open image $2" >&2; exit 1 ;;
esac
EOF
chmod +x "$work/sbverify"
export SBVERIFY="$work/sbverify"

sums() { (cd "$1" && for f in *; do [ "$f" = SHA256SUMS ] || shasum -a 256 "$f"; done >SHA256SUMS); }

# good <dir>: an unsigned proof build, named the way a lab build names it.
good() {
  rm -rf "$1"
  mkdir -p "$1"
  printf 'UNSIGNED a UKI\n' >"$1/appliance-lab-n3-amd64.efi"
  printf 'a raw disk\n' >"$1/appliance-lab-n3-amd64.raw"
  printf 'an ova\n' >"$1/appliance-lab-n3-amd64.ova"
  sums "$1"
}

# run <name> <want exit> <want text> <dir>
run() {
  local out rc=0
  out="$(bash "$script" "$4" 2>&1)" || rc=$?
  if [ "$rc" != "$2" ]; then
    bad "$1: exit $rc, want $2: $out"
  elif [[ "$out" != *"$3"* ]]; then
    bad "$1: no '$3' in: $out"
  else
    ok "$1"
  fi
}

d="$work/dist"
good "$d"
run "an unsigned build" 0 "1 UKI(s), no signature and no key" "$d"

good "$d"
printf 'SIGNED.. a UKI\n' >"$d/appliance-lab-n3-amd64.efi"
sums "$d"
run "a signed UKI" 1 "appliance-lab-n3-amd64.efi carries an Authenticode signature" "$d"

good "$d"
printf 'not a PE\n' >"$d/appliance-lab-n3-amd64.efi"
sums "$d"
run "a UKI sbverify can't read" 1 "sbverify can't read it as a PE image" "$d"

good "$d"
rm "$d/appliance-lab-n3-amd64.efi"
sums "$d"
run "no UKI at all" 1 "no UKI (*.efi)" "$d"

for f in SHA256SUMS.sigstore.json db.crt db.key update.age appliance-lab-n3-amd64.ova.sig; do
  good "$d"
  printf 'x\n' >"$d/$f"
  sums "$d"
  run "a $f file" 1 "$f is a signature, certificate, key or attestation file" "$d"
done

good "$d"
printf 'header\n-----BEGIN %s-----\nMIIfake\n' "PRIVATE KEY" >"$d/appliance-lab-n3-amd64.raw"
sums "$d"
run "a PEM private key inside a disk image" 1 "appliance-lab-n3-amd64.raw holds private key material" "$d"

good "$d"
printf 'AGE-SECRET-KEY-1%s\n' "QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ" >"$d/notes.txt"
sums "$d"
run "an age identity in any file" 1 "notes.txt holds private key material" "$d"

good "$d"
rm "$d/SHA256SUMS"
run "no SHA256SUMS" 1 "no SHA256SUMS" "$d"

good "$d"
printf 'changed\n' >>"$d/appliance-lab-n3-amd64.ova"
run "a stale SHA256SUMS" 1 "SHA256SUMS doesn't verify for appliance-lab-n3-amd64.ova" "$d"

good "$d"
printf 'late\n' >"$d/appliance-lab-n3-amd64.qcow2"
run "a file SHA256SUMS doesn't list" 1 "appliance-lab-n3-amd64.qcow2 isn't in SHA256SUMS" "$d"

run "a missing directory" 1 "no such directory" "$work/nowhere"

stub="${EFI_STUB:-/usr/lib/systemd/boot/efi/linuxx64.efi.stub}"
if command -v sbsign >/dev/null 2>&1 && command -v sbverify >/dev/null 2>&1 &&
  command -v openssl >/dev/null 2>&1 && [ -f "$stub" ]; then
  unset SBVERIFY
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=throwaway test key" \
    -keyout "$work/test.key" -out "$work/test.crt" 2>/dev/null
  good "$d"
  cp "$stub" "$d/appliance-lab-n3-amd64.efi"
  sums "$d"
  run "the real sbverify, an unsigned stub" 0 "no signature and no key" "$d"
  sbsign --key "$work/test.key" --cert "$work/test.crt" --output "$d/appliance-lab-n3-amd64.efi" "$stub" >/dev/null 2>&1
  sums "$d"
  run "the real sbverify, a signed stub" 1 "carries an Authenticode signature" "$d"
else
  echo "skip the real sbsign and sbverify (not installed)"
fi

[ "$fail" = 0 ] || exit 1
echo "check-unsigned_test: all passed"
