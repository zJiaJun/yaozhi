// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package app

import (
	"context"
	"time"

	"github.com/zJiaJun/yaozhi/internal/device"
	"github.com/zJiaJun/yaozhi/internal/macinput"
)

// keyBlocker keeps the remote's voice key from reaching the system as an
// ordinary key press (F5 on RC003). The mapping lives on the HID service,
// which the system recreates when the remote reconnects, so it is applied
// again every time the remote becomes ready, and once more shortly after in
// case the HID side shows up later than Bluetooth LE.
type keyBlocker struct {
	logf    func(string, ...any)
	ch      chan *device.Descriptor
	restore func() error
}

func newKeyBlocker(logf func(string, ...any)) *keyBlocker {
	return &keyBlocker{logf: logf, ch: make(chan *device.Descriptor, 4)}
}

// block asks for the voice key of d to be blocked. Safe from any goroutine.
func (b *keyBlocker) block(d *device.Descriptor) {
	if d == nil {
		return
	}
	select {
	case b.ch <- d:
	default:
	}
}

func (b *keyBlocker) run(ctx context.Context) {
	retry := time.NewTimer(time.Hour)
	retry.Stop()
	var current *device.Descriptor
	for {
		select {
		case d := <-b.ch:
			current = d
			b.apply(d)
			retry.Reset(2 * time.Second)
		case <-retry.C:
			b.apply(current)
		case <-ctx.Done():
			retry.Stop()
			if b.restore != nil {
				if err := b.restore(); err != nil {
					b.logf("恢复语音键失败：%v", err)
				}
			}
			return
		}
	}
}

func (b *keyBlocker) apply(d *device.Descriptor) {
	usage, ok := d.VoiceUsage()
	if !ok || d.Match.Vendor == 0 {
		return
	}
	if b.restore != nil {
		_ = b.restore() // services of an earlier connection may be gone
		b.restore = nil
	}
	n, restore, err := macinput.RemapKey(int(d.Match.Vendor), int(d.Match.Product), macinput.Usage(usage), macinput.UsageNone)
	if err != nil {
		b.logf("屏蔽 %s 的语音键失败：%v", d.Name, err)
		return
	}
	b.restore = restore
	b.logf("已屏蔽 %s 的语音键（%d 个 HID 服务）", d.Name, n)
}
