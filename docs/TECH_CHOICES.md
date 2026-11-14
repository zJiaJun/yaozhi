# 技术选型

这里记录遥指的关键技术决定：选了什么、为什么、放弃了什么，以及支撑这些决定的实测数据。M0 结束时（2026-11-07）写成，之后有新决定时更新，最近一次更新是 2026-11-14。

## 架构

```
┌──────────────────────────── cmd/yaozhi（MyGo 界面）────────────────────────────┐
│  菜单栏 · 主窗口 · 首次引导          只读引擎状态，调用引擎的少数几个操作        │
└──────────────────────────────────────┬─────────────────────────────────────────┘
                                       │ State / OnChange
┌──────────────────────────── internal/app（引擎，不依赖 MyGo）───────────────────┐
│  设备生命周期 · 语音会话 · 屏蔽语音键 · 就绪检查 · 设置和统计                   │
└───────────────┬──────────────────────────────────────────────┬─────────────────┘
                │ 统一事件                                      │ 开始 / 写入 / 结束
┌───────────────▼──────────────┐                 ┌─────────────▼──────────────────┐
│ 设备层 internal/device        │                 │ 输入法层 internal/voice         │
│  atvvble：蓝牙 + ATVV 适配器  │                 │  会话逻辑 + 虚拟麦克风 + 快捷键  │
│  devices/*.toml：设备描述     │                 │  presets/*.toml：输入法预设      │
└───────────────┬──────────────┘                 └─────────────┬──────────────────┘
                │                                              │
   corebluetooth · atvv                         coreaudio · macinput · macapp
                └────────────── purego（不用 cgo）──────────────┘
```

设备适配器只产出统一事件（连接状态、设备信息、按键、语音开始、音频帧、语音结束），输入法层只消费"开始、写入、结束"。两边的差异都写在 TOML 数据文件里，所以接入同协议的新遥控器或同类的新输入法不用改代码。

## 决定一览

| 环节 | 选择 | 放弃的方案 |
|---|---|---|
| 语言和界面 | Go + MyGo 原生界面 | MyGo 网页界面、Swift |
| 调系统接口 | purego，不用 cgo | cgo、tinygo bluetooth |
| 蓝牙 | 自己绑定 CoreBluetooth | 扫描广播 |
| 语音协议 | ATVV + IMA ADPCM，纯 Go 实现 | — |
| 数据文件格式 | TOML | JSON、YAML |
| 虚拟麦克风 | 用户自装的原版 BlackHole 2ch | 打包或修改 BlackHole |
| 给输入法选的麦克风 | 遥指自动创建的聚集设备 `yaozhi-mic` | 让用户手动创建、改 BlackHole 的名字 |
| 控制输入法 | 模拟 Fn 点按，屏蔽遥控器的 F5 | 让输入法直接绑 F5 |
| 输入法抽象 | 一个通用实现 + TOML 预设 | 每个输入法一个插件 |
| 辅助功能检测 | 每 2 秒问 `AXIsProcessTrusted` | 预检接口、子进程复查、监听 TCC 通知 |
| 菜单栏 | 系统菜单 | 自绘弹出面板 |
| 签名 | 开发用本机自签名证书 `yaozhi-dev` 和单独的 bundle ID，发布用自签名证书 `Yaozhi Release` | 临时签名；暂不加入 Apple 开发者计划 |
| 多开 | 同一时间只运行一个：同版本用 MyGo 的单实例锁，开发版和发布版之间用共享的文件锁 | 不限制 |

## 决定和原因

### Go + MyGo 原生界面

单个二进制约 10 MB，不带网页内核；MyGo 自带菜单栏、权限引导（`permissionflow`）、动画和无窗口渲染测试。MyGo 的网页界面要 Bun 和前端工具链；Swift 和"用 Go 写"的目标不符。

引擎（`internal/app`）不依赖 MyGo，界面只读它的状态，MyGo 的 API 变化影响面只在 `cmd/yaozhi`。

### purego，不用 cgo

和 MyGo 一致，`CGO_ENABLED=0` 就能编译，不依赖本机 SDK 和链接器版本（开发机上 ld 26.6 就读不了 MacOSX27 SDK）。CoreBluetooth、Core Audio、CGEvent、IOHIDEventSystemClient、AppKit 都通过 purego 调用。

