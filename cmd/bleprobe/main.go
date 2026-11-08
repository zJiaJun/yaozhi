// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

// Command bleprobe is the M0 T0.2 check: it connects to an RC003 (or another
// ATVV remote) over BLE, subscribes to ATVV and prints what the remote sends
// while the voice key is held. It is verification code and may be thrown
// away once the device layer exists.
//
// The connection flow follows open-voice-bridge's XiaomiBluetoothBridge.swift
// (GPL-3.0): look up an already connected peripheral first, scan otherwise,
// subscribe Audio and Control, then request capabilities.
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/zJiaJun/yaozhi/internal/atvv"
	"github.com/zJiaJun/yaozhi/internal/coreaudio"
	cb "github.com/zJiaJun/yaozhi/internal/corebluetooth"
)

// blackHoleUID is the UID of the unmodified BlackHole 2ch driver.
const blackHoleUID = "BlackHole2ch_UID"

const (
	deviceInfoUUID  = "180A"
	modelNumberUUID = "2A24"
)

// rc003Names are the GAP names RC003 uses (open-voice-bridge
// VoiceBridgeDeviceProfile.swift), compared case-insensitively.
var rc003Names = []string{"mi rc", "xiaomi bluetooth remote 2 pro", "小米蓝牙语音遥控器"}

func isRC003Name(name string) bool {
	return slices.Contains(rc003Names, strings.ToLower(strings.TrimSpace(name)))
}

type options struct {
	id          string
	any         bool
	verbose     bool
	wavDir      string
	play        bool
	info        bool
	findTimeout time.Duration
	initTimeout time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.id, "id", "", "按 CoreBluetooth 标识连接（连上后会打印）")
	flag.BoolVar(&o.any, "any", false, "接受任何提供 ATVV 服务的设备，不只认 RC003 的名字")
	flag.BoolVar(&o.info, "info", false, "不找遥控器，只读出系统里已连接 BLE 设备的型号和电量，用来检查蓝牙绑定")
	flag.BoolVar(&o.verbose, "v", false, "打印每个音频包的内容")
	flag.BoolVar(&o.play, "play", false, "把解码后的语音实时播放到 BlackHole 2ch，并统计每段的排空时间")
	flag.StringVar(&o.wavDir, "wav", "", "把每段语音解码成 16 kHz WAV 写到这个目录（只用于开发自检）")
	flag.DurationVar(&o.findTimeout, "find-timeout", 30*time.Second, "找设备的超时")
	flag.DurationVar(&o.initTimeout, "init-timeout", 15*time.Second, "连上后完成 ATVV 初始化的超时")
	flag.Parse()

	run := run
	if o.info {
		run = func(o options) error { return runInfo(o.findTimeout) }
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "bleprobe:", err)
		os.Exit(1)
	}
}

// probe holds all state. Every field is touched only on the Central's queue.
type probe struct {
	o       options
	c       *cb.Central
	start   time.Time
	done    chan error
	ended   bool
	target  *cb.Peripheral
	tx      *cb.Characteristic
	audio   *cb.Characteristic
	control *cb.Characteristic
	subs    map[string]bool
	session *atvv.Session
	player  *coreaudio.Player // nil unless -play
	stream  streamStats
	streams int
	stopAt  time.Time // latest AUDIO_STOP
	late    int       // audio packets dropped after it
}

type streamStats struct {
	packets  int
	bytes    int
	decoded  int
	samples  []int16
	peak     int
	lastTick time.Time
	first    time.Time // first audio packet
	last     time.Time // latest audio packet
	lastPkt  []byte
	startAt  time.Time // AUDIO_START (or implicit start)
}

