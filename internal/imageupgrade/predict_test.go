// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package imageupgrade_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"example.org/appliance/internal/codes"
	"example.org/appliance/internal/imageupgrade"
	"example.org/appliance/internal/ukipcr"
	"example.org/appliance/internal/verify"
	"example.org/appliance/test/kit/fixtures"
)

// checkingSealer is a Sealer that also checks the PCR 11 prediction
// against this boot, the way keycustody does in TPM mode.
type checkingSealer struct {
	*fakeSealer
	check func(ctx context.Context, predictRunning func() ([]byte, error)) error
}

func (c checkingSealer) CheckPrediction(ctx context.Context, predictRunning func() ([]byte, error)) error {
	return c.check(ctx, predictRunning)
}

// runningUKI puts a real UKI-shaped PE on the ESP as the running release.
func runningUKI(t *testing.T, s *imageupgrade.Stager) []byte {
	t.Helper()
	uki := fixtures.PE(t, fixtures.Section{Name: ".linux", Data: []byte("the running kernel")},
		fixtures.Section{Name: ".cmdline", Data: []byte("appliance.version=" + s.Running)})
	esp := s.ESP.(orderESP).dir
	if err := os.WriteFile(filepath.Join(esp, imageupgrade.UKIDir, imageupgrade.GoodName(s.Running)), uki, 0o600); err != nil {
		t.Fatal(err)
	}
	return uki
}

// When the running image's own prediction doesn't match the PCR 11 this
// boot measured, the predictor doesn't know something the stub measures,
// so a copy sealed to the new image's prediction wouldn't unseal. The stage
// is refused before the slot or the ESP is touched.
func TestAPredictionThatDoesntMatchThisBootRefusesTheStage(t *testing.T) {
	s, dir, slots, sealer, order := stager(t, "0.0.9")
	runningUKI(t, s)
	s.Sealer = checkingSealer{fakeSealer: sealer, check: func(context.Context, func() ([]byte, error)) error {
		return codes.New(codes.UpgradeUnpredictable, "PCR 11 doesn't match")
	}}
	if _, err := s.Stage(ctx, verify.LocalLayout(dir), "amd64"); !codes.Is(err, codes.UpgradeUnpredictable) {
		t.Fatalf("got %v", err)
	}
	if slots.written != 0 || len(sealer.sealed) != 0 || len(*order) != 0 {
		t.Fatalf("a refused stage wrote: slot %d sealed %v order %v", slots.written, sealer.sealed, *order)
	}
}

// The checker is handed the running UKI's prediction, read from the ESP
// entry of the running release.
func TestTheCheckerGetsTheRunningUKIsPrediction(t *testing.T) {
	s, dir, slots, sealer, _ := stager(t, "0.0.9")
	want, err := ukipcr.Predict(runningUKI(t, s))
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	s.Sealer = checkingSealer{fakeSealer: sealer, check: func(_ context.Context, predict func() ([]byte, error)) error {
		var err error
		got, err = predict()
		return err
	}}
	if _, err := s.Stage(ctx, verify.LocalLayout(dir), "amd64"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want.Value) {
		t.Fatalf("the checker got %x, want %x", got, want.Value)
	}
	if slots.written == 0 || len(sealer.sealed) != 1 {
		t.Fatalf("a checked stage didn't go on: slot %d sealed %v", slots.written, sealer.sealed)
	}
}

// A running UKI that isn't on the ESP, or that can't be predicted, can't
// vouch for the predictor, so the stage is refused.
func TestARunningUKIThatCantBePredictedRefusesTheStage(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, s *imageupgrade.Stager){
		"not a PE": func(*testing.T, *imageupgrade.Stager) {},
		"not on the ESP": func(t *testing.T, s *imageupgrade.Stager) {
			if err := os.Remove(filepath.Join(s.ESP.(orderESP).dir, imageupgrade.UKIDir, imageupgrade.GoodName(s.Running))); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, dir, slots, sealer, _ := stager(t, "0.0.9")
			setup(t, s)
			s.Sealer = checkingSealer{fakeSealer: sealer, check: func(_ context.Context, predict func() ([]byte, error)) error {
				_, err := predict()
				return err
			}}
			if _, err := s.Stage(ctx, verify.LocalLayout(dir), "amd64"); !codes.Is(err, codes.UpgradeUnpredictable) {
				t.Fatalf("got %v", err)
			}
			if slots.written != 0 {
				t.Fatal("the slot was written")
			}
		})
	}
}

// A Sealer with nothing to check (key-file mode, the tests' fakes) stages
// exactly as before.
func TestASealerWithNoCheckStagesAsBefore(t *testing.T) {
	s, dir, _, sealer, _ := stager(t, "0.0.9")
	if _, err := s.Stage(ctx, verify.LocalLayout(dir), "amd64"); err != nil || len(sealer.sealed) != 1 {
		t.Fatalf("%v %v", err, sealer.sealed)
	}
}
