# 遥指 Yaozhi

用遥控器指挥你的 Mac。

遥指把蓝牙遥控器变成 Mac 的控制器：自定义按键、切换应用，并支持按住遥控器说话、由输入法转成文字。第一款支持的设备是**小米蓝牙遥控器 2 Pro（RC003）**，之后会接入更多设备。

> 当前状态：**M0 已完成，正在做 M1**。已经能按住遥控器说话、松开出字，界面还在完善。路线图见 [docs/ROADMAP.md](docs/ROADMAP.md)，M0 的技术选型见 [docs/TECH_CHOICES.md](docs/TECH_CHOICES.md)。

## 语音输入原理

遥指不做语音识别，只负责把遥控器变成一支"麦克风"：

1. **声音**：遥控器经 BLE 的 ATVV 协议（Android TV 语音协议）发送 ADPCM 音频，遥指把它解码成 16 kHz 单声道，写入虚拟声卡 BlackHole 2ch。输入法通过一个包含 BlackHole 2ch 的聚集设备收音（见下文）。
2. **开始和结束**：Typeless 只认"点一下开始、再点一下结束"，遥控器只能"按住说话"。遥指在按下时模拟一次 Fn 点按，松开并等音频播放完后再模拟一次。

遥指不修改系统默认的输入/输出设备，也不保存或上传任何音频。

## 使用前需要自己准备

