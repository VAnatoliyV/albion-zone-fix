#!/bin/bash
# Собирает dist/AlbionZoneFix.zip: программа под Windows x64 + официальные файлы zapret (Flowseal).
set -e
cd "$(dirname "$0")"
ZAPRET_VER="1.10.3"
OUT=dist/AlbionZoneFix
rm -rf dist && mkdir -p "$OUT/zapret/bin" dist/cache

echo "тесты..."
go test ./... >/dev/null

echo "собираю AlbionZoneFix.exe..."
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$OUT/AlbionZoneFix.exe" .

echo "беру zapret $ZAPRET_VER с GitHub Flowseal..."
ZIP=dist/cache/zapret.zip
curl -sfL -o "$ZIP" "https://github.com/Flowseal/zapret-discord-youtube/releases/download/$ZAPRET_VER/zapret-discord-youtube-$ZAPRET_VER.zip"
unzip -q -o "$ZIP" -d dist/cache
cp dist/cache/zapret-discord-youtube-$ZAPRET_VER/bin/* "$OUT/zapret/bin/"

cp README-RU.txt LICENSES.txt "$OUT/"
(cd dist && zip -qr AlbionZoneFix.zip AlbionZoneFix)
rm -rf dist/cache
ls -l dist/AlbionZoneFix.zip | awk '{print "готово:", $NF, $5, "байт"}'
