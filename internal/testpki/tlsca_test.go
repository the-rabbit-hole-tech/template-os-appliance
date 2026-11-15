// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

package testpki_test

import (
	"testing"
	"time"

	"example.org/appliance/internal/clock"
	"example.org/appliance/internal/testpki"
)

// The code under test reads the time from a fake clock fixed at a past
// date, while the TLS stack in the same test reads the wall clock. A test
// CA must be valid at both, whatever day the suite runs on (issue #44: it
// was valid only from 30 days before the run, so it stopped covering the
// fake clock once the run date passed the fake date plus 30 days).
func TestTheTLSCAIsValidAtTheFakeClockAndTheWallClock(t *testing.T) {
	ca := testpki.NewTLSCA(t)
	for name, at := range map[string]time.Time{
		"the fake clock": clock.NewFake().Now(),
		"the wall clock": time.Now(),
	} {
		for _, c := range []struct {
			what      string
			notBefore time.Time
			notAfter  time.Time
		}{
			{"root", ca.Root.NotBefore, ca.Root.NotAfter},
			{"intermediate", ca.Intermediate.NotBefore, ca.Intermediate.NotAfter},
		} {
			if at.Before(c.notBefore) || at.After(c.notAfter) {
				t.Errorf("the %s is not valid at %s (%s): valid from %s to %s",
					c.what, name, at.UTC().Format(time.RFC3339),
					c.notBefore.UTC().Format(time.RFC3339), c.notAfter.UTC().Format(time.RFC3339))
			}
		}
	}
}
