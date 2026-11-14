GO ?= go

# 本机 Command Line Tools 的链接器（ld 26.6）读不了 MacOSX27 SDK 的 .tbd，
# cgo 链接系统框架（CoreBluetooth、IOKit 等）会报 "unknown architecture"。
# 存在 26.5 SDK 时默认用它；装好匹配的工具链后可用 `make SDKROOT=...` 覆盖。
SDK_FALLBACK := /Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk
ifneq ($(wildcard $(SDK_FALLBACK)),)
SDKROOT ?= $(SDK_FALLBACK)
export SDKROOT
endif

.PHONY: check fmt vet test yaozhi icon

check: fmt vet test

fmt:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "需要 gofmt:"; echo "$$unformatted"; exit 1; fi

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./...

# 遥指本体：MyGo 打包成 build/darwin-arm64/Yaozhi.app（配置见 mygo.json），用 scripts/yaozhi.sh 启动，输出接到终端。
# 签名：钥匙串里有名为 yaozhi-dev 的代码签名证书就用它（自签名即可，见 README"签名"），
# 授权按 bundle ID + 证书认，重新编译不失效；没有就临时签名，每次重新编译都要重新授权。也可以 make yaozhi SIGN=<身份>。
SIGN ?= $(shell security find-identity -p codesigning 2>/dev/null | grep -q '"yaozhi-dev"' && echo yaozhi-dev || echo -)
yaozhi:
	CGO_ENABLED=0 $(GO) tool mygo build -skip-dmg -sign "$(SIGN)"

# App 图标：按 internal/icon 里的图形重画 resources/icon.png（MyGo 打包时转成 icns）。
icon:
	$(GO) run internal/icon/gen.go -o resources/icon.png
