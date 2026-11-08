#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-only
#
# 用 open 启动 bin/BLEProbe.app（先 make bleprobe），输出接到当前终端，参数原样传给 bleprobe。
# 直接跑二进制会被 TCC 以"缺少蓝牙用途说明"杀掉，见 README。
# Ctrl-C 只会结束 open，所以这里转发给 App，让它关麦、断开后退出。
# 没有终端时（例如在脚本里）用 BLEPROBE_LOG 指定输出文件。
set -eu
app="$(cd "$(dirname "$0")/.." && pwd)/bin/BLEProbe.app"
[ -d "$app" ] || { echo "找不到 $app，先运行 make bleprobe" >&2; exit 1; }
out="${BLEPROBE_LOG:-$(tty)}"
trap 'pkill -INT -x bleprobe || true' INT TERM
open -W -n --stdout "$out" --stderr "$out" "$app" --args "$@"
