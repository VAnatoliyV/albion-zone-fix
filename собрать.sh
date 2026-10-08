#!/bin/bash
# Собирает dist/AlbionJournal.zip: программа под Windows x64 (Albion Journal: сбор цен,
# счётчик, Zone Fix) + приёмник своих цен acp-prices.exe + таблица предметов + официальные файлы zapret (Flowseal)
# и подпись dist/AlbionJournal.zip.sig для автообновления, плюс установщик
# dist/AlbionJournalSetup-<версия>.exe (NSIS, installer.nsi; те же файлы, что в zip).
#
#   ./собрать.sh          версия из файла VERSION
#   ./собрать.sh 1.0.1    версия из аргумента (и записывается в VERSION)
#
# Подпись — закрытым ключом ~/.config/albion-journal/windows-update.key
# (tools/updatekey; формат — internal/update/sign.go). Без ключа сборка
# останавливается: выпуск без подписи программа не поставит.
set -e
cd "$(dirname "$0")"
# Только числа через точку: иначе программа сочтёт себя сборкой разработчика.
valid() { printf '%s' "$1" | grep -Eq '^[0-9]+(\.[0-9]+)+$'; }
if [ -n "$1" ]; then
  valid "$1" || { echo "версия «$1» не похожа на 1.2.3 — VERSION не трогаю"; exit 1; }
  echo "$1" > VERSION
fi
VERSION="$(tr -d ' \r\n' < VERSION)"
valid "$VERSION" || { echo "в VERSION «$VERSION» — не похоже на 1.2.3"; exit 1; }
echo "версия $VERSION"
ZAPRET_VER="1.10.3"
OUT=dist/AlbionJournal
RECV=../acp-prices-src # приёмник своих цен (тот же код, что у мака)
ITEMS=../items_by_id.json # таблица предметов для счётчика урона (та же, что у мака)
rm -rf dist && mkdir -p "$OUT/zapret/bin" dist/cache

echo "тесты..."
go test ./... >/dev/null

echo "собираю AlbionJournal.exe..."
# -H windowsgui: у программы окно и трей, чёрная консоль больше не нужна.
# Значок exe (кролик) — rsrc_windows_amd64.syso, Go подхватывает его сам. Пересобрать:
#   go run github.com/akavel/rsrc@v0.10.2 -ico internal/desktop/rabbit.ico -arch amd64 -o rsrc_windows_amd64.syso
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$VERSION" -o "$OUT/AlbionJournal.exe" .

echo "собираю acp-prices.exe..."
(cd "$RECV" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$OLDPWD/$OUT/acp-prices.exe" .)

echo "беру zapret $ZAPRET_VER с GitHub Flowseal..."
ZIP=dist/cache/zapret.zip
curl -sfL -o "$ZIP" "https://github.com/Flowseal/zapret-discord-youtube/releases/download/$ZAPRET_VER/zapret-discord-youtube-$ZAPRET_VER.zip"
unzip -q -o "$ZIP" -d dist/cache
cp dist/cache/zapret-discord-youtube-$ZAPRET_VER/bin/* "$OUT/zapret/bin/"

cp README-RU.txt LICENSES.txt "$OUT/"
cp TESTER-RU.txt dist/ # памятка тестеру рядом с выпуском (в zip и установщик не входит)
cp "$ITEMS" "$OUT/items_by_id.json"
(cd dist && zip -qr AlbionJournal.zip AlbionJournal)
rm -rf dist/cache

echo "подписываю..."
go run ./tools/updatekey sign dist/AlbionJournal.zip "$VERSION"
go run ./tools/updatekey verify dist/AlbionJournal.zip "$VERSION"
echo "собираю установщик (NSIS)..."
command -v makensis >/dev/null || { echo "нет makensis: brew install makensis"; exit 1; }
makensis -V2 -DVERSION="$VERSION" installer.nsi
ls -l dist/AlbionJournal.zip dist/AlbionJournal.zip.sig dist/AlbionJournalSetup-"$VERSION".exe | awk '{print "готово:", $NF, $5, "байт"}'
echo "выпуск: тег v$VERSION в VAnatoliyV/albion-zone-fix, вложения AlbionJournal.zip, AlbionJournal.zip.sig и AlbionJournalSetup-$VERSION.exe"
