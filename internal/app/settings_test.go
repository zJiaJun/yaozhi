// SPDX-License-Identifier: GPL-3.0-only

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	if s := loadSettings(path); s != (Settings{}) {
		t.Fatalf("missing file: %+v", s)
	}
	want := Settings{Paused: true, GainDB: 6}
	if err := saveJSON(path, want); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(path); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if err := os.WriteFile(path, []byte(`{"gain_db": 99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(path); got.GainDB != 20 || got.Paused {
		t.Fatalf("a gain out of range is brought back into it: %+v", got)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(path); got != (Settings{}) {
		t.Fatalf("broken file: %+v", got)
	}
}
