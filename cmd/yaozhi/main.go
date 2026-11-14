// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

// Command yaozhi is the Yaozhi app: a menu bar icon with its menu, and a main
// window, in MyGo's native UI. The engine (internal/app) does the work; this
// command only shows its state. The look, the five-step setup and the
// readiness list follow docs/design/index.html ("墨与朱").
//
// Build and run it as an app bundle (make yaozhi, then scripts/yaozhi.sh):
// macOS kills a process that touches Bluetooth without the usage string the
// bundle's Info.plist carries.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/ui"

	"github.com/zJiaJun/yaozhi/internal/app"
	"github.com/zJiaJun/yaozhi/internal/device"
	"github.com/zJiaJun/yaozhi/internal/icon"
	"github.com/zJiaJun/yaozhi/internal/voice"
)

var start = time.Now()

// recent keeps the latest log lines for a diagnostics report. Like the
// log itself, they never hold audio or text.
var recent struct {
	sync.Mutex
	lines []string
}

func logf(format string, args ...any) {
	line := fmt.Sprintf("[%7.3f] %s", time.Since(start).Seconds(), fmt.Sprintf(format, args...))
	fmt.Println(line)
	recent.Lock()
	recent.lines = append(recent.lines, line)
	if n := len(recent.lines) - 300; n > 0 {
		recent.lines = recent.lines[n:]
	}
	recent.Unlock()
}

// supportDir is where user files live.
func supportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Yaozhi")
}

func main() {
	presetID := flag.String("preset", "typeless", "输入法预设")
	anyRemote := flag.Bool("any", false, "也接受没有设备描述的 ATVV 遥控器（试用新设备）")
	pretend := flag.String("pretend", "", "假装缺少一些东西，用来看首次启动的引导：all，或用逗号隔开的 bluetooth、remote、blackhole、typeless、typeless-off、ax、text")
	flag.Parse()

	// One Yaozhi at a time. Another of this same build takes this launch
	// for a click on its icon and shows its window (OnSecondInstance
	// below); the other build, release or development, is told apart by
	// the lock both share, and this one stops before it touches anything.
	mygo.App.SetName("Yaozhi")
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	if dir := supportDir(); dir != "" {
		unlock, other, err := app.LockInstance(filepath.Join(dir, "running.lock"), holder())
		switch {
		case errors.Is(err, app.ErrRunning):
			alreadyRunning(other)
			return
		case err != nil:
			logf("没能检查是否已有遥指在运行：%v", err)
		default:
			defer unlock() // keeps the lock's file open until Yaozhi quits
		}
	}

	descs, err := device.Catalog(filepath.Join(supportDir(), "devices"))
	if err != nil {
		logf("跳过了有问题的设备描述：%v", err)
	}
	presets, err := voice.Presets(filepath.Join(supportDir(), "presets"))
	if err != nil {
		logf("跳过了有问题的输入法预设：%v", err)
	}
	i := slices.IndexFunc(presets, func(p voice.Preset) bool { return p.ID == *presetID })
	if i < 0 {
		log.Fatalf("没有输入法预设 %q", *presetID)
	}

	eng := app.New(app.Options{
		Devices: descs, Preset: presets[i], AcceptAny: *anyRemote, Logf: logf,
		StatsFile:    filepath.Join(supportDir(), "stats.json"),
		SettingsFile: filepath.Join(supportDir(), "settings.json"),
	})
	s := &shell{eng: eng, preset: presets[i], launched: time.Now(), page: pageOverview, selKey: "voice", pretend: parsePretend(*pretend)}
	eng.OnChange(s.changed)

	mygo.App.OnSecondInstance(func([]string, string) {
		if s.win != nil {
			s.showWindow()
		}
	})
	mygo.App.OnWindowAllClosed(func() {}) // lives on in the menu bar
	mygo.App.WhenReady(s.ready)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { s.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { s.stop() })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// holder is what this Yaozhi writes into the instance lock for another
// to tell the user: where its app bundle is, and its process.
func holder() string {
	where := "遥指"
	if exe, err := os.Executable(); err == nil {
		if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
			where = exe[:i+len(".app")]
		} else {
			where = exe
		}
	}
	return fmt.Sprintf("%s（进程 %d）", where, os.Getpid())
}

