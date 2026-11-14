// SPDX-License-Identifier: GPL-3.0-only

package app

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"

	"github.com/zJiaJun/yaozhi/internal/voice"
)

// Settings is what the user chose, kept across launches in a small JSON
// file. The zero value is the default: voice input on, no gain.
type Settings struct {
	// Paused turns voice input off: the voice key goes back to the system
	// and nothing is sent to the input method.
	Paused bool `json:"paused"`
	// GainDB makes the voice louder before it reaches the microphone, from
	// 0 to voice.MaxGainDB.
	GainDB float64 `json:"gain_db"`
	// TextSeen records that text was once seen to arrive from the input
	// method, in Yaozhi's own try box: the whole chain works. Only that it
	// happened is kept, never the text.
	TextSeen bool `json:"text_seen"`
	// RemoteSeen records that a remote was once connected: it is paired
	// with this Mac, and when it is not connected now, it is only asleep.
	RemoteSeen bool `json:"remote_seen"`
}

// valid returns s with its values within their ranges.
func (s Settings) valid() Settings {
	if math.IsNaN(s.GainDB) {
		s.GainDB = 0
	}
	s.GainDB = math.Max(0, math.Min(voice.MaxGainDB, s.GainDB))
	return s
}

// loadSettings reads the file; a missing or broken one is the defaults.
func loadSettings(path string) Settings {
	var s Settings
	if !loadJSON(path, &s) {
		s = Settings{}
	}
	return s.valid()
}

// loadJSON reads path into v, and reports whether it did. "" is no file.
func loadJSON(path string, v any) bool {
	if path == "" {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

// saveJSON writes v to path through a temporary file, so a crash leaves
// the old one whole. "" keeps nothing.
func saveJSON(path string, v any) error {
	if path == "" {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