func run(o options) error {
	p := &probe{
		o:       o,
		start:   time.Now(),
		done:    make(chan error, 1),
		subs:    map[string]bool{},
		session: atvv.NewSession(nil),
	}
	if o.play {
		dev, err := coreaudio.DeviceByUID(blackHoleUID)
		if errors.Is(err, coreaudio.ErrNotFound) {
			return errors.New("没找到 BlackHole 2ch，请先安装原版：brew install blackhole-2ch")
		}
		if err != nil {
			return err
		}
		if p.player, err = coreaudio.NewPlayer(dev.UID, atvv.SupportedSampleRate); err != nil {
			return err
		}
		defer p.player.Close()
		if err := p.player.Start(); err != nil {
			return err
		}
		p.logf("语音会实时播放到 %s（%.0f Hz）", dev.Name, dev.SampleRate)
	}
	c, err := cb.NewCentral(&cb.Delegate{
		StateChanged:              p.stateChanged,
		Discovered:                p.discovered,
		Connected:                 p.connected,
		FailedToConnect:           func(_ cb.Peripheral, err error) { p.finish(fmt.Errorf("连接失败：%w", err)) },
		Disconnected:              p.disconnected,
		ServicesDiscovered:        p.servicesDiscovered,
		CharacteristicsDiscovered: p.characteristicsDiscovered,
		NotifyStateUpdated:        p.notifyStateUpdated,
		ValueUpdated:              p.valueUpdated,
	})
	if err != nil {
		return err
	}
	p.c = c
	p.logf("等待蓝牙就绪（第一次运行时 macOS 会请求蓝牙权限）")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	find := time.AfterFunc(o.findTimeout, func() {
		c.Do(func() {
			if p.target == nil {
				p.finish(fmt.Errorf("%v 内没找到遥控器：确认 RC003 已在系统蓝牙里配对并处于连接状态，或按任意键唤醒它", o.findTimeout))
			}
		})
	})
	defer find.Stop()

	select {
	case err = <-p.done:
	case <-sig:
		fmt.Println()
		p.logf("退出")
	}
	cleaned := make(chan struct{})
	c.Do(func() {
		if p.session.Streaming() && p.tx != nil {
			p.write(p.session.MicCloseCommand(), "MIC_CLOSE")
		}
		c.Close()
		close(cleaned)
	})
	select {
	case <-cleaned:
	case <-time.After(time.Second):
	}
	return err
}

func (p *probe) logf(format string, args ...any) {
	fmt.Printf("[%7.3f] %s\n", time.Since(p.start).Seconds(), fmt.Sprintf(format, args...))
}

func (p *probe) finish(err error) {
	if p.ended {
		return
	}
	p.ended = true
	p.done <- err
}

func (p *probe) stateChanged(s cb.State) {
	p.logf("蓝牙状态：%s", s)
	switch s {
	case cb.StatePoweredOn:
		if p.target == nil {
			p.findRemote()
		}
	case cb.StateUnauthorized:
		p.finish(errors.New("没有蓝牙权限：到 系统设置 → 隐私与安全性 → 蓝牙 里允许运行它的终端 App"))
	case cb.StateUnsupported:
		p.finish(errors.New("这台 Mac 不支持低功耗蓝牙"))
	case cb.StatePoweredOff:
		p.finish(errors.New("蓝牙已关闭"))
	}
}

func (p *probe) accept(name string, advertisesATVV bool) bool {
	return isRC003Name(name) || (p.o.any && advertisesATVV)
}

func (p *probe) findRemote() {
	if p.o.id != "" {
		if ps := p.c.RetrieveByIdentifier(p.o.id); len(ps) > 0 {
			p.logf("按标识找到 %q", ps[0].Name())
			p.connect(ps[0])
			return
		}
		p.logf("标识 %s 不在系统记录里，改为查找", p.o.id)
	}

	connected := p.c.RetrieveConnected(atvv.ServiceUUID)
	for _, cand := range connected {
		p.logf("系统已连接的 ATVV 设备：%q id=%s", cand.Name(), cand.Identifier())
	}
	for _, cand := range connected {
		if p.accept(cand.Name(), true) {
			p.connect(cand)
			return
		}
	}
	if len(connected) > 0 {
		p.logf("以上设备名字都不是 RC003；确认是 ATVV 遥控器的话加 -any")
	}

	p.logf("扫描广播中的设备……")
	p.c.Scan()
}

func (p *probe) discovered(cand cb.Peripheral, adv cb.Advertisement, rssi int) {
	if p.target != nil {
		return
	}
	atvvAdv := slices.ContainsFunc(adv.ServiceUUIDs, func(u string) bool { return strings.EqualFold(u, atvv.ServiceUUID) })
	name := cand.Name()
	if name == "" {
		name = adv.LocalName
	}
	if !(p.accept(name, atvvAdv) || isRC003Name(adv.LocalName) || (atvvAdv && isRC003Name(name))) {
		return
	}
	p.logf("扫描到 %q id=%s RSSI=%d", name, cand.Identifier(), rssi)
	p.c.StopScan()
	p.connect(cand)
}

func (p *probe) connect(cand cb.Peripheral) {
	p.target = &cand
	p.logf("连接 %q id=%s", cand.Name(), cand.Identifier())
	p.c.Connect(cand)
}

func (p *probe) isTarget(q cb.Peripheral) bool { return p.target != nil && p.target.Equal(q) }

