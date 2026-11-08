// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	cb "github.com/zJiaJun/yaozhi/internal/corebluetooth"
)

const (
	batteryServiceUUID = "180F"
	batteryLevelUUID   = "2A19"
)

// runInfo connects to every BLE peripheral the system holds a connection to
// and reads its model number and battery level. It checks the binding
// (connect, discovery, read) without an ATVV remote at hand.
func runInfo(timeout time.Duration) error {
	start := time.Now()
	logf := func(format string, args ...any) {
		fmt.Printf("[%7.3f] %s\n", time.Since(start).Seconds(), fmt.Sprintf(format, args...))
	}
	done := make(chan error, 1)
	pending := map[cb.Peripheral]int{} // reads still outstanding
	var c *cb.Central
	send := func(err error) {
		select {
		case done <- err:
		default:
		}
	}

	finishOne := func(p cb.Peripheral) {
		if pending[p]--; pending[p] <= 0 {
			delete(pending, p)
			if len(pending) == 0 {
				send(nil)
			}
		}
	}

	c, err := cb.NewCentral(&cb.Delegate{
		StateChanged: func(s cb.State) {
			logf("蓝牙状态：%s", s)
			if s != cb.StatePoweredOn {
				if s != cb.StateUnknown && s != cb.StateResetting {
					send(fmt.Errorf("蓝牙不可用：%s", s))
				}
				return
			}
			ps := c.RetrieveConnected(deviceInfoUUID)
			if len(ps) == 0 {
				send(errors.New("系统里没有已连接、带设备信息服务的 BLE 设备"))
				return
			}
			for _, p := range ps {
				logf("已连接设备 %q id=%s，连接中", p.Name(), p.Identifier())
				pending[p] = 0
				c.Connect(p)
			}
		},
		Connected: func(p cb.Peripheral) {
			p.DiscoverServices(deviceInfoUUID, batteryServiceUUID)
		},
		FailedToConnect: func(p cb.Peripheral, err error) {
			logf("%q 连接失败：%v", p.Name(), err)
			finishOne(p)
		},
		ServicesDiscovered: func(p cb.Peripheral, err error) {
			if err != nil {
				logf("%q 查找服务失败：%v", p.Name(), err)
				finishOne(p)
				return
			}
			for _, s := range p.Services() {
				p.DiscoverCharacteristics(s, modelNumberUUID, batteryLevelUUID)
				pending[p]++
			}
			if pending[p] == 0 {
				finishOne(p)
			}
		},
		CharacteristicsDiscovered: func(p cb.Peripheral, s cb.Service, err error) {
			// Swap this service's slot for one per read issued.
			if err == nil {
				for _, ch := range s.Characteristics() {
					if u := ch.UUID(); strings.EqualFold(u, modelNumberUUID) || strings.EqualFold(u, batteryLevelUUID) {
						p.Read(ch)
						pending[p]++
					}
				}
			}
			finishOne(p)
		},
		ValueUpdated: func(p cb.Peripheral, ch cb.Characteristic, v []byte, err error) {
			switch {
			case err != nil:
				logf("%q 读取 %s 失败：%v", p.Name(), ch.UUID(), err)
			case strings.EqualFold(ch.UUID(), modelNumberUUID):
				logf("%q 型号：%q", p.Name(), strings.TrimRight(string(v), "\x00"))
			case strings.EqualFold(ch.UUID(), batteryLevelUUID) && len(v) > 0:
				logf("%q 电量：%d%%", p.Name(), v[0])
			}
			finishOne(p)
		},
	})
	if err != nil {
		return err
	}
	select {
	case err = <-done:
	case <-time.After(timeout):
		err = fmt.Errorf("%v 内没读完", timeout)
	}
	closed := make(chan struct{})
	c.Do(func() { c.Close(); close(closed) })
	<-closed
	return err
}
