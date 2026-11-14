// SPDX-License-Identifier: GPL-3.0-only

package app

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLockInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "running.lock")

	unlock, _, err := LockInstance(path, "/Applications/Yaozhi.app")
	if err != nil {
		t.Fatal(err)
	}
	// flock locks belong to an open file, so a second open in this same
	// process stands for another Yaozhi.
	if _, other, err := LockInstance(path, "build/Yaozhi.app"); !errors.Is(err, ErrRunning) || other != "/Applications/Yaozhi.app" {
		t.Fatalf("second lock: other %q, err %v; want the first holder and ErrRunning", other, err)
	}

	unlock()
	unlock2, _, err := LockInstance(path, "build/Yaozhi.app")
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	defer unlock2()
	if _, other, _ := LockInstance(path, "x"); other != "build/Yaozhi.app" {
		t.Fatalf("holder %q, want the new one, not what was left before", other)
	}
}
