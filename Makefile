# Сборка Video Downloader: консольный бинарник, .app для macOS и .exe для Windows.
#
#   make build        — bin/v-down (веб- и консольный режимы)
#   make run          — запустить веб-оболочку в браузере
#   make vet          — go vet ./...
#   make app          — dist/Video Downloader.app (macOS, нужен swiftc)
#   make windows      — dist/v-down.exe (консольный сервер под Windows)
#   make windows-app  — dist/VideoDownloader.exe (окно WebView2) + архив
#   make dist         — релизные архивы всех платформ (build.sh)
#   make icons        — перерисовать иконки (.icns и .ico) из Swift-скриптов
#   make clean        — убрать bin/ и dist/

GO      ?= go
SWIFTC  ?= swiftc
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

BIN  := bin
DIST := dist

.PHONY: all build run vet test clean app windows windows-app windows-icon icons dist

all: build

build: $(BIN)/v-down

$(BIN)/v-down: $(wildcard *.go) go.mod
	@mkdir -p $(BIN)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $@ .

run: build
	$(BIN)/v-down

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

dist:
	./build.sh $(VERSION)

clean:
	rm -rf $(BIN) $(DIST)

# ---------------------------------------------------------------- macOS .app
# Ручной бандл через swiftc — Xcode-проект для одного файла не нужен.
APP := $(DIST)/Video Downloader.app

app: $(BIN)/v-down app/VideoDownloaderApp.swift app/Info.plist.in app/make-icon.swift
	rm -rf "$(APP)"
	mkdir -p "$(APP)/Contents/MacOS" "$(APP)/Contents/Resources"
	$(SWIFTC) -O -swift-version 5 -o "$(APP)/Contents/MacOS/VideoDownloader" app/VideoDownloaderApp.swift
	cp $(BIN)/v-down "$(APP)/Contents/Resources/"
	sed "s/@VERSION@/$(VERSION)/" app/Info.plist.in > "$(APP)/Contents/Info.plist"
	rm -rf $(DIST)/icon.iconset
	swift app/make-icon.swift $(DIST)/icon.iconset
	iconutil -c icns -o "$(APP)/Contents/Resources/AppIcon.icns" $(DIST)/icon.iconset
	rm -rf $(DIST)/icon.iconset
	# Подпись ad-hoc: без неё macOS ругается на неподписанный бандл.
	codesign --force --deep -s - "$(APP)"
	@echo "готово: $(APP)"

# -------------------------------------------------------------- Windows .exe
# Консольный сервер: тот же код, что и на маке, кросс-сборка без cgo.
windows: $(DIST)/v-down.exe

$(DIST)/v-down.exe: $(wildcard *.go) go.mod
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $@ .

# Оболочка с окном WebView2 — отдельный модуль в app-windows (чтобы главный
# бинарник остался без зависимостей). -H windowsgui убирает окно консоли.
WINAPP := $(DIST)/VideoDownloader.exe

windows-app: $(DIST)/v-down.exe $(WINAPP) docs/README-windows.md
	cp docs/README-windows.md $(DIST)/README-windows.md
	cd $(DIST) && rm -f video-downloader-windows.zip && \
		zip -q video-downloader-windows.zip VideoDownloader.exe v-down.exe README-windows.md
	@echo "готово: $(DIST)/video-downloader-windows.zip (оболочка + движок + README)"

$(WINAPP): $(wildcard app-windows/*.go) app-windows/go.mod
	@mkdir -p $(DIST)
	cd app-windows && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		$(GO) build -trimpath -ldflags "-H windowsgui $(LDFLAGS)" -o ../$(WINAPP) .

# Иконка .ico лежит в репозитории: сборка не должна требовать Swift под рукой.
windows-icon:
	swift app-windows/make-ico.swift app-windows/icon.ico
	cd app-windows && $(GO) run github.com/akavel/rsrc@v0.10.2 \
		-ico icon.ico -arch amd64 -o rsrc_windows_amd64.syso

icons: windows-icon
	@echo "иконка macOS перерисовывается на каждой сборке make app"
