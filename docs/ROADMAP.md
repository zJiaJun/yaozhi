# 路线图

定位：用遥控器控制 Mac，语音输入是其中一项功能。第一版先把语音输入打通。

## M0 技术验证（约一周，验证代码可以丢弃）

先逐个证明风险点能用 Go 打通。

- [x] **T0.1 仓库骨架**：GPL-3.0、Go module、`make check`、CI 配置（推送到 GitHub 后生效）
- [x] **T0.2 BLE 连通**：用 Go 命令行连接 RC003、订阅 ATVV；决定用 cgo（tinygo bluetooth）还是不用 cgo 自己绑定。通过标准：按住语音键能打印出 `04`、音频包、`00`（原定还有 `08`，见下面的真机记录）
  - 选型：不用 cgo，用 purego 自己绑定 CoreBluetooth（`internal/corebluetooth`）。MyGo 规定不用 cgo，`mygo build` 默认 `CGO_ENABLED=0`；tinygo bluetooth 走 cgo，而且没有暴露 `retrieveConnectedPeripherals`，而配对后的 RC003 由系统保持连接、不再广播，主要靠这个接口才能找到
  - 命令行进程访问蓝牙要有 `NSBluetoothAlwaysUsageDescription`，否则会被 TCC 直接杀掉。开发工具要包成 `.app`，T0.6 的正式 App 也一样
  - 真机记录（2026-11-05，`cmd/bleprobe`，7 段语音）：
    - 系统已连接设备里直接找到 RC003，型号字段为 `RC003`，从连接到 ATVV 就绪约 0.1 秒
    - 能力协商：版本 1.0、16 kHz、帧长 120 字节、交互模式 `03`。这个模式下按键直接发 `04` 开始推流，**不发 `08`**，主机不用发 MIC_OPEN；`08` 的处理仍要保留，给需要主机开麦的设备
    - 每个音频通知正好一帧 120 字节（240 个采样），到达速率等于实时速率，没有成批补发；7 段都没收到 `0A` 同步
    - `04` 之后约 170 ms 才有第一包音频，所以解码时长比按住时长短约 170 ms。这段缺失来自遥控器，遥指这边没有丢包
    - 末包都在 `00` 之前约 11 ms 到达，没有迟到音频被丢弃，结尾不丢字
    - 松键时最后一帧有时会出现低频冲击（一次接近满幅 -32617，一次 -8037），应该是按键的机械声。M1 写入 BlackHole 时，最后一帧要淡出或限幅
- [x] **T0.3 ADPCM 解码**：`internal/atvv`，测试数据与 open-voice-bridge 对照。真机音频用 `scripts/bleprobe.sh -wav <目录>` 导出 WAV，2026-11-05 人耳确认能听清
- [x] **T0.4 写入 BlackHole**：按 UID `BlackHole2ch_UID` 找到设备并播放 16 kHz PCM；左右声道写同一份数据（Typeless 按单声道录音，会把两个声道混成一个），按设备当前采样率重采样，不改 BlackHole 自身的采样率。通过标准：用 QuickTime 从 BlackHole 录音能听到人声
  - 实现：`internal/coreaudio`，purego 调用 AudioQueue（做法参考 MyGo 的 `audio/output_darwin.go`）。输入 16 kHz 双声道 16 位，重采样到 BlackHole 的 48 kHz 由 AudioQueue 完成；3 个 20 ms 缓冲区，没数据时填静音，每个缓冲区播完时记录播放进度
  - 真机记录（2026-11-06，`bleprobe -play`）：QuickTime 从 BlackHole 录到了人声；收到 `00` 后 73–95 ms 播完。M1 的"等播完再点结束"按约 100 ms 设计，另加超时兜底
- [x] **T0.5 Fn 点按与 F5 屏蔽**：只对 RC003 把 F5 映射为空，并能恢复；用 CGEvent 模拟 Fn 点按。通过标准：Typeless 能被模拟点按开始和结束，遥控器语音键本身不再触发
  - 实现：`internal/macinput`。F5 屏蔽用 IOHIDEventSystemClient 给 VID/PID 匹配 RC003 的 HID 服务写 `UserKeyMapping`（`0x70000003E → 0`），恢复时只还原 F5 这一条；Fn 点按用 CGEvent 发键码 63 加 `maskSecondaryFn`，按下 120 ms 后松开（同 remote-mic-app）
  - 真机记录（2026-11-06，`cmd/inputprobe`）：不屏蔽时语音键就是 F5，按住时系统每约 84 ms 自动重复一次按下；屏蔽后按住 10.9 秒，系统一个 F5 都没收到；两次模拟 Fn 让 Typeless 开始和结束，备忘录里出了字；退出后映射恢复原样
  - 权限：模拟按键要"辅助功能"，监听按键要"输入监控"。临时签名的授权跟着二进制走，每次重新编译都要重新授权；T0.6 前要定一个固定的签名身份
