// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

// Command audioprobe is the M0 T0.4 check: it lists audio devices, finds
// BlackHole 2ch by UID and plays a 16 kHz mono WAV (or a test tone) into
// it. Record from BlackHole 2ch in QuickTime to hear the result. Audio
// output needs no privacy permission, so it runs as a plain binary.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/zJiaJun/yaozhi/internal/coreaudio"
)

// blackHoleUID is the UID of the unmodified BlackHole 2ch driver.
const blackHoleUID = "BlackHole2ch_UID"

const rate = 16000

func main() {
	list := flag.Bool("list", false, "列出所有音频设备")
	wav := flag.String("wav", "", "播放这个 16 kHz 单声道 16 位 WAV")
	tone := flag.Duration("tone", 0, "播放这么长的 440 Hz 测试音（-12 dBFS）")
	uid := flag.String("device", blackHoleUID, "输出设备 UID")
	flag.Parse()
	if err := run(*list, *wav, *tone, *uid); err != nil {
		fmt.Fprintln(os.Stderr, "audioprobe:", err)
		os.Exit(1)
	}
}

func run(list bool, wavPath string, tone time.Duration, uid string) error {
	if list {
		devs, err := coreaudio.Devices()
		if err != nil {
			return err
		}
		for _, d := range devs {
			fmt.Printf("%-28q uid=%-40q %6.0f Hz  输出 %d 声道  输入 %d 声道",
				d.Name, d.UID, d.SampleRate, d.OutputChannels, d.InputChannels)
			if d.Aggregate {
				fmt.Printf("  聚集设备：%q", d.SubDevices)
			}
			fmt.Println()
		}
		return nil
	}

	dev, err := coreaudio.DeviceByUID(uid)
	if errors.Is(err, coreaudio.ErrNotFound) && uid == blackHoleUID {
		return errors.New("没找到 BlackHole 2ch，请先安装原版：brew install blackhole-2ch")
	}
	if err != nil {
		return err
	}
	fmt.Printf("输出设备：%s（%s），%.0f Hz，%d 声道\n", dev.Name, dev.UID, dev.SampleRate, dev.OutputChannels)

	var samples []int16
	switch {
	case wavPath != "":
		if samples, err = readWAV(wavPath); err != nil {
			return err
		}
	case tone > 0:
		samples = sine(440, tone, 0.25)
	default:
		return errors.New("用 -wav 或 -tone 指定要播放的内容，或用 -list 列出设备")
	}

	p, err := coreaudio.NewPlayer(dev.UID, rate)
	if err != nil {
		return err
	}
	defer p.Close()
	start := time.Now()
	end := p.Write(samples)
	if err := p.Start(); err != nil {
		return err
	}
	dur := time.Duration(len(samples)) * time.Second / rate
	if !p.WaitPlayed(end, dur+2*time.Second) {
		return fmt.Errorf("播放超时：已播 %d / %d 个采样", p.Played(), end)
	}
	if err := p.Err(); err != nil {
		return err
	}
	fmt.Printf("播放完成：音频 %v，实际用时 %v（队列延迟约 %v）\n",
		dur.Round(time.Millisecond), time.Since(start).Round(time.Millisecond), p.Latency())
	return nil
}

func sine(freq float64, d time.Duration, amp float64) []int16 {
	n := int(d.Seconds() * rate)
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(amp * 32767 * math.Sin(2*math.Pi*freq*float64(i)/rate))
	}
	return out
}

// readWAV reads a 16-bit mono WAV at 16 kHz.
func readWAV(path string) ([]int16, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, fmt.Errorf("%s 不是 WAV 文件", path)
	}
	le := binary.LittleEndian
	var fmtOK bool
	for off := 12; off+8 <= len(b); {
		id, size := string(b[off:off+4]), int(le.Uint32(b[off+4:]))
		body := b[off+8 : min(off+8+size, len(b))]
		switch id {
		case "fmt ":
			if len(body) < 16 || le.Uint16(body[0:]) != 1 || le.Uint16(body[2:]) != 1 ||
				le.Uint32(body[4:]) != rate || le.Uint16(body[14:]) != 16 {
				return nil, fmt.Errorf("%s 不是 16 kHz 单声道 16 位 PCM", path)
			}
			fmtOK = true
		case "data":
			if !fmtOK {
				return nil, fmt.Errorf("%s 缺少 fmt 块", path)
			}
			out := make([]int16, len(body)/2)
			for i := range out {
				out[i] = int16(le.Uint16(body[2*i:]))
			}
			return out, nil
		}
		off += 8 + size + size%2
	}
	return nil, fmt.Errorf("%s 没有 data 块", path)
}
