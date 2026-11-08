// SPDX-License-Identifier: GPL-3.0-only

// Package tomlfile loads the TOML data files Yaozhi uses (device descriptors,
// input method presets): strict decoding plus loading a directory of them.
package tomlfile

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/BurntSushi/toml"
)

// Decode decodes data into v. Unknown fields are an error so that typos in
// hand-written files do not go unnoticed.
func Decode(data []byte, v any) error {
	md, err := toml.Decode(string(data), v)
	if err != nil {
		return err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		names := make([]string, len(undecoded))
		for i, k := range undecoded {
			names[i] = k.String()
		}
		return fmt.Errorf("unknown field(s): %s", strings.Join(names, ", "))
	}
	return nil
}

// LoadFS parses every *.toml file at the top level of fsys, in name order.
// Files that fail to parse are skipped and reported together in err, so one
// broken file does not hide the others. A file whose id (as returned by id)
// was already loaded is an error too.
func LoadFS[T any](fsys fs.FS, parse func([]byte) (T, error), id func(T) string) (items []T, err error) {
	names, err := fs.Glob(fsys, "*.toml")
	if err != nil {
		return nil, err
	}
	var errs []error
	seen := map[string]string{}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		item, err := parse(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if prev, dup := seen[id(item)]; dup {
			errs = append(errs, fmt.Errorf("%s: id %q is already used by %s", name, id(item), prev))
			continue
		}
		seen[id(item)] = name
		items = append(items, item)
	}
	return items, errors.Join(errs...)
}

// Merge returns base with every item of extra added, replacing the base item
// with the same id. base is not modified.
func Merge[T any](base, extra []T, id func(T) string) []T {
	out := append([]T(nil), base...)
	for _, e := range extra {
		replaced := false
		for i := range out {
			if id(out[i]) == id(e) {
				out[i], replaced = e, true
				break
			}
		}
		if !replaced {
			out = append(out, e)
		}
	}
	return out
}
