#!/usr/bin/env bash
#
# Кросс-сборка релизных бинарников v-down под все платформы.
#
# Использование:
#   ./build.sh                 # версия из git (или "dev"), сборка всех целей
#   ./build.sh v1.2.0          # явная версия
#   TARGETS="windows/amd64" ./build.sh   # собрать только указанные цели
#
# Результат — в папке dist/: бинарники + архивы (.zip для Windows, .tar.gz для остальных).

set -euo pipefail
cd "$(dirname "$0")"

# --- версия ---
VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi

# --- цели сборки (GOOS/GOARCH) ---
DEFAULT_TARGETS="windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64"
TARGETS="${TARGETS:-$DEFAULT_TARGETS}"

DIST="dist"
APP="v-down"
LDFLAGS="-s -w -X main.version=${VERSION}"

rm -rf "$DIST"
mkdir -p "$DIST"

echo "▸ Сборка v-down ${VERSION}"
echo "  Цели: ${TARGETS}"
echo

for target in $TARGETS; do
  GOOS="${target%/*}"
  GOARCH="${target#*/}"

  # Дружелюбное имя ОС в названии архива.
  os_name="$GOOS"
  [ "$GOOS" = "darwin" ] && os_name="macos"

  bin="$APP"
  [ "$GOOS" = "windows" ] && bin="$APP.exe"

  stage="$DIST/${APP}_${VERSION}_${os_name}_${GOARCH}"
  mkdir -p "$stage"

  echo "  • ${GOOS}/${GOARCH} ..."
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$stage/$bin" .

  # Кладём README рядом с бинарником.
  [ -f README.md ] && cp README.md "$stage/"

  # Упаковка: zip для Windows, tar.gz для остальных.
  base="$(basename "$stage")"
  if [ "$GOOS" = "windows" ]; then
    ( cd "$DIST" && zip -qr "${base}.zip" "$base" )
  else
    ( cd "$DIST" && tar -czf "${base}.tar.gz" "$base" )
  fi

  # Промежуточную папку убираем, оставляем только архив.
  rm -rf "$stage"
done

echo
echo "▸ Готово. Артефакты в $DIST/:"
ls -1 "$DIST"