- [x] **发现：Typeless 看不到 BlackHole**。Typeless 2.10.0 的麦克风列表会去掉名字带 "(Virtual)" 的设备（"系统默认"一项除外），而 Chromium 会给 BlackHole 的名字加上这个后缀。解决：用"音频 MIDI 设置"建一个只包含原版 BlackHole 2ch 的聚集设备（后缀是 "(Aggregate)"），Typeless 选它。2026-11-06 已验证可用，并用它跑通了遥控器 → BlackHole → Typeless 出字
- [x] **T0.6 MyGo 外壳**：菜单栏图标和窗口；在打包好的 `.app` 里运行 T0.2 的代码。通过标准：弹出蓝牙授权并能连上遥控器。**先和用户对齐界面的展示效果和布局，再开发**
  - 界面已定稿（2026-11-07）：主窗口 `docs/design/main-window.html`（版本一·舞台：跟随系统外观，浅色为摄影棚风格；底部状态岛统一显示语音、权限和提醒；概览里的"准备情况"就绪时收成一个胶囊；错峰入场、数字滚动，支持减少动态效果，窗口在后台时暂停动画）、首次设置向导 `docs/design/onboarding.html`（最后一步在向导自己的输入框里出字，自动判定成功）、权限状态 `docs/design/permissions.html`
  - 第一版不做按键（M2）：主窗口只有概览、语音、设备三页，不申请输入监控
  - 实现：`cmd/yaozhi` 用 MyGo 原生界面（`ui` 包）做菜单栏图标、弹出面板和主窗口骨架；引擎在 `internal/app`（设备、语音会话、屏蔽 F5、每 2 秒检查辅助功能 / BlackHole / yaozhi-mic）；`mygo.json` 配置，`make yaozhi` 打出 `build/darwin-arm64/Yaozhi.app`（临时签名，Info.plist 带蓝牙用途说明）
  - 真机记录（2026-11-07）：打包后的 App 连上遥控器并屏蔽 F5；用 MyGo `permissionflow` 把 Yaozhi 拖进辅助功能列表，授权后不重启就能模拟 Fn，按住说话在备忘录出字（按下约 430 ms 后 Typeless 开始，松键约 440 ms 后结束）。MyGo 拖拽时控制台有一条 `NSFilenamesPboardType is not a valid UTI` 警告，不影响使用
- [x] **结论**：写一页技术选型记录，决定是否继续。见 [TECH_CHOICES.md](TECH_CHOICES.md)：继续，进入 M1

## M1 语音输入 v0.1

- [x] 设备层：`Device` 接口、统一事件、RC003 适配器（识别、会话、断线重连）
  - 实现：`internal/device`（事件、TOML 设备描述，内置 `devices/xiaomi-rc003.toml`）、`internal/device/atvvble`（蓝牙 + ATVV，按描述匹配设备）
  - 真机记录（2026-11-07）：8 段语音（含 134–228 ms 的短按）都有开始和结束；在系统设置里断开遥控器后约 1 秒重连，2.5 秒内恢复可用。遥指运行时会立刻把遥控器连回来，以后菜单栏要有"暂停"
- [ ] 音频层：检测和选择 BlackHole、增益、实时电平
- [x] 聚集设备 yaozhi-mic：遥指自动创建（2026-11-07 决定）。`AudioHardwareCreateAggregateDevice`，固定 UID，只含 BlackHole 2ch；每次启动检查、缺了就重建，所以不依赖重启后是否保留；用户手动建过的同名设备直接用；菜单里可以移除。已实现（`coreaudio.CreateAggregate`，引擎每 2 秒检查）
  - 真机记录（2026-11-07）：遥指先认出并直接用了用户手动建的 yaozhi-mic；用户删掉它后，约 2 秒内遥指建好自己的（`com.github.zjiajun.yaozhi.mic`，只含 BlackHole 2ch）；Typeless 能在麦克风列表里选到它，按住说话出字。待验证：系统重启后的情况（每次启动会重建，所以只影响 Typeless 是否要重新选）、"移除 yaozhi-mic 并退出"
- [ ] 语音会话：Typeless 点按控制（缓存开头音频、等播完再结束、快速连按不串、断线补发结束点按）；松键时最后一帧淡出；电源键映射为 F20
  - 已完成（`internal/voice`）：输入法预设（TOML，内置 `presets/typeless.toml`）、会话逻辑（开头缓存、语音不足 150 ms 当误触、最后一帧淡出、连按排队、断线照样结束）、VirtualMic（点按式和按住式）；`cmd/yaozhi` 接好了 F5 屏蔽（每次重连重新屏蔽）和辅助功能权限检测
  - 真机记录（2026-11-07，`make yaozhi`）：遥指单独运行，按住说话、松开在备忘录出字；按下约 430 ms 后 Typeless 开始，缓存的约 260 ms 开头语音全部补上；松键约 430 ms 后 Typeless 结束（等剩余语音播完约 310 ms + 点按 120 ms）；228 ms 的短按按误触忽略
  - 待做：电源键映射为 F20；断线中断和授权后不重启即可模拟按键这两项还没在真机上验证；结尾等待能否缩短
