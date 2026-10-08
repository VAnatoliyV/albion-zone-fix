#!/bin/bash
# Собирает dist/AlbionJournal.zip: программа под Windows x64 (Albion Journal: сбор цен,
# счётчик, Zone Fix) + таблица предметов + официальные файлы zapret (Flowseal).
set -e
cd "$(dirname "$0")"
ZAPRET_VER="1.10.3"
OUT=dist/AlbionJournal
ITEMS=../items_by_id.json # таблица предметов для счётчика урона (та же, что у мака)
rm -rf dist && mkdir -p "$OUT/zapret/bin" dist/cache

echo "тесты..."
go test ./... >/dev/null

echo "собираю AlbionJournal.exe..."
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$OUT/AlbionJournal.exe" .

echo "беру zapret $ZAPRET_VER с GitHub Flowseal..."
ZIP=dist/cache/zapret.zip
curl -sfL -o "$ZIP" "https://github.com/Flowseal/zapret-discord-youtube/releases/download/$ZAPRET_VER/zapret-discord-youtube-$ZAPRET_VER.zip"
unzip -q -o "$ZIP" -d dist/cache
cp dist/cache/zapret-discord-youtube-$ZAPRET_VER/bin/* "$OUT/zapret/bin/"

cp README-RU.txt LICENSES.txt "$OUT/"
cp "$ITEMS" "$OUT/items_by_id.json"
(cd dist && zip -qr AlbionJournal.zip AlbionJournal)
rm -rf dist/cache
ls -l dist/AlbionJournal.zip | awk '{print "готово:", $NF, $5, "байт"}'
