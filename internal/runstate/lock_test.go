//go:build unix

package runstate

import (
	"errors"
	"testing"
)

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	if IsRunning(dir) {
		t.Fatal("IsRunning before anything ran")
	}

	l, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if !IsRunning(dir) {
		t.Error("IsRunning false while the lock is held")
	}

	var held *HeldError
	if _, err := Acquire(dir); !errors.As(err, &held) {
		t.Fatalf("second Acquire: got %v, want a *HeldError", err)
	}

	l.Release()
	if IsRunning(dir) {
		t.Error("IsRunning true after Release")
	}
	l2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	l2.Release()
}

func TestStatusRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := Read(dir); ok || err != nil {
		t.Fatalf("Read of empty dir: ok=%v err=%v", ok, err)
	}

	want := Status{PID: 42, Phase: PhaseDownloading, Message: "downloading 1/2", Counts: Counts{Total: 2}}
	if err := Write(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Read(dir)
	if err != nil || !ok {
		t.Fatalf("Read: ok=%v err=%v", ok, err)
	}
	if got.PID != 42 || got.Message != want.Message || got.Counts != want.Counts {
		t.Errorf("round trip: got %+v", got)
	}
	if got.Running || !got.Active() {
		t.Errorf("no lock held but downloading: want Running=false Active=true, got %v %v", got.Running, got.Active())
	}
}
