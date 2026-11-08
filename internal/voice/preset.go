// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zJiaJun/yaozhi/internal/tomlfile"
)

// Trigger modes.
const (
	// ModeToggle: one tap starts listening, another tap stops (Typeless).
	ModeToggle = "toggle"
	// ModeHold: the input method listens while the key is held.
	ModeHold = "hold"
)

// KeyFn is the only trigger key supported so far: the Fn/Globe key.
const KeyFn = "fn"

// Preset describes how to drive one "virtual microphone + shortcut" input
// method. It is decoded from a TOML file; see presets/typeless.toml.
type Preset struct {
	ID   string `toml:"id"`
	Name string `toml:"name"`
	// Microphone is the input the user has to select in the input method,
	// shown during setup.
	Microphone string  `toml:"microphone"`
	Trigger    Trigger `toml:"trigger"`
	Timing     Timing  `toml:"timing"`
}

// Trigger is the shortcut that starts and stops the input method.
type Trigger struct {
	Key  string `toml:"key"`
	Mode string `toml:"mode"`
}

// Timing holds the waits around the trigger, in milliseconds. Zero means the
// default where one exists.
type Timing struct {
	// TapMS is how long a tap holds the key down (ModeToggle). Default 120.
	TapMS int `toml:"tap_ms"`
	// ReadyMS is how long to wait after starting the input method before the
	// buffered audio is released into the microphone.
	ReadyMS int `toml:"ready_ms"`
	// EndDelayMS is how long to wait after the last audio has played before
	// stopping the input method.
	EndDelayMS int `toml:"end_delay_ms"`
}

// DefaultTapMS is the tap length remote-mic-app uses for Typeless.
const DefaultTapMS = 120

// Tap returns the tap length.
func (t Timing) Tap() time.Duration {
	if t.TapMS == 0 {
		return DefaultTapMS * time.Millisecond
	}
	return time.Duration(t.TapMS) * time.Millisecond
}

// Ready returns ReadyMS as a duration.
func (t Timing) Ready() time.Duration { return time.Duration(t.ReadyMS) * time.Millisecond }

// EndDelay returns EndDelayMS as a duration.
func (t Timing) EndDelay() time.Duration { return time.Duration(t.EndDelayMS) * time.Millisecond }

var presetIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// maxWaitMS bounds every timing field; longer waits are surely mistakes.
const maxWaitMS = 2000

// Validate reports the first problem that would make the preset unusable.
func (p *Preset) Validate() error {
	switch {
	case !presetIDPattern.MatchString(p.ID):
		return fmt.Errorf("id %q: use lowercase letters, digits and dashes", p.ID)
	case strings.TrimSpace(p.Name) == "":
		return errors.New("name is empty")
	case p.Trigger.Key != KeyFn:
		return fmt.Errorf("trigger.key %q is not supported (known: %q)", p.Trigger.Key, KeyFn)
	case p.Trigger.Mode != ModeToggle && p.Trigger.Mode != ModeHold:
		return fmt.Errorf("trigger.mode %q: use %q or %q", p.Trigger.Mode, ModeToggle, ModeHold)
	}
	for name, v := range map[string]int{"tap_ms": p.Timing.TapMS, "ready_ms": p.Timing.ReadyMS, "end_delay_ms": p.Timing.EndDelayMS} {
		if v < 0 || v > maxWaitMS {
			return fmt.Errorf("timing.%s = %d: use 0 to %d", name, v, maxWaitMS)
		}
	}
	return nil
}

// ParsePreset decodes and validates one preset.
func ParsePreset(data []byte) (Preset, error) {
	var p Preset
	if err := tomlfile.Decode(data, &p); err != nil {
		return Preset{}, err
	}
	if err := p.Validate(); err != nil {
		return Preset{}, err
	}
	return p, nil
}

func presetID(p Preset) string { return p.ID }

//go:embed presets/*.toml
var builtinPresets embed.FS

// BuiltinPresets returns the presets compiled into Yaozhi.
func BuiltinPresets() []Preset {
	sub, err := fs.Sub(builtinPresets, "presets")
	if err != nil {
		panic(err)
	}
	ps, err := tomlfile.LoadFS(sub, ParsePreset, presetID)
	if err != nil {
		// Built-in files are checked by the tests; reaching this is a bug.
		panic(fmt.Sprintf("voice: built-in preset: %v", err))
	}
	return ps
}

// Presets returns the built-in presets plus those in userDir (which may not
// exist); a user file replaces the built-in preset with the same id. err
// reports user files that were skipped; the list is usable anyway.
func Presets(userDir string) ([]Preset, error) {
	ps := BuiltinPresets()
	if userDir == "" {
		return ps, nil
	}
	user, err := tomlfile.LoadFS(os.DirFS(userDir), ParsePreset, presetID)
	if err != nil {
		err = fmt.Errorf("%s: %w", filepath.Clean(userDir), err)
	}
	return tomlfile.Merge(ps, user, presetID), err
}