func (p *probe) connected(q cb.Peripheral) {
	if !p.isTarget(q) {
		return
	}
	p.logf("已连接，查找服务")
	q.DiscoverServices(atvv.ServiceUUID, deviceInfoUUID)
	time.AfterFunc(p.o.initTimeout, func() {
		p.c.Do(func() {
			if !p.session.Ready() {
				p.finish(fmt.Errorf("%v 内没完成 ATVV 初始化", p.o.initTimeout))
			}
		})
	})
}

func (p *probe) disconnected(q cb.Peripheral, err error) {
	if !p.isTarget(q) {
		return
	}
	if err == nil {
		err = errors.New("连接已断开")
	}
	p.finish(fmt.Errorf("遥控器断开：%w", err))
}

func (p *probe) servicesDiscovered(q cb.Peripheral, err error) {
	if !p.isTarget(q) {
		return
	}
	if err != nil {
		p.finish(fmt.Errorf("查找服务失败：%w", err))
		return
	}
	foundATVV := false
	for _, s := range q.Services() {
		switch {
		case strings.EqualFold(s.UUID(), atvv.ServiceUUID):
			foundATVV = true
			q.DiscoverCharacteristics(s, atvv.TransmitUUID, atvv.AudioUUID, atvv.ControlUUID)
		case strings.EqualFold(s.UUID(), deviceInfoUUID):
			q.DiscoverCharacteristics(s, modelNumberUUID)
		}
	}
	if !foundATVV {
		p.finish(errors.New("设备没有 ATVV 语音服务"))
	}
}

func (p *probe) characteristicsDiscovered(q cb.Peripheral, s cb.Service, err error) {
	if !p.isTarget(q) {
		return
	}
	if err != nil {
		p.finish(fmt.Errorf("查找特征失败（%s）：%w", s.UUID(), err))
		return
	}
	for _, ch := range s.Characteristics() {
		ch := ch
		switch u := ch.UUID(); {
		case strings.EqualFold(u, modelNumberUUID):
			q.Read(ch)
		case strings.EqualFold(u, atvv.TransmitUUID):
			p.tx = &ch
		case strings.EqualFold(u, atvv.AudioUUID):
			p.audio = &ch
			q.SetNotify(ch, true)
		case strings.EqualFold(u, atvv.ControlUUID):
			p.control = &ch
			q.SetNotify(ch, true)
		}
	}
	if strings.EqualFold(s.UUID(), atvv.ServiceUUID) && (p.tx == nil || p.audio == nil || p.control == nil) {
		p.finish(errors.New("ATVV 特征不完整"))
	}
}

func (p *probe) notifyStateUpdated(q cb.Peripheral, ch cb.Characteristic, err error) {
	if !p.isTarget(q) {
		return
	}
	if err != nil {
		p.finish(fmt.Errorf("订阅 %s 失败：%w", ch.UUID(), err))
		return
	}
	if !ch.IsNotifying() {
		p.finish(fmt.Errorf("订阅 %s 未生效", ch.UUID()))
		return
	}
	p.subs[strings.ToUpper(ch.UUID())] = true
	p.logf("已订阅 %s", label(ch.UUID()))
	if p.subs[atvv.AudioUUID] && p.subs[atvv.ControlUUID] && p.tx != nil {
		p.write(atvv.GetCapabilities(), "GET_CAPS")
	}
}

func (p *probe) write(b []byte, what string) {
	withResponse := p.tx.Properties()&cb.PropWriteWithoutResponse == 0
	p.target.Write(*p.tx, b, withResponse)
	p.logf("→ %-9s %s", what, hex.EncodeToString(b))
}

func (p *probe) valueUpdated(q cb.Peripheral, ch cb.Characteristic, v []byte, err error) {
	if !p.isTarget(q) {
		return
	}
	if err != nil {
		p.logf("读取 %s 出错：%v", label(ch.UUID()), err)
		return
	}
	switch u := ch.UUID(); {
	case strings.EqualFold(u, atvv.ControlUUID):
		p.handleControl(v)
	case strings.EqualFold(u, atvv.AudioUUID):
		p.handleAudio(v)
	case strings.EqualFold(u, modelNumberUUID):
		p.logf("型号：%q", strings.TrimRight(string(v), "\x00"))
	}
}

