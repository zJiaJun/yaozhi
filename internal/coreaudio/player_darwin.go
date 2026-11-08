// SPDX-License-Identifier: GPL-3.0-only

package coreaudio

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/zJiaJun/yaozhi/internal/cf"
)

// The Audio Queue setup follows MyGo's audio/output_darwin.go (MIT): one
// purego callback for every queue, routed by the queue's user data.

type audioStreamBasicDescription struct {
	sampleRate       float64
	formatID         uint32
	formatFlags      uint32
	bytesPerPacket   uint32
	framesPerPacket  uint32
	bytesPerFrame    uint32
	channelsPerFrame uint32
	bitsPerChannel   uint32
	reserved         uint32
}

type audioQueueBuffer struct {
	audioDataBytesCapacity uint32
	audioData              unsafe.Pointer
	audioDataByteSize      uint32
	userData               uintptr // end position (in input samples) of what it holds
	packetDescCapacity     uint32
	packetDescs            uintptr
	packetDescCount        uint32
}

const (
	formatLinearPCM    = 0x6C70636D // 'lpcm'
	formatFlagsInt16   = 0x4 | 0x8  // kAudioFormatFlagIsSignedInteger | kAudioFormatFlagIsPacked
	propCurrentDevice  = 0x61716364 // 'aqcd' kAudioQueueProperty_CurrentDevice
	outputChannels     = 2
	bytesPerFrame      = 2 * outputChannels
	defaultBufferCount = 3
	defaultBufferMs    = 20
)

var (
	aqNewOutput   func(format *audioStreamBasicDescription, callback, user, runLoop, runLoopMode uintptr, flags uint32, out *uintptr) int32
	aqSetProperty func(q uintptr, id uint32, data unsafe.Pointer, size uint32) int32
	aqAllocate    func(q uintptr, size uint32, out **audioQueueBuffer) int32
	aqEnqueue     func(q uintptr, b *audioQueueBuffer, n uint32, descs uintptr) int32
	aqStart       func(q uintptr, at uintptr) int32
	aqStop        func(q uintptr, immediate bool) int32
	aqDispose     func(q uintptr, immediate bool) int32

	aqCallback uintptr
	players    sync.Map // uintptr id -> *Player
	nextPlayer atomic.Uintptr
)

func loadQueue() error {
	lib := audioToolboxLib
	purego.RegisterLibFunc(&aqNewOutput, lib, "AudioQueueNewOutput")
	purego.RegisterLibFunc(&aqSetProperty, lib, "AudioQueueSetProperty")
	purego.RegisterLibFunc(&aqAllocate, lib, "AudioQueueAllocateBuffer")
	purego.RegisterLibFunc(&aqEnqueue, lib, "AudioQueueEnqueueBuffer")
	purego.RegisterLibFunc(&aqStart, lib, "AudioQueueStart")
	purego.RegisterLibFunc(&aqStop, lib, "AudioQueueStop")
	purego.RegisterLibFunc(&aqDispose, lib, "AudioQueueDispose")
	// Called on the queue's own thread when it is done with a buffer.
	aqCallback = purego.NewCallback(func(user, q uintptr, b *audioQueueBuffer) {
		if v, ok := players.Load(user); ok {
			v.(*Player).refill(b)
		}
	})
	return nil
}

// Player plays mono 16-bit PCM on one output device through an Audio Queue.
// The same samples go to the left and right channels, and the queue converts
// the rate to whatever the device runs at, without changing the device.
//
// A new Player is stopped: Start it before audio is due and Stop it when
// there is none for a while, so that it costs nothing while idle. While
// started it keeps playing, filling gaps with silence, so its latency stays
// constant. Methods are safe for concurrent use.
type Player struct {
	id        uintptr
	q         uintptr
	rate      int
	frames    int // per buffer
	bufferDur time.Duration
	buffers   []*audioQueueBuffer

	runMu sync.Mutex // serialises Start and Stop

	mu      sync.Mutex
	cond    *sync.Cond
	fifo    []int16 // written, not yet handed to the queue
	written int64   // samples written in total
	queued  int64   // samples handed to the queue
	played  int64   // samples in buffers the queue has finished with
	running bool
	inQueue map[*audioQueueBuffer]bool // buffers the queue holds
	closed  bool
	err     error
}

// NewPlayer opens a player on the device with the given UID ("" for the
// system default output) for mono input at rate Hz.
func NewPlayer(deviceUID string, rate int) (*Player, error) {
	if err := load(); err != nil {
		return nil, err
	}
	p := &Player{
		id:        nextPlayer.Add(1),
		rate:      rate,
		frames:    rate * defaultBufferMs / 1000,
		bufferDur: defaultBufferMs * time.Millisecond,
	}
	p.cond = sync.NewCond(&p.mu)
	p.inQueue = map[*audioQueueBuffer]bool{}
	desc := audioStreamBasicDescription{
		sampleRate:       float64(rate),
		formatID:         formatLinearPCM,
		formatFlags:      formatFlagsInt16,
		bytesPerPacket:   bytesPerFrame,
		framesPerPacket:  1,
		bytesPerFrame:    bytesPerFrame,
		channelsPerFrame: outputChannels,
		bitsPerChannel:   16,
	}
	if st := aqNewOutput(&desc, aqCallback, p.id, 0, 0, 0, &p.q); st != 0 {
		return nil, fmt.Errorf("coreaudio: AudioQueueNewOutput: OSStatus %d", st)
	}
	fail := func(format string, args ...any) (*Player, error) {
		aqDispose(p.q, true)
		return nil, fmt.Errorf(format, args...)
	}
	if deviceUID != "" {
		uid := cf.String(deviceUID)
		st := aqSetProperty(p.q, propCurrentDevice, unsafe.Pointer(&uid), uint32(unsafe.Sizeof(uid)))
		cf.Release(uid)
		if st != 0 {
			return fail("coreaudio: select device %s: OSStatus %d", deviceUID, st)
		}
	}
	players.Store(p.id, p)
	for range defaultBufferCount {
		var b *audioQueueBuffer
		if st := aqAllocate(p.q, uint32(p.frames*bytesPerFrame), &b); st != 0 {
			players.Delete(p.id)
			return fail("coreaudio: AudioQueueAllocateBuffer: OSStatus %d", st)
		}
		p.buffers = append(p.buffers, b)
	}
	return p, nil
}