注意：通过 purego 调 Objective-C 时，自动释放池必须在同一个系统线程上创建和释放，调用期间要 `runtime.LockOSThread()`，否则协程换线程后会段错误。

### 自己绑定 CoreBluetooth

配对后的 RC003 由系统保持连接、不再广播，必须用 `retrieveConnectedPeripheralsWithServices` 才找得到。tinygo bluetooth 走 cgo，也没有暴露这个接口。

### TOML 描述设备和输入法

能写注释，原生支持 `0x2717` 这类十六进制数。JSON 不能写注释、不支持十六进制；YAML 的缩进和隐式类型容易出错。内置文件用 `go:embed` 编进程序，用户文件放在 `~/Library/Application Support/Yaozhi/` 下，`id` 相同时替换内置的。

### 原版 BlackHole 2ch + 聚集设备 yaozhi-mic

遥指不打包、不修改 BlackHole，只按 UID `BlackHole2ch_UID` 检测，用 AudioQueue 写入（系统把 16 kHz 重采样到设备的采样率，不改 BlackHole 自身的采样率）。

Typeless 会把名字带 "(Virtual)" 的输入设备从麦克风列表里去掉，看不到 BlackHole。解决办法是一个只含 BlackHole 2ch 的聚集设备（后缀是 "(Aggregate)"），Typeless 选它，遥指照旧按 UID 写入 BlackHole。

聚集设备由遥指用 `AudioHardwareCreateAggregateDevice` 自动创建：固定 UID、公开可见；每次启动检查，缺了就重建，所以不依赖系统重启后是否保留；已有同名且只含 BlackHole 的设备（用户手动建的）直接用。不提供移除入口，要删可以在"音频 MIDI 设置"里手动删。

放弃的方案：remote-mic-app 用改名的 BlackHole（MiRemoteV 2ch），需要分发驱动；改 BlackHole 的名字或 UID 违反"不修改"的约定。

### 模拟 Fn 点按，屏蔽 F5

Typeless 用 Fn"点一下开始、再点一下结束"，遥控器是按住说话。遥指在按下时模拟一次 Fn（CGEvent，键码 63 加 `maskSecondaryFn`，按住 120 ms），松开并等音频播完后再模拟一次。开头的音频先缓存，Typeless 开始后再放出。

遥控器的语音键同时会给系统发 F5，按住时还会自动重复。遥指用 IOHIDEventSystemClient 给 VID/PID 匹配的 HID 服务写 `UserKeyMapping`，只把这个遥控器的 F5 映射为空，退出时只还原这一条。系统会给重连的遥控器建新的 HID 服务，所以每次连上都重新屏蔽。

### 通用输入法实现 + 预设

Typeless、Handy 这类"虚拟麦克风 + 快捷键"的输入法只差快捷键、点按还是按住、等待时间、要选的麦克风。只写一个通用实现，差别放进预设；会话逻辑（缓存开头、等播完再结束、误触、连按、断线补发结束）只写一次。Go 的 plugin 不适合分发，也没必要。

### 辅助功能每 2 秒检查一次

试过的几种问法：

| 问法 | 授权 | 关掉开关 | 用"−"删除 |
|---|---|---|---|
| `AXIsProcessTrusted` | 实时 | 实时 | 不变 |
| `CGPreflightPostEventAccess` | 重启前一直是 false | 不变 | 不变 |
| TCC 预检 | 始终返回 2 | — | — |
| 新启动的子进程再问 | 实时 | 实时 | 能发现 |

最后只用 `AXIsProcessTrusted` 定时检查。子进程复查和监听 tccd 的 Darwin 通知 `com.apple.tcc.access.changed` 都能发现"−"删除，但复杂度不值得，参考的几个同类 App 也都只做定时检查。被"−"删除要到下次启动才发现。

授权引导用 MyGo 的 `permissionflow`：打开系统设置对应页，下面贴一个面板把 App 拖进列表。macOS 26 上这比弹窗可靠，授权后不用重启。

### 系统菜单做菜单栏

MyGo 的弹出面板（Popover）会先激活整个 App，开着的主窗口会跟着跳到最前；换成不激活的面板后，自绘的配色又和系统菜单不一致。所以菜单栏直接用系统菜单，只放设备状态和几个操作。

### 自签名证书，开发和发布各一张

