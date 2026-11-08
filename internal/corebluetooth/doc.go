// SPDX-License-Identifier: GPL-3.0-only

// Package corebluetooth is a small binding of macOS CoreBluetooth (central
// role only) through purego, without cgo, so it builds with CGO_ENABLED=0
// like MyGo. It covers what an ATVV remote needs: finding connected or
// advertising peripherals, connecting, discovering services and
// characteristics, subscribing, reading and writing.
//
// All CoreBluetooth calls run on one serial dispatch queue per Central:
// Delegate functions are called there, and other goroutines reach it with
// Central.Do.
package corebluetooth