// Start starts playing: what was written so far first, then silence until
// more comes. Starting a started player does nothing.
func (p *Player) Start() error {
	p.runMu.Lock()
	defer p.runMu.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("coreaudio: player closed")
	}
	if p.running {
		p.mu.Unlock()
		return nil
	}
	// Prime the buffers the queue does not hold; no callback runs while the
	// queue is stopped, so this cannot race with refill.
	var prime []*audioQueueBuffer
	for _, b := range p.buffers {
		if !p.inQueue[b] {
			p.fill(b)
			p.inQueue[b] = true
			prime = append(prime, b)
		}
	}
	p.running = true
	p.mu.Unlock()
	for _, b := range prime {
		if st := aqEnqueue(p.q, b, 0, 0); st != 0 {
			p.mu.Lock()
			p.inQueue[b] = false
			p.mu.Unlock()
			return p.stopWith(fmt.Errorf("coreaudio: AudioQueueEnqueueBuffer: OSStatus %d", st))
		}
	}
	if st := aqStart(p.q, 0); st != 0 {
		return p.stopWith(fmt.Errorf("coreaudio: AudioQueueStart: OSStatus %d", st))
	}
	return nil
}

// stopWith marks the player stopped after a failed Start. p.runMu is held.
func (p *Player) stopWith(err error) error {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	return err
}

// Stop stops playing at once. Anything written but not yet played is
// dropped and counts as played, so WaitPlayed does not wait for it. Stopping
// a stopped player does nothing.
func (p *Player) Stop() {
	p.runMu.Lock()
	defer p.runMu.Unlock()
	p.mu.Lock()
	if !p.running || p.closed {
		p.mu.Unlock()
		return
	}
	p.running = false
	p.mu.Unlock()
	aqStop(p.q, true)
	p.mu.Lock()
	p.fifo = nil
	p.queued, p.played = p.written, p.written
	p.cond.Broadcast()
	p.mu.Unlock()
}

// Running reports whether the player is started.
func (p *Player) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// Latency is roughly how long a sample written now waits before the queue
// is done with it: the buffers already queued ahead of it.
func (p *Player) Latency() time.Duration { return defaultBufferCount * p.bufferDur }

// Write queues samples and returns the position just after them, for
// WaitPlayed. It never blocks on the device.
func (p *Player) Write(mono []int16) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.fifo = append(p.fifo, mono...)
		p.written += int64(len(mono))
	}
	return p.written
}

// Written returns how many samples have been written.
func (p *Player) Written() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written
}

// Played returns how many samples the queue has finished playing.
func (p *Player) Played() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.played
}

// WaitPlayed waits until every sample before pos has been played, and
// reports whether that happened within timeout. "Played" means the queue
// handed the buffer on; the device may add a few milliseconds more.
func (p *Player) WaitPlayed(pos int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	t := time.AfterFunc(timeout, func() {
		p.mu.Lock()
		p.cond.Broadcast()
		p.mu.Unlock()
	})
	defer t.Stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.played < pos && !p.closed && time.Now().Before(deadline) {
		p.cond.Wait()
	}
	return p.played >= pos
}

// Err returns the first error the queue thread hit, if any.
func (p *Player) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Close stops playback at once, dropping anything not yet played.
func (p *Player) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.cond.Broadcast()
	p.mu.Unlock()
	aqStop(p.q, true)
	aqDispose(p.q, true)
	players.Delete(p.id)
}

func (p *Player) refill(b *audioQueueBuffer) {
	p.mu.Lock()
	p.inQueue[b] = false
	if p.closed || !p.running {
		// Stop flushes the queue: keep the buffer for the next Start.
		p.mu.Unlock()
		return
	}
	if pos := int64(b.userData); pos > p.played {
		p.played = pos
		p.cond.Broadcast()
	}
	p.fill(b)
	p.inQueue[b] = true
	p.mu.Unlock()
	if st := aqEnqueue(p.q, b, 0, 0); st != 0 {
		p.mu.Lock()
		p.inQueue[b] = false
		if p.err == nil {
			p.err = fmt.Errorf("coreaudio: AudioQueueEnqueueBuffer: OSStatus %d", st)
		}
		p.mu.Unlock()
	}
}

// fill copies up to one buffer of pending samples into b, as stereo, and
// pads the rest with silence. p.mu must be held.
func (p *Player) fill(b *audioQueueBuffer) {
	out := unsafe.Slice((*int16)(b.audioData), p.frames*outputChannels)
	n := duplicateToStereo(out, p.fifo)
	clear(out[2*n:])
	p.fifo = p.fifo[n:]
	if len(p.fifo) == 0 {
		p.fifo = p.fifo[:0:0] // let the backing array go
	}
	p.queued += int64(n)
	b.audioDataByteSize = uint32(p.frames * bytesPerFrame)
	b.userData = uintptr(p.queued)
}

// duplicateToStereo writes src as interleaved L/R pairs into dst and returns
// how many source samples fit.
func duplicateToStereo(dst, src []int16) int {
	n := min(len(src), len(dst)/2)
	for i, s := range src[:n] {
		dst[2*i] = s
		dst[2*i+1] = s
	}
	return n
}
