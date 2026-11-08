# M0 技术选型记录

2026-11-07。M0 的目标是逐个证明风险点能用 Go 打通，这里汇总每个环节选了什么、为什么、真机测到了什么，以及还留着的风险。

## 结论

**继续，进入 M1。** 整条链路已经在真机上跑通：遥指在 MyGo 打包的 `.app` 里连上小米蓝牙遥控器 2 Pro，按住语音键说话，松开后 Typeless 在备忘录出字。授权辅助功能、断线重连、F5 屏蔽都验证过。目前没有发现需要换技术路线的问题。

## 各环节的选择

| 环节 | 选择 | 为什么 | 放弃的方案 |
|---|---|---|---|
| 语言和界面 | Go + MyGo 原生界面（`ui` 包） | 单个二进制约 10 MB，不带网页内核；MyGo 自带菜单栏面板、权限引导、动画、Liquid Glass | MyGo 网页界面：要 Bun 和前端工具链；Swift：和"用 Go 写"的目标不符 |
| 调系统接口 | purego，不用 cgo | 和 MyGo 一致，`CGO_ENABLED=0` 就能编译，不依赖本机 SDK 和链接器（本机 ld 读不了 MacOSX27 SDK） | tinygo bluetooth：走 cgo，也没有暴露找已连接设备的接口 |
| 蓝牙 | 自己绑定 CoreBluetooth（`internal/corebluetooth`） | 配对后的 RC003 由系统保持连接、不再广播，必须用 `retrieveConnectedPeripheralsWithServices` 才找得到 | 扫描广播：配对后的遥控器不广播 |
| 语音协议 | ATVV + IMA ADPCM 解码（`internal/atvv`，纯 Go） | 以 open-voice-bridge 的真机实现为准，测试数据取自它 | — |
| 设备抽象 | 适配器只发统一事件；同协议设备的差异写 TOML 描述文件（`internal/device`） | 加同协议的新遥控器不用改代码 | JSON：不能写注释，不支持十六进制；YAML：缩进和隐式类型容易出错 |
| 虚拟麦克风 | 用户自己装的原版 BlackHole 2ch，遥指用 AudioQueue 写入（`internal/coreaudio`） | 不打包、不修改驱动；AudioQueue 自己把 16 kHz 重采样到 48 kHz | 改版 BlackHole（remote-mic-app 的做法）：要分发驱动 |
| 给 Typeless 选的麦克风 | 只包含 BlackHole 2ch 的聚集设备 yaozhi-mic | Typeless 会去掉名字带 "(Virtual)" 的输入设备，聚集设备的后缀是 "(Aggregate)" | 改 BlackHole 的名字或 UID：违反"不修改"的约定 |
| 控制输入法 | 模拟 Fn 点按（CGEvent），屏蔽遥控器发的 F5（IOHIDEventSystemClient 的 UserKeyMapping，只针对这个遥控器） | Typeless 用 Fn 点一下开始、再点一下结束；不屏蔽的话 F5 会被系统当普通按键 | 让 Typeless 直接绑 F5：Typeless 不支持按住式，而且 F5 按住时会自动重复 |
| 输入法抽象 | 通用的"虚拟麦克风 + 快捷键"实现，各输入法的差别写进 TOML 预设（`internal/voice`） | Typeless、Handy 这类只差快捷键和触发方式 | 每个输入法写一个插件：没必要，Go 的 plugin 也不适合分发 |
| 权限 | 辅助功能用 `AXIsProcessTrusted` 实时检测；授权引导用 MyGo 的 `permissionflow`（打开系统设置，下面贴面板把 App 拖进列表） | `CGPreflightPostEventAccess` 在授权后到重启前一直返回 false；macOS 26 上拖进列表比弹窗可靠 | 自己写拖拽面板：MyGo 已经有了 |

## 真机数据

| 项目 | 结果 |
|---|---|
| 连接 | 找到系统已连接的 RC003 到 ATVV 就绪约 0.1 秒 |
| 音频 | 16 kHz，每包一帧 120 字节，按实时速率到达；按下后约 170 ms 才有第一包；结尾不丢包 |
| 写入 BlackHole | 遥控器发出 `00` 后 73–95 ms 播完 |
| F5 屏蔽 | 不屏蔽时按住语音键，系统每约 84 ms 收到一次 F5；屏蔽后按住 10.9 秒一次都没收到 |
| 断线重连 | 在系统设置里断开后约 1 秒重连，2.5 秒内恢复可用 |
| 一次说话 | 按下约 430 ms 后 Typeless 开始，期间缓存的约 260 ms 开头语音全部补上；松键约 430–440 ms 后 Typeless 结束 |
| 误触 | 134–228 ms 的短按按误触忽略，不打扰 Typeless |
| 授权 | 用 MyGo 的引导把 App 拖进辅助功能列表，授权后不重启就能模拟 Fn |
| 体积和占用 | `.app` 9.8 MB；主窗口开着时常驻内存约 110 MB，空闲 CPU 约 2.5–3% |

## 风险和待定事项

| 事项 | 影响 | 打算 |
|---|---|---|
| 签名 | 已解决开发期部分（2026-11-08）：临时签名下每次重新编译授权都会失效 | 开发期用本机自签名证书 `yaozhi-dev`，授权按 bundle ID + 证书认，重新编译三次权限都保留；发布前仍要 Developer ID 签名并公证 |
| yaozhi-mic 怎么建 | 已定：遥指自动创建，Typeless 能选到并收音（2026-11-07 真机验证） | 每次启动检查、缺了就重建；待测系统重启后 Typeless 是否要重新选 |
| 依赖 Typeless 的行为 | Typeless 改快捷键逻辑或麦克风列表的过滤规则，链路就会断 | 输入法层已经抽象出来，换输入法只换预设；向导最后一步用"在向导里出字"来自检 |
| MyGo 还年轻（v0.8.1） | API 可能变；拖拽时有一条 `NSFilenamesPboardType` 警告 | 版本固定在 go.mod；界面和引擎分开（`internal/app` 不依赖 MyGo）；警告可以反馈给 MyGo |
| 空闲占用偏高 | 已解决（2026-11-08）：空闲 CPU 从约 3% 降到 0 | AudioQueue 只在说话时运行，设备检查改为监听系统通知 |
| F5 屏蔽在重连后失效 | 系统会给重连的遥控器建新的 HID 服务 | 已在每次连上和 2 秒后各重新屏蔽一次；还要实测睡眠唤醒 |
| 还没验证的 | 睡眠唤醒、开关 Mac 蓝牙、说话中途断线时 Typeless 是否被正确结束、电量读取 | 放进 M1 验收 |
| macOS 27 改名 | "辅助功能"改叫"设备控制与数据访问" | 界面文字按系统版本显示，MyGo 的 `Title()` 已经处理 |