macOS 按签名要求记住隐私授权。临时签名的签名要求是代码哈希，每次重新编译都要重新授权；用证书签名时签名要求是"bundle ID + 证书"，和代码无关。

- 开发：本机自签名证书 `yaozhi-dev`，重新编译不用重新授权。bundle ID 加 `.dev`、名字是 "Yaozhi Dev"，和发布版在系统里算两个 App，同一台 Mac 上切换不会互相顶掉授权。
- 发布：GitHub Actions 用另一张自签名证书 `Yaozhi Release`（存在 Secrets 里）给 Release 里的 App 签名，用户升级后授权保留。2026-11-14 在本机用临时钥匙串走过一遍导入、签名、打包：App 带 hardened runtime 和时间戳，签名要求是 `identifier "com.github.zjiajun.yaozhi" and certificate leaf = H"…"`。

代价是没有公证：用户第一次打开要在"隐私与安全性"里手动放行。Developer ID（Apple 开发者计划，99 美元/年）能免掉这一步，以后再加；换证书会让所有用户重新授权一次。

### 同一时间只运行一个

两个遥指同时运行会都连遥控器、都屏蔽 F5、都模拟 Fn，Typeless 收到两次点按，刚开始就结束。再次打开同一个版本时，MyGo 的 `RequestSingleInstanceLock` 把这次启动转给已经在运行的那个，由它显示主窗口。这把锁按 bundle ID 区分，管不到开发版和发布版之间，所以另加一把两者共用的文件锁 `~/Library/Application Support/Yaozhi/running.lock`（`flock`，进程结束时系统自动释放，崩溃也不会留下死锁）：拿不到锁的那个弹窗说明谁在运行，然后退出，引擎和菜单栏都不启动。

## 实测数据

RC003、macOS 26、Typeless 2.10–2.11。

| 项目 | 结果 |
|---|---|
| 连接 | 从找到系统已连接的 RC003 到 ATVV 就绪约 0.1 秒 |
| 能力协商 | ATVV 1.0、16 kHz、帧长 120 字节、交互模式 `03`：按键直接发 `04` 开始推流，不发 `08` |
| 音频 | 每个通知一帧 120 字节（240 个采样），按实时速率到达；按下后约 170 ms 才有第一包；末包在 `00` 之前约 11 ms 到达，结尾不丢包 |
| 松键冲击 | 最后一帧偶尔有接近满幅的低频冲击（按键机械声），所以末帧淡出 |
| 写入 BlackHole | 遥控器发出 `00` 后 73–95 ms 播完；空闲状态下启动输出 24–55 ms |
| F5 屏蔽 | 不屏蔽时按住语音键，系统每约 84 ms 收到一次 F5；屏蔽后按住 10.9 秒一次都没收到 |
| 断线重连 | 在系统设置里断开后约 1 秒重连，2.5 秒内恢复可用 |
| 一次说话 | 按下约 430 ms 后 Typeless 开始，缓存的约 260 ms 开头语音全部补上；松键约 430 ms 后 Typeless 结束（等剩余语音约 310 ms + 点按 120 ms） |
| 误触 | 134–228 ms 的短按按误触忽略 |
| 辅助功能 | 授权、关掉开关后约 2 秒内识别 |
| 占用 | `.app` 约 10 MB；空闲 CPU 约 0.1%，内存约 70 MB |

## 风险

| 风险 | 影响 | 对策 |
|---|---|---|
| 依赖 Typeless 的行为 | Typeless 改快捷键逻辑或麦克风列表的过滤规则，链路就会断 | 输入法层已抽象，换输入法只换预设；首次引导最后一步用"在遥指里出字"自检 |
| MyGo 还年轻（v0.8） | API 可能变 | 版本固定在 `go.mod`；引擎不依赖 MyGo |
| 没有公证 | 用户第一次打开要手动放行，有人会因此放弃 | README 和 Release 说明写清步骤；以后换 Developer ID 并公证 |
| 发布证书丢失 | 换证书后所有用户都要重新授权 | 生成后放进密码管理器备份 |
| macOS 改名 | macOS 27 把"辅助功能"改叫"设备控制与数据访问" | 界面文字按系统版本显示，MyGo 的 `permissionflow` 已处理 |
| 还没验证的场景 | 睡眠唤醒、开关蓝牙、说话中途断线、系统重启后的 `yaozhi-mic` | 列入 [M1 验收](ROADMAP.md#验收) |
