// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package keycustody_test

import (
	"bytes"
	"errors"
	"testing"

	"example.org/appliance/internal/codes"
	"example.org/appliance/internal/keycustody"
)

// TestSealForImageUnlocksTheNextRelease: a copy sealed to the predicted
// PCR 11 alone (the install copy pruned) still unlocks when PCR 11 holds
// that value.
func TestSealForImageUnlocksTheNextRelease(t *testing.T) {
	needCryptsetup(t)
	sim := simTPM(t)
	d := &fileDisk{dir: t.TempDir()}
	kc := keycustody.New(keycustody.Deps{TPM: sim, Disk: d})
	if err := kc.Initialize(ctx, keycustody.ModeTPM, keycustody.SBOn); err != nil {
		t.Fatal(err)
	}
	if err := sim.ExtendPCR(11, []byte("the running UKI")); err != nil {
		t.Fatal(err)
	}
	pcrs, err := sim.ReadPCRs([]int{11})
	if err != nil {
		t.Fatal(err)
	}
	if err := kc.SealForImage(ctx, "next-uki-sha", pcrs[11]); err != nil {
		t.Fatal(err)
	}
	if err := kc.Prune(ctx, []string{"next-uki-sha"}); err != nil {
		t.Fatal(err)
	}
	if err := keycustody.New(keycustody.Deps{TPM: sim, Disk: d}).Unlock(ctx); err != nil {
		t.Fatalf("the next release's copy must unlock: %v", err)
	}
	if err := kc.Prune(ctx, []string{"nothing"}); !codes.Is(err, codes.KeyCustodyInvalid) {
		t.Fatalf("pruning every copy must be refused: %v", err)
	}
}

func TestSealForImageWithSecureBootOffIsRefused(t *testing.T) {
	needCryptsetup(t)
	sim := simTPM(t)
	kc := keycustody.New(keycustody.Deps{TPM: sim, Disk: &fileDisk{dir: t.TempDir()}})
	if err := kc.Initialize(ctx, keycustody.ModeTPM, keycustody.SBOff); err != nil {
		t.Fatal(err)
	}
	if err := kc.SealForImage(ctx, "x", make([]byte, 32)); !codes.Is(err, codes.UpgradeUnpredictable) {
		t.Fatalf("got %v", err)
	}
}

// TestCheckPredictionTrustsAMatchingBoot: the running UKI's prediction is
// the PCR 11 this boot measured, so the predictor is trusted.
func TestCheckPredictionTrustsAMatchingBoot(t *testing.T) {
	needCryptsetup(t)
	sim := simTPM(t)
	kc := keycustody.New(keycustody.Deps{TPM: sim, Disk: &fileDisk{dir: t.TempDir()}})
	if err := kc.Initialize(ctx, keycustody.ModeTPM, keycustody.SBOn); err != nil {
		t.Fatal(err)
	}
	if err := sim.ExtendPCR(11, []byte("the running UKI")); err != nil {
		t.Fatal(err)
	}
	pcrs, err := sim.ReadPCRs([]int{11})
	if err != nil {
		t.Fatal(err)
	}
	if err := kc.CheckPrediction(ctx, func() ([]byte, error) { return pcrs[11], nil }); err != nil {
		t.Fatal(err)
	}
}

// TestCheckPredictionRefusesABootItDoesntMatch: PCR 11 isn't what the
// running UKI predicts, so the stub measures something the predictor
// doesn't know, and nothing may be sealed to its prediction.
func TestCheckPredictionRefusesABootItDoesntMatch(t *testing.T) {
	needCryptsetup(t)
	sim := simTPM(t)
	kc := keycustody.New(keycustody.Deps{TPM: sim, Disk: &fileDisk{dir: t.TempDir()}})
	if err := kc.Initialize(ctx, keycustody.ModeTPM, keycustody.SBOn); err != nil {
		t.Fatal(err)
	}
	// A fresh simulator's PCR 11 is all zeros; a prediction of anything else
	// doesn't match it.
	wrong := bytes.Repeat([]byte{0xab}, 32)
	if err := kc.CheckPrediction(ctx, func() ([]byte, error) { return wrong, nil }); !codes.Is(err, codes.UpgradeUnpredictable) {
		t.Fatalf("got %v", err)
	}
	broken := errors.New("no entry")
	if err := kc.CheckPrediction(ctx, func() ([]byte, error) { return nil, broken }); !codes.Is(err, codes.UpgradeUnpredictable) || !errors.Is(err, broken) {
		t.Fatalf("a prediction that failed: %v", err)
	}
}

// TestCheckPredictionInKeyFileModeChecksNothing: with no copy sealed to
// PCR 11 there's nothing to protect, and the running UKI isn't even read.
func TestCheckPredictionInKeyFileModeChecksNothing(t *testing.T) {
	needCryptsetup(t)
	kc := keycustody.New(keycustody.Deps{Disk: &fileDisk{dir: t.TempDir()}})
	if err := kc.Initialize(ctx, keycustody.ModeKeyfile, keycustody.SBOff); err != nil {
		t.Fatal(err)
	}
	if err := kc.CheckPrediction(ctx, func() ([]byte, error) {
		t.Fatal("key-file mode read the running UKI")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
}
