# 开发文档

## 环境

- Apple Silicon 的 Mac，macOS 13 或更新
- Go 1.27.3（写在 [`.go-version`](../.go-version)，用 goenv 管理）
- MyGo 的命令行通过 `go.mod` 里的 `tool` 声明引入，不用单独安装

```bash
goenv install 1.27.3
make check
```

## 常用命令

| 命令 | 作用 |
|---|---|
| `make check` | gofmt 检查、`go vet`、`go test -race`，CI 跑的也是它 |
| `make yaozhi` | 用 MyGo 打包成 `build/darwin-arm64/Yaozhi.app`（`CGO_ENABLED=0`） |
| `make dmg` | 同上，另外打出 `yaozhi-<版本>-arm64.dmg` |
| `scripts/yaozhi.sh [参数]` | 启动打包好的 App，日志接到当前终端，Ctrl-C 正常退出 |
| `make icon` | 按 `internal/icon` 里的几何重画 `resources/icon.png` |
| `make screenshots` | 无窗口渲染各页，更新 `docs/screenshots/` 里 README 用的截图 |

## 运行和调试

macOS 要求访问蓝牙的程序在 `Info.plist` 里写明用途，所以遥指必须以 `.app` 运行。直接运行 `go run ./cmd/yaozhi` 或编译出的二进制会被 TCC 杀掉（退出码 134，没有输出）。

```bash
make yaozhi && scripts/yaozhi.sh
```

没有终端时（例如在脚本里），用 `YAOZHI_LOG=<文件>` 指定日志输出。

启动参数：

| 参数 | 作用 |
|---|---|
| `-preset <id>` | 选输入法预设，默认 `typeless` |
| `-any` | 也接受没有设备描述的 ATVV 遥控器，用来试新设备 |
| `-pretend <项>` | 在什么都装好的 Mac 上看首次启动的引导。`all`，或用逗号隔开的 `bluetooth`、`remote`、`blackhole`、`typeless`、`typeless-off`、`ax`、`text`。只改界面显示，点某一步的按钮会结束对这一步的假装 |

用户文件在 `~/Library/Application Support/Yaozhi/`：

| 路径 | 内容 |
|---|---|
| `settings.json` | 暂停开关、音量增益、引导进度（只记是否完成，不记文字） |
| `stats.json` | 使用次数和时长 |
| `devices/*.toml` | 用户自己的设备描述，和内置的合并，`id` 相同时替换内置的 |
| `presets/*.toml` | 用户自己的输入法预设，规则同上 |
| `diagnostics/` | "设备"页"导出诊断"写出的文件 |

### 权限

| 权限 | 用途 | 什么时候申请 |
|---|---|---|
| 蓝牙 | 连接遥控器、接收语音 | 首次引导第 1 步点"继续"后 |
| 辅助功能 | 模拟 Fn 点按 | 首次引导第 4 步，用 MyGo 的 `permissionflow` 引导把 App 拖进列表 |
| 输入监控 | 监听按键（M2） | M1 不申请 |

遥指每 2 秒检查一次辅助功能（`AXIsProcessTrusted`），授权和开关变化都能跟上。用"−"把 App 从列表里删掉时系统接口不会变，要到下次启动才会发现，这是有意的取舍。

### 签名

macOS 记住隐私授权时，存的是 App 的签名要求。临时签名（`codesign -s -`）的签名要求就是代码哈希，重新编译后对不上，蓝牙和辅助功能都要重新授权，列表里还会留下失效的旧条目。用证书签名时，签名要求是"bundle ID + 证书"，和代码内容无关，重新编译后授权仍然有效。

开发时建一张本机自签名证书就够了：

1. 打开"钥匙串访问"，选"证书助理 → 创建证书"。
2. 名称填 `yaozhi-dev`，身份类型选"自签名根证书"，证书类型选"代码签名"。不需要设为信任。

`make yaozhi` 发现钥匙串里有这张证书就用它签名（带 hardened runtime 和时间戳），没有就退回临时签名；也可以用 `make yaozhi SIGN=<身份>` 指定。从临时签名换成证书签名后要再授权一次，并删掉列表里旧的 Yaozhi。这张证书只在本机有效；发布用的是另一张证书，见下面的"发布"。

## 发布

推送 `v*` 标签时，[`.github/workflows/release.yml`](../.github/workflows/release.yml) 在 GitHub 的 macOS 运行器上：

1. 核对标签和 `mygo.json` 的 `version` 是否一致；
2. 跑 `make check`；
3. 把发布证书导入一个临时钥匙串，`make dmg SIGN="Yaozhi Release"` 签名并打包；
4. 检查签名，生成 `SHA256SUMS.txt`；
5. 和 dmg 一起上传到同名的**草稿** Release，说明取自 [`.github/release-notes.md`](../.github/release-notes.md) 加上自动生成的变更列表。标签带 `-`（如 `v0.1.0-beta.1`）时标为预发布。

检查草稿没问题后，在网页上点发布。同一个标签重跑 workflow 会替换草稿里的文件。

### 发布证书

Release 里的 App 用自签名证书 `Yaozhi Release` 签名，证书和私钥存在仓库的 Secrets 里。用户授权时 macOS 记住的是"bundle ID + 证书"，所以**只要一直用同一张证书，用户升级后蓝牙和辅助功能都不用重新授权**。证书丢了或换了，所有用户都要重新授权一次，所以生成后要备份。

只需要做一次：

```bash
scripts/release-cert.sh release-cert
```

