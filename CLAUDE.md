# CLAUDE.md

给在这个仓库里工作的 Claude Code 的说明。和用户交流一律用中文。

## 项目

遥指（Yaozhi）：用蓝牙遥控器控制 Mac。自定义按键、切换应用是长期核心，语音输入是第一项功能（M1），按键映射在 M2。第一款设备是小米蓝牙遥控器 2 Pro（RC003），之后会接入更多设备。

- 名称和文案不要写成"只做语音"，也不要带小米品牌。
- 进度和任务拆分见 [docs/ROADMAP.md](docs/ROADMAP.md)，技术决定和原因见 [docs/TECH_CHOICES.md](docs/TECH_CHOICES.md)，开发细节见 [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)。

## 命令

```bash
make check          # gofmt 检查、go vet、go test -race；提交前必须通过
make yaozhi         # 打包 build/darwin-arm64/Yaozhi.app
make dmg            # 同上，另外打出 dmg
scripts/yaozhi.sh   # 启动 App，日志接到终端；可加 -preset、-any、-pretend
make screenshots    # 更新 docs/screenshots（README 用）
make icon           # 重画 resources/icon.png
```

- Go 版本固定在 `.go-version`（1.27.3，用 goenv 管理）。
- 访问蓝牙的程序必须以带用途说明的 `.app` 运行。直接跑二进制（包括 `go run`）会被 TCC 杀掉：退出码 134，没有输出。
- 界面改动不用打开 App 也能检查：`YAOZHI_SNAP=<目录> go test ./cmd/yaozhi -run TestSnapshots` 把各页渲染成图片。
- 需要真机操作的验证（按遥控器、开关系统权限、点菜单栏）交给用户，说清楚要做什么、看什么。

## 结构

| 路径 | 内容 |
|---|---|
| `cmd/yaozhi` | MyGo 界面：菜单栏、主窗口、首次引导。界面以 `docs/design/index.html` 为准 |
| `internal/app` | 不含界面的引擎，不依赖 MyGo |
| `internal/device` | 设备层：统一事件、设备描述；`atvvble` 是蓝牙 + ATVV 适配器 |
| `internal/voice` | 输入法层：语音会话、虚拟麦克风 + 快捷键的通用实现、输入法预设 |
| `internal/atvv` | ATVV 协议和 ADPCM 解码（纯 Go） |
| `internal/corebluetooth`、`coreaudio`、`macinput`、`macapp`、`cf` | 系统接口的 purego 绑定 |

### 两层抽象

- **设备层**：适配器只对外发统一事件（连接状态、设备信息、按键、语音开始、16 kHz 单声道 PCM 音频帧、语音结束），协议细节留在适配器里。同一协议的设备共用一个适配器，设备之间的差异（名字、型号、VID/PID、按键 usage、要屏蔽的键）写在设备描述里；协议不同才写新适配器。
- **输入法层**：语音会话只调用"开始、写入音频、结束"。缓存开头音频、等播完再结束、快速连按、断线补发结束这些逻辑只写一次。
  - A 类：虚拟麦克风 + 模拟快捷键（Typeless、Handy 等），遥指不识别。只做一个通用实现，差别（快捷键、点按还是按住、等待时间、要选的麦克风、安装方式）写在预设里。Typeless 是第一个预设。
  - B 类：遥指自己识别再输入文字。会改动"不做语音识别"这条决定，M1 只留接口、不实现。
- 设备描述和输入法预设都用 TOML，是数据不是插件，不在运行时加载代码。内置的用 `go:embed` 编进程序，用户文件放在 `~/Library/Application Support/Yaozhi/` 下对应的目录。

## 必须遵守的决定

改动前先确认不违反下面这些；需要改变其中某条时，先和用户确认。

- **不用 cgo**：原生接口一律通过 purego 调用，`CGO_ENABLED=0` 必须能编译。通过 purego 调 Objective-C 时，自动释放池的创建和释放要在同一个系统线程上（`runtime.LockOSThread()`）。
- **只支持 macOS（Apple Silicon）**。
- **BlackHole**：用户自己安装原版 BlackHole 2ch。不打包、不修改、不改名、不改 UID 或 bundle ID，不写驱动安装脚本。只按 UID `BlackHole2ch_UID` 检测，找不到就引导用户安装。不修改系统默认输入/输出设备，不改 BlackHole 的采样率。
- **聚集设备 `yaozhi-mic`**：由遥指自动创建，只含 BlackHole 2ch、固定 UID、公开可见；每次启动检查，缺了就重建；已有同名且只含 BlackHole 的设备直接用；不提供移除入口。首次引导要提示用户在 Typeless 里选它。
- **输入法**：Typeless 用 Fn"点一下开始、再点一下结束"。遥指屏蔽遥控器语音键发出的 F5，自己模拟 Fn 点按；开头的音频先缓存，结束前等音频播完。豆包输入法不支持（它会隐藏虚拟音频设备）。
- **隐私**：遥指不做语音识别，不保存、不上传音频，日志和诊断里不记录语音内容。按键监听只输出要测的键，不打印用户的其他按键。
- **签名**：发布用的自签名证书 `Yaozhi Release` 不要重新生成或更换，换了所有用户都要重新授权；发布流程见 `docs/DEVELOPMENT.md` 的"发布"。
- **权限**：模拟按键要"辅助功能"，监听按键要"输入监控"（M2 才申请）。辅助功能每 2 秒检查一次 `AXIsProcessTrusted`，不处理用"−"删除的情况。

## 代码约定

- 协议和解码这类纯逻辑放在不依赖系统框架的包里并写单测；测试数据尽量取自参考项目的测试，并在注释里写明出处。
- 新文件开头写 `// SPDX-License-Identifier: GPL-3.0-only`。
- 引用参考项目的做法时，在代码注释和 README 的"来源与许可"里注明。
- 改了界面要同步更新 `docs/design/index.html`；改了功能或进度要同步更新 `docs/ROADMAP.md`；新的技术决定写进 `docs/TECH_CHOICES.md`。
- cgo 链接系统框架报 `unknown architecture` 时，看 `docs/DEVELOPMENT.md` 的"已知环境问题"。

## 参考项目

下面两个是 GPL-3.0 的参考实现，开发机上放在仓库旁边（`../open-voice-bridge`、`../remote-mic-app`），**只读，不要修改**：

- [open-voice-bridge](https://github.com/nijez/open-voice-bridge)：RC003 适配器已在 macOS 真机验证（Swift），`apps/windows/rc003/` 里有 Python 版。ATVV 协议、ADPCM 解码、按键 usage 以这里为准。
- [remote-mic-app](https://github.com/HD838A/remote-mic-app)（无线麦 SayAll）：Typeless 的 Fn 点按流程、BlackHole 音量检测可参考。它用改过的 BlackHole（MiRemoteV 2ch），我们不走这条路。

`open-voice-bridge/Resources/RC003-remote-photo.png` 是有版权的实物照片，不在 GPL 授权范围内，不要拷贝；遥控器界面用自己画的矢量图。

## Git

- 模块路径 `github.com/zJiaJun/yaozhi`，以 GPL-3.0-only 发布。
- 提交、推送、打标签、发布 Release 都要等用户明确要求。
