// SPDX-License-Identifier: GPL-3.0-only

// Package coreaudio is a small binding of macOS Core Audio through purego,
// without cgo: listing devices by UID and playing mono PCM on a chosen
// output device with an Audio Queue. It never changes a device's settings
// or the system default devices.
package coreaudio
