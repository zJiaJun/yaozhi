// SPDX-License-Identifier: GPL-3.0-only

package device

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/zJiaJun/yaozhi/internal/tomlfile"
)

// Protocols an adapter exists for. A descriptor naming any other protocol is
// rejected.
const (
	ProtocolATVV = "atvv" // Android TV Voice over BLE, package atvvble
)

// Descriptor describes one device model. It is decoded from a TOML file; see
// devices/xiaomi-rc003.toml for an annotated example.
type Descriptor struct {
	// ID is a stable lowercase identifier such as "xiaomi-rc003", used in
	// settings and as the override key for user files.
	ID string `toml:"id"`
	// Name is shown to the user.
	Name  string `toml:"name"`
	Match Match  `toml:"match"`
	Voice Voice  `toml:"voice"`
	// Keys maps key names (lowercase, e.g. "volume_up") to the HID usage the
	// system receives for that key: usage page << 32 | usage, the format
	// macOS key remapping uses (keyboard F5 is 0x7_0000_003E).
	Keys map[string]uint64 `toml:"keys"`
}

// Match tells an adapter which devices this descriptor covers. A device
// matches when its name or model number is listed; the HID IDs are used to
// find the device's keys on the system side.
type Match struct {
	// Names are Bluetooth names, compared case-insensitively after trimming
	// spaces.
	Names []string `toml:"names"`
	// Models are Device Information model numbers, compared exactly.
	Models []string `toml:"models"`
	// Vendor and Product are the HID vendor and product IDs.
	Vendor  uint16 `toml:"vendor"`
	Product uint16 `toml:"product"`
}

// Voice describes the device's microphone.
type Voice struct {
	// Protocol selects the adapter; see the Protocol constants.
	Protocol string `toml:"protocol"`
	// Key names the entry in Keys that starts talking. Besides starting the
	// audio stream, the key also reaches the system as an ordinary key press,
	// which Yaozhi blocks so it does not trigger anything else.
	Key string `toml:"key"`
}

// MatchesName reports whether a Bluetooth name belongs to this device.
func (d *Descriptor) MatchesName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && slices.ContainsFunc(d.Match.Names, func(n string) bool {
		return strings.EqualFold(strings.TrimSpace(n), name)
	})
}

// MatchesModel reports whether a Device Information model number belongs to
// this device.
func (d *Descriptor) MatchesModel(model string) bool {
	model = strings.TrimSpace(model)
	return model != "" && slices.Contains(d.Match.Models, model)
}

// VoiceUsage returns the HID usage of the voice key, if the descriptor has one.
func (d *Descriptor) VoiceUsage() (uint64, bool) {
	u, ok := d.Keys[d.Voice.Key]
	return u, ok && d.Voice.Key != ""
}

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	keyPattern = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)
)

// Validate reports the first problem that would make the descriptor unusable.
func (d *Descriptor) Validate() error {
	switch {
	case !idPattern.MatchString(d.ID):
		return fmt.Errorf("id %q: use lowercase letters, digits and dashes", d.ID)
	case strings.TrimSpace(d.Name) == "":
		return errors.New("name is empty")
	case len(d.Match.Names) == 0 && len(d.Match.Models) == 0:
		return errors.New("match needs at least one entry in names or models")
	case d.Voice.Protocol != ProtocolATVV:
		return fmt.Errorf("voice.protocol %q is not supported (known: %q)", d.Voice.Protocol, ProtocolATVV)
	}
	for _, k := range slices.Sorted(maps.Keys(d.Keys)) {
		if !keyPattern.MatchString(k) {
			return fmt.Errorf("key name %q: use lowercase letters, digits and underscores", k)
		}
		// Usage pages and usages are 16-bit; anything else is a typo (or a
		// negative number, which TOML lets through as a huge unsigned one).
		if u := d.Keys[k]; u>>32 == 0 || u>>48 != 0 || u&0xFFFF_0000 != 0 {
			return fmt.Errorf("key %q: usage %#x is not page<<32|usage with 16-bit page and usage", k, u)
		}
	}
	if d.Voice.Key != "" {
		if _, ok := d.Keys[d.Voice.Key]; !ok {
			return fmt.Errorf("voice.key %q is not in [keys]", d.Voice.Key)
		}
	}
	return nil
}

// Parse decodes and validates one descriptor. Unknown fields are an error so
// that typos do not go unnoticed.
func Parse(data []byte) (Descriptor, error) {
	var d Descriptor
	if err := tomlfile.Decode(data, &d); err != nil {
		return Descriptor{}, err
	}
	if err := d.Validate(); err != nil {
		return Descriptor{}, err
	}
	return d, nil
}

func descriptorID(d Descriptor) string { return d.ID }

// LoadFS reads every *.toml file at the top level of fsys, in name order.
// Files that fail to parse are skipped and reported together in err, so one
// broken file does not hide the others. Two files with the same id are an
// error for the second one.
func LoadFS(fsys fs.FS) ([]Descriptor, error) {
	return tomlfile.LoadFS(fsys, Parse, descriptorID)
}

//go:embed devices/*.toml
var builtin embed.FS

// Builtin returns the descriptors compiled into Yaozhi.
func Builtin() []Descriptor {
	sub, err := fs.Sub(builtin, "devices")
	if err != nil {
		panic(err)
	}
	ds, err := LoadFS(sub)
	if err != nil {
		// Built-in files are checked by the tests; reaching this is a bug.
		panic(fmt.Sprintf("device: built-in descriptor: %v", err))
	}
	return ds
}

// Catalog returns the built-in descriptors plus those in userDir (which may
// not exist). A user file whose id matches a built-in one replaces it. err
// reports user files that were skipped; the returned list is usable anyway.
func Catalog(userDir string) ([]Descriptor, error) {
	ds := Builtin()
	if userDir == "" {
		return ds, nil
	}
	// fs.Glob ignores a missing directory, so no user files is not an error.
	user, err := LoadFS(os.DirFS(userDir))
	if err != nil {
		err = fmt.Errorf("%s: %w", filepath.Clean(userDir), err)
	}
	return tomlfile.Merge(ds, user, descriptorID), err
}