- [x] App 图标和菜单栏图标：定稿 A1"墨指"黑白原稿（2026-11-08 用户决定；比较过的配色和头尾互换方案在提交 79cb87e 的 `docs/design/icons-a1.html`、`icons-a1-swap.html`）。`internal/icon` 用有向距离场按设计稿的几何画图，`make icon` 生成 `resources/icon.png`，MyGo 打包成 icns；菜单栏用同一图形的模板图，需要处理时右下角加橙点或红点。macOS 26 能识别成标准圆角方形，不会套灰底
  - 待做：装 Xcode 26 后用 Icon Composer 做分层的 `.icon`（`mygo.json` 的 `macos.icon`），支持深色、着色和透明模式；菜单栏"正在听"和"已暂停"两种状态等菜单栏界面开发时一起做
- [ ] 菜单栏和设置窗口
- [ ] 首次设置向导和权限检测
  - 权限状态设计已定（2026-11-07，原型 `docs/design/permissions.html`）：主窗口"准备情况"清单，权限和语音链路每项一行，五种状态（未允许、等待中、已允许、已被关闭、更新后没生效）；状态岛、菜单栏图标和菜单栏面板跟着变；运行中持续检查，权限被关闭时发通知
  - 授权引导用 MyGo 的 `plugins/permissionflow`：打开系统设置对应页，下面贴面板把 Yaozhi 拖进列表，面板文字先固定用英文；中文提示和成功反馈由遥指自己的状态岛给出
  - 蓝牙在向导第一步请求，按钮写"继续"；辅助功能在设置语音时请求；输入监控到 M2 设置按键映射时再请求
- [ ] 日志和诊断（不记录语音内容）
  - M0 的验证工具 bleprobe、audioprobe、inputprobe 已删除（2026-11-08，主链路都已并入遥指；代码在提交 9c5b252 里）。还没并进来的：读遥控器电量和型号（`cmd/bleprobe/info.go`，做主窗口时并进 `atvvble`）；列出音频设备和聚集设备成员（`audioprobe -list`，放进诊断信息）；监听按键（`inputprobe -watch`，M2 做进按键设置界面，`macinput.WatchKeys` 还在）
- [ ] 打包签名（先用固定的本机签名身份，避免每次编译都要重新授权；之后 Developer ID 签名和公证）
  - 本机签名已完成（2026-11-08）：`make yaozhi` 在钥匙串里有 `yaozhi-dev` 自签名证书时用它签名（hardened runtime + 时间戳），签名要求为 `identifier "com.github.zjiajun.yaozhi" and certificate leaf = H"…"`。真机记录：授权一次后改代码重新编译三次（代码哈希每次都变），辅助功能和蓝牙都没有失效，hardened runtime 下蓝牙、ATVV、F5 屏蔽正常
  - 待做：Developer ID 签名和公证（发布前）
- [x] 降低空闲占用：AudioQueue 只在说话时运行（开始时启动，结束 3 秒后停），设备检查改成监听 Core Audio 的设备变化通知
  - 真机记录（2026-11-08）：空闲 CPU 从约 2.5–3% 降到 0.0%，真实内存约 45 MB；从空闲状态启动输出 24–55 ms，藏在按下后约 420 ms 的缓冲窗口里；冷启动、连续说话两种情况开头缓存都是约 255 ms，和改前一致
- 验收：真机连续 20 次按住说话都能出字，开头和结尾不丢字；Mac 睡眠唤醒、遥控器断线后都能自动恢复

## M2 控制 Mac v0.2

- [ ] 读取遥控器按键，尽量独占设备
- [ ] 识别单击、双击、长按，按住时连续触发
- [ ] 动作库：按键、快捷键录制、媒体键、打开 App、切换应用
- [ ] 矢量遥控器设置界面，按下实体键时实时高亮
- [ ] 按当前 App 自动切换键位方案；配置导入导出

## M3 更多设备

- [ ] 用 JSON 文件描述设备
- [ ] 第二款设备：优先选同样走 ATVV 的遥控器（如 Chromecast 语音遥控器、小米蓝牙遥控器 2 非 Pro）
- [ ] 设备选择界面

## 不做

- 打包、修改或重命名 BlackHole
- 豆包输入法（它会隐藏虚拟音频设备）
- 语音识别本身
- 修改系统默认音频设备