// alreadyRunning tells the user that another Yaozhi runs, and quits. It
// starts neither the engine nor the menu bar icon: this one never takes
// the remote.
func alreadyRunning(other string) {
	if other == "" {
		other = "另一个遥指"
	}
	logf("已有遥指在运行：%s", other)
	mygo.App.WhenReady(func() {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
		go func() {
			_, _ = mygo.Dialog.Message(mygo.MessageOptions{
				Type:    mygo.MessageWarning,
				Message: "遥指已经在运行",
				Detail: other + " 正在运行。两个遥指同时运行会争用遥控器，按住说话会失效。\n\n" +
					"请先在菜单栏退出它，再打开这一个。",
			})
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// shell is the UI. Its fields are used on the main thread only, except
// changed, which the engine calls from its goroutines.
type shell struct {
	eng      *app.Engine
	preset   voice.Preset
	st       app.State
	win      *mygo.Window
	tray     *mygo.Tray
	trayLook string
	trayMenu string // what the menu bar menu says, to set it again only when that changes
	quitting bool
	guiding  bool
	launched time.Time

	page      string // the main window's page
	selKey    string // the key picked out on the "按键" page
	readyOpen bool   // the readiness list, opened from its chip

	flash      string // a moment's news on the status bar
	flashUntil time.Time

	try tryBox // the overview's box to try voice input in

	// finished: the last step of the first launch was done in this run,
	// and the overview still shows the steps, all ticked, until the user
	// moves on.
	finished bool

	pretend map[string]bool // what is shown as missing, see pretend.go

	// The overview's level bars while listening: the latest levels, the
	// newest first.
	wave   [30]float32
	waveAt time.Time
	level  float32

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *shell) ready() {
	logf("遥指 %s", versionLine())
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		if err := s.eng.Run(s.ctx); err != nil {
			logf("引擎退出：%v", err)
		}
	}()

	var err error
	s.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(icon.GlyphIdle, "", false), IconIsTemplate: true, ToolTip: "遥指"})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	s.tray.OnAppearanceChange(func() { s.trayLook = "?"; s.updateTray() })

	s.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "遥指",
		Width:         1040,
		Height:        660,
		MinWidth:      900,
		MinHeight:     580,
		TitleBarStyle: mygo.TitleBarHiddenInset,
		Hidden:        true,
		Content:       ui.View(s.mainView),
	})
	s.win.OnClose(func(e *mygo.CloseEvent) {
		// Closing the window keeps Yaozhi in the menu bar; Quit closes it.
		if !s.quitting {
			e.PreventDefault()
			s.hideWindow()
		}
	})
	s.st = s.pretended(s.eng.State())
	s.updateTray()
	s.showWindow()
}

// changed is called by the engine from any goroutine.
func (s *shell) changed() {
	if s.win != nil {
		s.win.Update(func() { s.setState(s.eng.State()) })
	}
}

// setState takes a new state, saying on the status bar what just became
// fine.
func (s *shell) setState(st app.State) {
	st = s.pretended(st)
	old := s.st
	s.st = st
	s.updateTray()
	if old == st || time.Since(s.launched) < 3*time.Second {
		return // the first checks, not news
	}
	s.try.follow(s, old, st)
	o, n := old.Ready, st.Ready
	switch {
	case n.Accessibility && !o.Accessibility:
		s.say("辅助功能已允许，可以说话了")
	case n.BlackHole && n.Mic && !(o.BlackHole && o.Mic):
		s.say(app.MicName + " 已就绪")
	case st.Conn == device.StateReady && old.Conn != device.StateReady && st.Voice == app.VoiceIdle:
		s.say("遥控器已连接")
	}
	if n.Ready() && !o.Ready() {
		s.readyOpen = false
	}
	if st.TextSeen && !old.TextSeen {
		s.finished = true
	}
}

func (s *shell) stop() {
	s.cancel()
	select {
	case <-s.done: // the engine restored the voice key and released Fn
	case <-time.After(2 * time.Second):
		logf("退出时引擎没有及时停下")
	}
	if s.tray != nil {
		s.tray.Destroy()
	}
}

func (s *shell) showWindow() {
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	s.win.Show()
	s.win.Focus()
}

func (s *shell) hideWindow() {
	s.win.Hide()
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
}

