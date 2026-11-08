// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

// Command inputprobe is the M0 T0.5 check: it disables RC003's voice key
// (F5) for the duration of a run and restores it afterwards, simulates Fn
// taps to start and stop Typeless, and prints the key events macOS delivers
// to show both work. Posting and watching events need the Accessibility and
// Input Monitoring permissions, so run it as InputProbe.app (make
// inputprobe).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zJiaJun/yaozhi/internal/macinput"
)

// RC003 HID identity (open-voice-bridge VoiceBridgeDeviceProfile.swift).
const (
	rc003Vendor  = 0x2717
	rc003Product = 0x32B8
)

func main() {
	show := flag.Bool("show", false, "显示 RC003 的 HID 服务和当前按键映射")
	watch := flag.Duration("watch", 0, "这么长时间内打印系统收到的 F5 和 Fn 事件，其他按键只计数（需要输入监控权限）")
	block := flag.Bool("block-f5", false, "运行期间屏蔽 RC003 的 F5，退出时恢复")
	tap := flag.Duration("tap-fn", 0, "2 秒后模拟一次 Fn 点按，等这么久，再点按一次（让 Typeless 开始、结束；需要辅助功能权限）")
	flag.Parse()
	if err := run(*show, *watch, *block, *tap); err != nil {
		fmt.Fprintln(os.Stderr, "inputprobe:", err)
		os.Exit(1)
	}
}

func run(show bool, watch time.Duration, block bool, tap time.Duration) error {
	if show {
		return showMappings()
	}
	if watch == 0 && !block && tap == 0 {
		flag.Usage()
		os.Exit(2)
	}
	// Ask for both permissions up front, so one round in System Settings
	// covers whatever is missing.
	var missing []string
	if tap > 0 {
		ok, err := macinput.RequestPostEvents()
		if err != nil {
			return err
		}
		if !ok {
			missing = append(missing, "辅助功能")
		}
	}
	if watch > 0 {
		ok, err := macinput.RequestListenEvents()
		if err != nil {
			return err
		}
		if !ok {
			missing = append(missing, "输入监控")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("还缺权限：到 系统设置 → 隐私与安全性 → %s 里打开 InputProbe，然后重新运行", strings.Join(missing, "、"))
	}

	restore := func() error { return nil }
	if block {
		n, r, err := macinput.RemapKey(rc003Vendor, rc003Product, macinput.UsageF5, macinput.UsageNone)
		if errors.Is(err, macinput.ErrNoService) {
			return errors.New("RC003 没有连接（找不到它的 HID 服务）")
		}
		if err != nil {
			return err
		}
		restore = r
		fmt.Printf("已在 %d 个服务上屏蔽 RC003 的 F5\n", n)
	}
	cleanup := func() {
		_ = macinput.ReleaseFnIfPressed()
		if err := restore(); err != nil {
			fmt.Fprintln(os.Stderr, "inputprobe: 恢复 F5 失败：", err)
		} else if block {
			fmt.Println("已恢复 RC003 的 F5")
		}
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		cleanup()
		os.Exit(130)
	}()
	defer cleanup()

	tapDone := make(chan error, 1)
	if tap > 0 {
		go func() { tapDone <- tapTwice(tap) }()
	} else {
		tapDone <- nil
	}

	if watch > 0 {
		fmt.Printf("监听按键 %v，Ctrl-C 提前结束\n", watch)
		start := time.Now()
		others := 0
		if err := macinput.WatchKeys(watch, func(e macinput.KeyEvent) {
			// Only the keys under test are printed; anything else the user
			// types stays out of the output.
			if e.KeyCode != macinput.KeyCodeF5 && e.KeyCode != macinput.KeyCodeFunction {
				others++
				return
			}
			fmt.Printf("[%7.3f] %s\n", time.Since(start).Seconds(), describe(e))
		}); err != nil {
			return err
		}
		fmt.Printf("监听结束：另有 %d 个其他按键事件，未打印\n", others)
	}
	return <-tapDone
}

func describe(e macinput.KeyEvent) string {
	name := ""
	switch e.KeyCode {
	case macinput.KeyCodeF5:
		name = " (F5)"
	case macinput.KeyCodeFunction:
		name = " (Fn)"
		if e.Flags&macinput.FlagSecondaryFn != 0 {
			name = " (Fn 按下)"
		} else {
			name = " (Fn 松开)"
		}
	}
	src := "实体按键"
	if e.Synthetic {
		src = "遥指模拟"
	}
	return fmt.Sprintf("%-13s keycode=%-3d%s flags=%#x  %s", e.Type, e.KeyCode, name, e.Flags, src)
}

func showMappings() error {
	svcs, err := macinput.ServicesFor(rc003Vendor, rc003Product)
	if err != nil {
		return err
	}
	defer svcs.Close()
	if len(svcs.Items) == 0 {
		return errors.New("RC003 没有连接（找不到它的 HID 服务）")
	}
	for _, s := range svcs.Items {
		fmt.Printf("服务 %#x 映射：%s\n", s.RegistryID, formatMappings(s.KeyMappings()))
	}
	return nil
}

func formatMappings(ms []macinput.KeyMapping) string {
	if len(ms) == 0 {
		return "（无）"
	}
	out := ""
	for _, m := range ms {
		out += fmt.Sprintf("%#x→%#x ", uint64(m.Src), uint64(m.Dst))
	}
	return out
}

func tapTwice(wait time.Duration) error {
	time.Sleep(2 * time.Second)
	fmt.Println("→ Fn 点按（开始）")
	if err := macinput.TapFn(macinput.DefaultTapDuration); err != nil {
		return err
	}
	fmt.Printf("等待 %v……\n", wait)
	time.Sleep(wait)
	fmt.Println("→ Fn 点按（结束）")
	return macinput.TapFn(macinput.DefaultTapDuration)
}