func (p *probe) handleControl(v []byte) {
	ev, err := p.session.HandleControl(v)
	desc := ev.Kind.String()
	if len(v) > 0 {
		desc = fmt.Sprintf("%02x %s", v[0], desc)
	}
	p.logf("← control %s  [%s]", desc, hex.EncodeToString(v))
	switch {
	case errors.Is(err, atvv.ErrUnsupportedRate):
		p.finish(err)
		return
	case err != nil:
		p.logf("  忽略：%v", err)
		return
	}
	switch ev.Kind {
	case atvv.EventCapabilities:
		c := ev.Capabilities
		p.logf("ATVV 就绪：版本 %04x，%d Hz，帧长 %d。按住语音键说话，Ctrl-C 退出", c.Version, c.SampleRate, c.FrameSize)
	case atvv.EventMicButton:
		p.write(p.session.MicOpenCommand(), "MIC_OPEN")
	case atvv.EventAudioStart:
		p.beginStream()
	case atvv.EventAudioStop:
		p.endStream()
	}
}

func (p *probe) beginStream() {
	p.streams++
	now := time.Now()
	p.stream = streamStats{lastTick: now, startAt: now}
}

func (p *probe) handleAudio(v []byte) {
	pcm, started, err := p.session.HandleAudio(v)
	if err != nil {
		p.logf("← audio %d 字节，忽略：%v", len(v), err)
		return
	}
	if started {
		p.logf("（音频先于 AUDIO_START 到达，隐式开始）")
		p.beginStream()
	}
	if !p.session.Streaming() {
		p.late++
		p.logf("← audio %3d 字节：AUDIO_STOP 后 %d ms 到达，按迟到丢弃（本段第 %d 个）",
			len(v), time.Since(p.stopAt).Milliseconds(), p.late)
		return
	}
	s := &p.stream
	now := time.Now()
	if s.first.IsZero() {
		s.first = now
	}
	s.last = now
	s.lastPkt = v
	s.packets++
	s.bytes += len(v)
	s.decoded += len(pcm)
	for _, x := range pcm {
		if a := absInt(int(x)); a > s.peak {
			s.peak = a
		}
	}
	if p.player != nil {
		p.player.Write(pcm)
	}
	if p.o.wavDir != "" {
		s.samples = append(s.samples, pcm...)
	}
	switch {
	case p.o.verbose:
		p.logf("← audio %3d 字节 [%s]", len(v), hex.EncodeToString(v))
	case s.packets <= 3:
		p.logf("← audio %3d 字节（第 %d 包）", len(v), s.packets)
	case time.Since(s.lastTick) >= time.Second:
		p.logf("← audio 累计 %d 包 %d 字节", s.packets, s.bytes)
		s.lastTick = time.Now()
	}
}

func (p *probe) endStream() {
	s := p.stream
	if p.player != nil {
		// Measure off the Central's queue: waiting here would stall Bluetooth.
		pos, stop, n := p.player.Written(), time.Now(), p.streams
		go func() {
			ok := p.player.WaitPlayed(pos, 2*time.Second)
			p.c.Do(func() {
				if ok {
					p.logf("第 %d 段在 AUDIO_STOP 后 %d ms 播完", n, time.Since(stop).Milliseconds())
				} else {
					p.logf("第 %d 段 2 秒内没播完", n)
				}
			})
		}()
	}
	p.stopAt = time.Now()
	p.late = 0
	if !s.first.IsZero() {
		p.logf("首包距 AUDIO_START %d ms，末包距 AUDIO_STOP %d ms，按住 %d ms",
			s.first.Sub(s.startAt).Milliseconds(), p.stopAt.Sub(s.last).Milliseconds(), p.stopAt.Sub(s.startAt).Milliseconds())
		p.logf("末包 [%s]", hex.EncodeToString(s.lastPkt))
	}
	p.logf("第 %d 段语音结束：%d 包，%d 字节，解码 %.2f 秒，峰值 %s",
		p.streams, s.packets, s.bytes, float64(s.decoded)/atvv.SupportedSampleRate, dbfs(s.peak))
	if p.o.wavDir != "" && len(s.samples) > 0 {
		path := filepath.Join(p.o.wavDir, fmt.Sprintf("bleprobe-%s-%02d.wav", p.start.Format("150405"), p.streams))
		if err := writeWAV(path, s.samples, atvv.SupportedSampleRate); err != nil {
			p.logf("写 WAV 失败：%v", err)
		} else {
			p.logf("已写入 %s", path)
		}
	}
}

func label(uuid string) string {
	switch {
	case strings.EqualFold(uuid, atvv.AudioUUID):
		return "audio"
	case strings.EqualFold(uuid, atvv.ControlUUID):
		return "control"
	case strings.EqualFold(uuid, atvv.TransmitUUID):
		return "transmit"
	}
	return uuid
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func dbfs(peak int) string {
	if peak == 0 {
		return "-inf dBFS"
	}
	return fmt.Sprintf("%.1f dBFS", 20*math.Log10(float64(peak)/32768))
}