脚本在仓库根目录的 `release-cert/` 里生成 `yaozhi-release.p12` 和它的密码 `password.txt`，并打印存进 Secrets 的两条 `gh secret set` 命令（`MACOS_CERTIFICATE`、`MACOS_CERTIFICATE_PASSWORD`）。`release-cert/` 和 `*.p12` 都在 `.gitignore` 里，只留在本地；另外在密码管理器里也存一份，本地文件丢了还能找回。

自签名证书没有经过 Apple 公证，用户第一次打开时 Gatekeeper 会拦，要在"系统设置 → 隐私与安全性"里点"仍要打开"。以后加入 Apple 开发者计划后，换成 Developer ID 证书，在 `mygo.json` 里加 `macos.notarize`，workflow 再导入公证凭据即可（见 MyGo 文档 `docs/github-actions.md`）。注意换证书同样会让用户重新授权一次。

### 发一个版本

1. 把 `mygo.json` 的 `version` 改成新版本（例如 `0.1.0`）并提交。
2. 打标签并推送：

   ```bash
   git tag v0.1.0
   ```

   ```bash
   git push origin v0.1.0
   ```

3. 等 workflow 跑完，在 Releases 页面检查草稿，没问题就点发布。

## 项目结构

```
cmd/yaozhi/              MyGo 界面：菜单栏、主窗口四页、首次引导
internal/
  app/                   不含界面的引擎：设备、语音会话、屏蔽语音键、权限和设备检查
  device/                设备层：统一事件、Device 接口、TOML 设备描述
    atvvble/             蓝牙 + ATVV 的设备适配器，断线自动重连
    devices/             内置设备描述（xiaomi-rc003.toml）
  voice/                 输入法层：语音会话、增益、虚拟麦克风 + 快捷键的通用实现
    presets/             内置输入法预设（typeless.toml）
  atvv/                  ATVV 协议、IMA ADPCM 解码、会话状态机（纯 Go）
  corebluetooth/         CoreBluetooth 中心角色的 purego 绑定
  coreaudio/             按 UID 找音频设备、聚集设备、AudioQueue 播放
  macinput/              按设备屏蔽按键（UserKeyMapping）、模拟 Fn、监听按键
  macapp/                判断 App 是否安装、是否在运行
  cf/                    CoreFoundation 的公共部分
  icon/                  App 图标和菜单栏图标（有向距离场绘制）
  tomlfile/              严格解析 TOML、按目录加载
resources/               打包资源（App 图标）
scripts/                 启动脚本
docs/
  design/index.html      界面设计稿（HTML 原型）
  screenshots/           README 用的截图
```

### 两层抽象

- **设备层**（`internal/device`）：适配器只对外发统一事件：连接状态、设备信息、按键、语音开始、16 kHz 单声道 PCM 音频帧、语音结束。协议细节留在适配器里。同一协议的设备共用一个适配器，设备之间的差异（名字、型号、VID/PID、按键 usage、要屏蔽的键）写在设备描述里；协议不同才写新适配器。
- **输入法层**（`internal/voice`）：语音会话只调用"开始、写入音频、结束"。缓存开头、等播完再结束、误触过滤、连按排队、断线补发结束只写一次。各输入法的差别（快捷键、点按还是按住、等待时间、要选的麦克风、安装方式）写在预设里。

设备描述和输入法预设都是数据，不是插件，不在运行时加载代码。

### 新增设备或输入法

- **同协议的遥控器**：在 `internal/device/devices/` 加一个 TOML，参照 `xiaomi-rc003.toml`。先用 `-any` 启动确认能连上、能收到语音。
- **虚拟麦克风 + 快捷键类的输入法**：在 `internal/voice/presets/` 加一个 TOML，参照 `typeless.toml`，用 `-preset <id>` 启动。
- 用户也可以把同样的文件放进 `~/Library/Application Support/Yaozhi/` 下对应的目录，不用重新编译。

## 约定

- 调系统接口一律通过 [purego](https://github.com/ebitengine/purego)，不用 cgo，`CGO_ENABLED=0` 必须能编译。
- 协议和解码这类纯逻辑放在不依赖系统框架的包里并写单测；测试数据尽量取自参考项目的测试，并在注释里写明出处。
- 引用参考项目的做法时，在代码注释和 README 的"来源与许可"里注明。
- 日志不记录语音内容；按键监听只输出要测的键，不打印用户的其他按键。
- 界面以 [`docs/design/index.html`](design/index.html) 为准，改界面时同步更新设计稿。设计稿用任意静态服务器打开，例如 `cd docs/design && python3 -m http.server`；支持 `?theme=dark&page=voice&state=listening&setup=3` 这类参数直接定位到某个状态。
- 界面改动可以用 `YAOZHI_SNAP=<目录> go test ./cmd/yaozhi -run TestSnapshots` 渲染成图片检查，不用打开 App。

## 已知环境问题

目前的代码不用 cgo，不受下面这个问题影响；以后引入需要 cgo 的依赖时才会遇到。

如果 Command Line Tools 的链接器比默认 SDK 旧（例如 ld 26.6 搭配 MacOSX27 SDK），cgo 链接系统框架时会报 `unknown architecture`。常见原因是一次更新里先后装了新旧两版 Command Line Tools，旧版覆盖了链接器，新版 SDK 却留了下来；`xcrun --show-sdk-version` 会显示比工具链新的版本。删掉多出来的 SDK，或安装与它配套的 Command Line Tools 即可。

在修好之前，`Makefile` 会自动改用 `MacOSX26.5.sdk`；直接运行 `go` 命令时需要自己设置：

```bash
export SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk
```
