#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-only
#
# 生成发布用的自签名代码签名证书，供 GitHub Actions 给 Release 里的 App 签名。
#
#   scripts/release-cert.sh <输出目录>
#
# 输出目录里会有 yaozhi-release.p12（证书和私钥）和它的密码 password.txt。
# 按脚本最后打印的命令存进仓库的 Secrets，然后删掉这个目录，或把它放进密码管理器。
#
# 用户授权时，macOS 记住的是"bundle ID + 这张证书"，所以只要一直用同一张证书签名，
# 用户升级遥指后蓝牙和辅助功能不用重新授权。证书丢了或换了，所有用户都要重新授权一次。
# 它不能代替 Developer ID：Gatekeeper 照样会拦第一次打开。
set -eu

[ $# -eq 1 ] || { echo "用法：$0 <输出目录>" >&2; exit 2; }
out="$1"
[ ! -e "$out/yaozhi-release.p12" ] || { echo "$out/yaozhi-release.p12 已存在，换一个目录" >&2; exit 1; }
mkdir -p "$out"
chmod 700 "$out"

# 用系统自带的 LibreSSL：它生成的 .p12 用 3DES 和 SHA-1，`security import` 一定能读；
# OpenSSL 3 及以后默认的算法，旧版 macOS 读不了。
ssl=/usr/bin/openssl
name="Yaozhi Release"

"$ssl" req -x509 -newkey rsa:3072 -sha256 -days 3650 -nodes \
	-subj "/CN=$name" \
	-addext "basicConstraints=critical,CA:false" \
	-addext "keyUsage=critical,digitalSignature" \
	-addext "extendedKeyUsage=critical,codeSigning" \
	-keyout "$out/key.pem" -out "$out/cert.pem" 2>/dev/null

"$ssl" rand -base64 24 >"$out/password.txt"
"$ssl" pkcs12 -export -name "$name" \
	-inkey "$out/key.pem" -in "$out/cert.pem" \
	-passout "file:$out/password.txt" -out "$out/yaozhi-release.p12"
rm "$out/key.pem" "$out/cert.pem"

cat <<EOF
已生成 $out/yaozhi-release.p12（签名身份 "$name"，有效期 10 年）。

存进 GitHub 仓库的 Secrets：

  base64 -i "$out/yaozhi-release.p12" | gh secret set MACOS_CERTIFICATE
  gh secret set MACOS_CERTIFICATE_PASSWORD < "$out/password.txt"

存好后把这个目录放进密码管理器备份，再删掉本地文件。
EOF