- macOS（Apple Silicon）
- [BlackHole 2ch](https://github.com/ExistentialAudio/BlackHole)：**需要你自己单独安装**，遥指不打包、不修改它，例如 `brew install blackhole-2ch`。请选 2ch 版本，并且只给遥指用：BlackHole 是回环设备，任何 App 播放到它里面的声音都会被输入法一起听到。如果还要录制系统声音，另装 16ch 给录屏用
- 聚集设备：Typeless 会把虚拟声卡从麦克风列表里去掉，所以要用"音频 MIDI 设置"包一层：点左下角"+"→"创建聚集设备"，只勾选 BlackHole 2ch，改名为 `yaozhi-mic`（名字里不要有 "communication"）。BlackHole 本身不用改
- Typeless（例如 `brew install --cask typeless`）：在它的设置里把麦克风选为 `yaozhi-mic`，触发键保持 Fn
- 系统设置 → 键盘 → "按下 🌐 键时" 设为"不执行任何操作"

## 开发

需要 Go 1.27.3（版本已写在 `.go-version`，用 goenv 时会自动切换）。

```bash
goenv install 1.27.3
make check
```

`make check` 依次运行 gofmt 检查、`go vet` 和 `go test -race`。

调用 macOS 原生接口时**不用 cgo**，统一通过 [purego](https://github.com/ebitengine/purego)，和 MyGo 的做法一致，`CGO_ENABLED=0` 就能编译。

### 运行遥指

界面用 [MyGo](https://github.com/egoist/mygo) 的原生界面，配置在 `mygo.json`。打包成 `build/darwin-arm64/Yaozhi.app`，再用脚本启动，输出接回当前终端。不能直接运行二进制：macOS 要求访问蓝牙的 App 在 Info.plist 里写明用途，直接运行的二进制算在终端 App 头上，会被直接杀掉（退出码 134，没有输出）。

```bash
make yaozhi
```

```bash
scripts/yaozhi.sh
```

第一次运行会弹出蓝牙授权。辅助功能在窗口"准备情况"里点"去授权"：系统设置会打开，把下面面板里的 Yaozhi 拖进列表即可，不用重启。

#### 签名

macOS 记住隐私授权时，存的是 App 的签名要求。临时签名（`codesign -s -`）的签名要求就是代码哈希，重新编译后对不上，蓝牙和辅助功能都要重新授权，列表里还会留下失效的旧条目。用证书签名时，签名要求是"bundle ID + 证书"，和代码内容无关，重新编译后授权仍然有效。

开发时建一张本机自签名证书就够了：打开"钥匙串访问"，选"证书助理 → 创建证书"，名称填 `yaozhi-dev`，身份类型选"自签名根证书"，证书类型选"代码签名"。不需要把它设为信任。`make yaozhi` 发现钥匙串里有这张证书就用它签名（带 hardened runtime 和时间戳），没有就退回临时签名；也可以用 `make yaozhi SIGN=<身份>` 指定。从临时签名换成证书签名后要最后授权一次，并删掉列表里旧的 Yaozhi。这张证书只在本机有效，发给别人用要 Developer ID 签名并公证。

关掉窗口后遥指留在菜单栏，点图标打开面板，右键是菜单。常用参数加在脚本后面：`-preset <id>` 选输入法预设（默认 `typeless`），`-any` 接受没有设备描述的 ATVV 遥控器。

设计稿在 `docs/design/`，用 `python3 -m http.server -d docs/design` 打开：`main-window.html`（主窗口）、`onboarding.html`（首次设置向导）、`permissions.html`（权限状态）。

### 已知环境问题

目前的代码不用 cgo，不受下面这个问题影响；以后引入需要 cgo 的依赖时才会遇到。

如果 Command Line Tools 的链接器比默认 SDK 旧（例如 ld 26.6 搭配 MacOSX27 SDK），cgo 链接系统框架时会报 `unknown architecture`。常见原因是一次更新里先后装了新旧两版 Command Line Tools，旧版覆盖了链接器，新版 SDK 却留了下来；`xcrun --show-sdk-version` 会显示比工具链新的版本。删掉多出来的 SDK，或安装与它配套的 Command Line Tools 即可。

在修好之前，`Makefile` 会自动改用 `MacOSX26.5.sdk`；直接运行 `go` 命令时需要自己设置：

```bash
export SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk
```

## 目录

| 路径 | 内容 |
|---|---|
| `cmd/yaozhi` | 遥指本体：MyGo 菜单栏图标、面板和主窗口 |
| `internal/app` | 不含界面的部分：设备、语音会话、屏蔽语音键、权限和设备检查，给界面一份状态 |
| `internal/device` | 设备层：统一事件、`Device` 接口、TOML 设备描述（内置 `devices/xiaomi-rc003.toml`） |
| `internal/device/atvvble` | 蓝牙 + ATVV 的设备适配器，断线自动重连 |
| `internal/voice` | 输入法层：语音会话（开头缓存、误触、淡出、连按排队）、TOML 输入法预设（内置 `presets/typeless.toml`）、虚拟麦克风 + 快捷键的通用实现 |
| `internal/tomlfile` | 严格解析 TOML、按目录加载 |
| `internal/atvv` | ATVV 协议、IMA ADPCM 解码、会话状态机（纯 Go，可单测） |
| `internal/corebluetooth` | CoreBluetooth 中心角色的 purego 绑定（不用 cgo） |
| `internal/cf` | CoreFoundation 的公共部分（字符串、数字、数组、字典） |
| `internal/coreaudio` | 按 UID 查找音频设备，用 AudioQueue 播放到指定设备 |
| `internal/macinput` | 按设备屏蔽按键（UserKeyMapping）、模拟 Fn、监听按键 |
| `internal/icon` | 画遥指的图形：App 图标和菜单栏图标（A1"墨指"） |
| `cmd/genicon` | 生成 `resources/icon.png`（`make icon`） |
| `resources` | 打包用的资源：App 图标 |
| `scripts/yaozhi.sh` | 以 `.app` 方式启动遥指 |
| `docs/design` | 界面设计稿（HTML 原型） |

## 来源与许可

本项目以 **GPL-3.0-only** 发布，见 [LICENSE](LICENSE)。

ATVV 协议处理参考了以下 GPL-3.0 项目：

- [open-voice-bridge](https://github.com/nijez/open-voice-bridge)：已在 macOS 真机验证的 RC003 适配器
- [remote-mic-app（无线麦 SayAll）](https://github.com/HD838A/remote-mic-app)
- [remote-bridge-hub](https://github.com/xxb26553663-star/remote-bridge-hub)

`internal/macinput` 屏蔽 F5（`UserKeyMapping`，只还原自己改的那一条）和模拟 Fn 点按（键码 63、120 ms）的做法参考了 remote-mic-app 的 `RemoteVoiceFunctionMapper.swift` 和 `KeyboardInjector.swift`。`internal/coreaudio` 的 AudioQueue 用法参考了 [MyGo](https://github.com/egoist/mygo)（MIT）的 `audio/output_darwin.go`。

`internal/device/atvvble` 的连接流程（先找系统已连接的设备；订阅 Audio 和 Control 后再请求能力）参考了 open-voice-bridge 的 `XiaomiBluetoothBridge.swift`；RC003 设备描述里的名字、型号和按键 usage 取自 open-voice-bridge 的 `VoiceBridgeDeviceProfile.swift`、`AppSettings.swift` 和 `RemoteButtons.swift`。`internal/voice` 的语音会话流程（先缓存开头、点按后放出、等播完再点按结束）参考了 remote-mic-app 的 `VoiceFnTapSessionController.swift`。

第三方依赖：[purego](https://github.com/ebitengine/purego)（Apache-2.0）、[MyGo](https://github.com/egoist/mygo)（MIT，其权限引导插件参照 [PermissionFlow](https://github.com/jaywcjlove/PermissionFlow)，MIT）、[BurntSushi/toml](https://github.com/BurntSushi/toml)（MIT）。

BlackHole 由用户自行安装，不随本项目分发。"小米"和"Xiaomi"是其权利人的商标，本项目与小米无关。