// guide opens System Settings at the permission, with MyGo's panel to drag
// Yaozhi into the list, and checks again once the user is done.
func (s *shell) guide(p permissionflow.Permission) {
	if s.guiding {
		return
	}
	s.guiding = true
	go func() {
		st, err := permissionflow.Guide(s.ctx, p, permissionflow.GuideOptions{})
		logf("授权引导（%s）结束：%v %v", p.Title(), st, err)
		s.eng.Recheck()
		s.win.Update(func() { s.guiding = false })
	}()
}

// exportDiagnostics writes what Yaozhi sees and its recent log to a text
// file, and shows it in the Finder.
func (s *shell) exportDiagnostics() {
	go func() {
		var b strings.Builder
		fmt.Fprintf(&b, "遥指 Yaozhi %s\n%s\n", versionLine(), time.Now().Format("2006-01-02 15:04:05 -0700"))
		if v, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			fmt.Fprintf(&b, "macOS %s", v)
		}
		b.WriteString("\n" + s.eng.Diagnostics() + "\n最近的日志：\n")
		recent.Lock()
		b.WriteString(strings.Join(recent.lines, "\n"))
		recent.Unlock()
		b.WriteString("\n")

		dir := filepath.Join(supportDir(), "diagnostics")
		path := filepath.Join(dir, "yaozhi-"+time.Now().Format("20060102-150405")+".txt")
		err := os.MkdirAll(dir, 0o755)
		if err == nil {
			err = os.WriteFile(path, []byte(b.String()), 0o644)
		}
		msg := "诊断已导出，不含语音和文字"
		if err != nil {
			logf("导出诊断失败：%v", err)
			msg = "导出诊断失败"
		} else {
			mygo.Shell.ShowItemInFolder(path)
		}
		s.win.Update(func() { s.say(msg) })
	}()
}

// ---------------------------------------------------------------- menu bar

func (s *shell) updateTray() {
	if s.tray == nil {
		return
	}
	x := s.status()
	state, badge := icon.GlyphIdle, ""
	switch {
	case x.tone == toneVoice:
		state = icon.GlyphLive
	case x.tone == toneBad:
		badge = "bad"
	case x.tone == toneWarn:
		badge = "warn"
	case s.st.Paused:
		state = icon.GlyphPaused
	}
	if look := fmt.Sprint(state, badge); look != s.trayLook {
		s.trayLook = look
		if badge == "" {
			_ = s.tray.SetIcon(trayIcon(state, "", false), true)
		} else {
			_ = s.tray.SetIcon(trayIcon(state, badge, s.tray.Dark()), false)
		}
	}
	// The menu is set anew whenever what it says changes.
	items := s.menu()
	var said strings.Builder
	for _, it := range items {
		said.WriteString(it.Label + "\n")
	}
	if said.String() != s.trayMenu {
		s.trayMenu = said.String()
		s.tray.SetMenu(mygo.NewMenu(items))
	}
}

// menu is the menu bar icon's menu, the system's own: who is connected,
// greyed out as something to read, then the few things to do from here.
func (s *shell) menu() []*mygo.MenuItem {
	x := s.status()
	st := s.st
	info := func(label string) *mygo.MenuItem { return &mygo.MenuItem{Label: label, Disabled: true} }
	do := func(label string, fn func()) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Click: func(*mygo.MenuItem, *mygo.Window) { fn() }}
	}

	items := []*mygo.MenuItem{info(s.deviceLine()), mygo.Separator()}
	if x.action != "" && x.do != nil && !x.resume { // resuming is the next item
		items = append(items, do(x.action+"…", x.do))
	}
	if st.Paused {
		items = append(items, do("启用语音输入", func() { s.eng.SetPaused(false) }))
	} else {
		items = append(items, do("暂停语音输入", func() { s.eng.SetPaused(true) }))
	}
	return append(items,
		do("打开遥指…", s.showWindow),
		do("关于遥指", showAbout),
		mygo.Separator(),
		do("退出遥指", mygo.App.Quit),
	)
}

// deviceLine is the remote in a line: its name, how it is connected, and
// its charge.
func (s *shell) deviceLine() string {
	st := s.st
	name := st.Device
	if name == "" {
		name = "遥控器"
	}
	switch st.Conn {
	case device.StateReady:
		if st.BatteryKnown {
			return fmt.Sprintf("%s · 电量 %d%%", name, st.Battery)
		}
		return name + " · 已连接"
	case device.StateConnecting:
		return name + " · 连接中"
	case device.StateUnavailable:
		return "蓝牙已关闭"
	}
	return name + " · 未连接"
}
