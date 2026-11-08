// SPDX-License-Identifier: GPL-3.0-only

// Package macinput reads and changes how macOS sees keys, through purego
// without cgo: per-device HID key remapping (UserKeyMapping, the mechanism
// behind hidutil) and posting synthetic Fn presses with CGEvent.
package macinput
