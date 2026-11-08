# 遥指 Yaozhi：给 Claude 的项目说明

和用户交流一律用中文。

## 产品定位

- 用遥控器控制 Mac。自定义按键、切换应用是长期核心，语音输入只是其中一项功能。名称和文案不要写成"只做语音"或带小米品牌。
- 第一款设备是小米蓝牙遥控器 2 Pro（RC003），之后会接入更多设备。从一开始就保留设备适配层：设备适配器只产出统一事件（按键事件；语音开始、音频帧、结束），语音和按键映射只消费这些事件。
- 第一版（M1）先把语音输入打通，按键映射放到 M2。进度和任务拆分见 `docs/ROADMAP.md`。

## 已定的技术决定

- 语言和界面：Go + [MyGo](https://github.com/egoist/mygo)（纯 Go 原生界面）。先只做 macOS（Apple Silicon）。
- 输入法：Typeless，它用 Fn"点一下开始、再点一下结束"。遥指要屏蔽遥控器语音键发出的 F5，自己模拟 Fn 点按；开头的音频要先缓存，结束前要等音频播完。豆包输入法不在支持范围内（它会隐藏虚拟音频设备）。
- 虚拟麦克风：用户自己安装原版 BlackHole 2ch。**不打包、不修改、不改名、不改 UID 或 bundle ID**，也不写驱动安装脚本。遥指只按 UID `BlackHole2ch_UID` 检测它，找不到就提示用户去装。不修改系统默认输入/输出设备，也不改 BlackHole 的采样率。
- Typeless 会把名字带 "(Virtual)" 的输入设备从麦克风列表里去掉，看不到 BlackHole。做法：用一个只包含原版 BlackHole 2ch 的聚集设备（`yaozhi-mic`）给 Typeless 选；遥指照旧按 UID 写入 BlackHole。聚集设备由遥指自动创建（2026-11-07 用户决定）：只含 BlackHole 2ch、固定 UID、公开可见；每次启动检查，缺了就重建；已有同名且只含 BlackHole 的设备（用户手动建的）就直接用；提供移除入口。向导里不再有创建这一步，但仍要提示用户在 Typeless 里选 yaozhi-mic。
- 原生接口一律不用 cgo，通过 purego 调用（和 MyGo 一致，`CGO_ENABLED=0` 可编译）。CoreBluetooth 在 `internal/corebluetooth`，Core Audio 在 `internal/coreaudio`，按键映射和注入在 `internal/macinput`，共用的 CoreFoundation 在 `internal/cf`。不含界面的引擎在 `internal/app`，MyGo 界面在 `cmd/yaozhi`（配置 `mygo.json`，`make yaozhi` 打包）；界面以 `docs/design/` 里定稿的原型为准。
- 遥指不做语音识别，不保存、不上传音频，日志里不记录语音内容。

## 两层抽象

- **设备层**（`internal/device`）：适配器只对外发统一事件：连接状态、按键、语音开始、16 kHz 单声道 PCM 音频帧、语音结束。解码等协议细节留在适配器里。同一协议的设备共用一个适配器（如 `internal/device/atvvble`：蓝牙 + ATVV），设备之间的差异（名字、型号、VID/PID、按键 usage、要屏蔽的键）写在设备描述文件里；协议不同才写新适配器。
- **输入法层**：语音会话只调用"开始、写入音频、结束"这个接口。缓存开头音频、等播完再结束、快速连按、断线补发结束这些逻辑只写一次。
  - A 类：虚拟麦克风 + 模拟快捷键（Typeless、Handy 等），遥指不识别。只做一个通用实现，各输入法之间的差别（快捷键、点按切换还是按住说话、结束前等待、要选的麦克风）写在预设文件里。Typeless 是第一个预设。
  - B 类：遥指自己识别再输入文字（本地模型或云端接口）。会改动"不做语音识别"这条决定，M1 只留接口、不实现。
- 设备描述和输入法预设都用 **TOML**（能写注释，原生支持 `0x2717` 这类十六进制数）。它们是数据，不是插件，不在运行时加载代码。内置的用 `go:embed` 编进程序，用户可以把自己的文件放进 `~/Library/Application Support/Yaozhi/` 下对应的目录。

## 参考项目（只读，不要修改）

本机 `../open-voice-bridge` 和 `../remote-mic-app` 是参考实现，两者都是 GPL-3.0：

- `open-voice-bridge`：RC003 适配器已在 macOS 真机验证（Swift）；`apps/windows/rc003/` 里有 Python 版。ATVV 协议、ADPCM 解码、按键 usage 以这里为准。
- `remote-mic-app`（无线麦 SayAll）：Typeless 的 Fn 点按流程、BlackHole 音量检测等可参考。它用的 MiRemoteV 2ch 是改过的 BlackHole，我们不走这条路。

`open-voice-bridge/Resources/RC003-remote-photo.png` 是有版权的实物照片，不在 GPL 授权范围内，不要拷贝；遥控器界面用自己画的矢量图。

本项目以 GPL-3.0-only 发布，新增代码引用参考项目时在 README 的"来源与许可"里注明。

## 开发

- Go 版本固定在 `.go-version`（1.27.3，用 goenv 管理）。
- 提交前跑 `make check`（gofmt 检查、`go vet`、`go test -race`）。
- 协议和解码这类纯逻辑放在不依赖系统框架的包里并写单测；测试数据尽量取自参考项目的测试，并在注释里写明出处。
- 访问蓝牙等隐私接口的程序必须以带用途说明的 `.app` 运行（见 `make bleprobe`），从 Claude Code 或终端直接跑二进制会被 TCC 杀掉（退出码 134，无输出）。模拟按键、监听按键要"辅助功能""输入监控"权限（见 `make inputprobe`），`make yaozhi` 用本机的 `yaozhi-dev` 证书签名，重新编译不用重新授权；probe 工具仍是临时签名，每次重新编译都要用户重新授权。
- 按键监听只输出要测的键，不打印用户的其他按键。
- cgo 链接系统框架报 `unknown architecture` 时，先看 README 的"已知环境问题"。

## Git

- 模块路径是 `github.com/zJiaJun/yaozhi`，但仓库目前只在本地。
- 提交、建 GitHub 仓库、推送都要等用户明确要求。
