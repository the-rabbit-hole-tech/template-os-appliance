// Copyright 2026 The Example Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package sshsession_test

import (
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"example.org/appliance/internal/sshsession"
)

// A real process running as the closed shell is listed from the live /proc
// and really ends.
func TestALiveShellIsListedAndEnds(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	// /proc/<pid>/exe holds the resolved path, so the temp dir is resolved
	// too.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	shellCopy := filepath.Join(dir, "appliance-shell")
	copyFile(t, sleep, shellCopy)
	cmd := exec.Command(shellCopy, "300") // #nosec G204 -- test-only
	cmd.Env = []string{"SSH_CONNECTION=192.0.2.60 50123 192.0.2.10 22"}
	before := time.Now().Add(-2 * time.Second)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-exited })

	p := &sshsession.Proc{Shell: shellCopy, Wait: 5 * time.Second}
	got := listWhenSourced(t, p, cmd.Process.Pid)
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PID != cmd.Process.Pid || got[0].Admin != me.Username || got[0].Source != "192.0.2.60" {
		t.Fatalf("sessions %+v", got)
	}
	if got[0].Started.Before(before) || got[0].Started.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("started %v", got[0].Started)
	}
	if _, err := p.End(got[0].ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the shell is still running")
	}
	if left, err := p.List(); err != nil || len(left) != 0 {
		t.Fatalf("still listed %+v %v", left, err)
	}
}

// listWhenSourced lists the sessions until the one for pid shows its source
// address, for at most five seconds. The process was started a moment ago:
// right after the exec the kernel already names the new binary, but the
// environment the source is read from is not always readable yet, so the
// first listing can hold the session with an empty Source. If it never
// fills in, the failure says what /proc held for pid, which is what the
// flake in issue #50 lacked.
func listWhenSourced(t *testing.T, p *sshsession.Proc, pid int) []sshsession.Session {
	t.Helper()
	var got []sshsession.Session
	deadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		got, err = p.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 1 && got[0].PID == pid && got[0].Source != "" {
			return got
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	env, envErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ")) // #nosec G304 -- test-only procfs read
	stat, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))        // #nosec G304 -- test-only procfs read
	exe, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	t.Fatalf("no session with a source after 5s: sessions %+v; /proc/%d: environ %q (err %v), stat %q, exe %q", got, pid, env, envErr, stat, exe)
	return got
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from) // #nosec G304 -- test-only
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY, 0o755) // #nosec G302 G304 -- test-only executable
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
