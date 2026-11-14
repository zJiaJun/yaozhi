// SPDX-License-Identifier: GPL-3.0-only

package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrRunning: another Yaozhi holds the instance lock.
var ErrRunning = errors.New("another Yaozhi is running")

// LockInstance makes sure that one Yaozhi runs at a time, whichever build.
// The development build and the release have bundle identifiers of their
// own, so the system and MyGo's single instance lock take them for two
// apps; but two of them at once would both take the remote, both block its
// voice key and both tap Fn, and the input method, tapped twice, would stop
// as soon as it starts.
//
// The lock is a flock on path, which both builds share: the system lets go
// of it when the process ends, however it ends. holder says who holds it,
// and is written into the file for the next one to tell the user. When
// another Yaozhi holds it, LockInstance returns ErrRunning and what that
// one wrote (possibly "").
//
// The lock lasts until unlock is called, and the caller must keep unlock
// reachable until then: the file is closed with it.
func LockInstance(path, holder string) (unlock func(), other string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, "", err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, "", err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		defer f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			b, _ := io.ReadAll(io.LimitReader(f, 4096))
			return nil, strings.TrimSpace(string(b)), ErrRunning
		}
		return nil, "", err
	}
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(holder+"\n"), 0)
	}
	return func() { f.Close() }, "", nil
}
