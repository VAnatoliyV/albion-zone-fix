#!/bin/bash
# Собирает dist/AlbionJournal.zip: программа под Windows x64 (Albion Journal: сбор цен,
# счётчик, Zone Fix) + приёмник своих цен acp-prices.exe + таблица предметов + официальные файлы zapret (Flowseal)
# + своё распознавание текста в ocr\ (ONNX Runtime и модель PaddleOCR, ocr/files.txt)
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
# Установщик удаляет у старых копий только файлы zapret\bin из этого списка
# (internal/oldcopy/zapret_bin.txt, вшит в exe): он должен совпадать с выпуском.
if ! diff <(ls "$OUT/zapret/bin" | LC_ALL=C sort) <(LC_ALL=C sort internal/oldcopy/zapret_bin.txt) >/dev/null; then
  echo "состав zapret\bin изменился: ls $OUT/zapret/bin > internal/oldcopy/zapret_bin.txt и собрать заново"; exit 1
fi

echo "беру своё распознавание текста (ocr/files.txt)..."
# Файлы не в git: скачиваются по зафиксированным адресам в кэш, SHA256
# скачанного и самого файла проверяются (иначе сборка останавливается).
DL="$HOME/.cache/albion-journal/dl"
mkdir -p "$OUT/ocr" "$DL"
sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
# vcredist FILE — файл из VC_redist.x64.exe (WiX): прикреплённый контейнер —
# второй кабинет MSCF в exe, в нём кабинет vcRuntimeMinimum (a12), в нём файл.
VCX=""
vcredist() {
  if [ -z "$VCX" ]; then
    command -v cabextract >/dev/null || { echo "нет cabextract: brew install cabextract"; exit 1; }
    VCX="$(mktemp -d)"
    python3 - "$1" "$VCX/att.cab" <<'PY'
import struct, sys
d = open(sys.argv[1], 'rb').read()
offs = []
i = d.find(b'MSCF\0\0\0\0')
while i >= 0:
    offs.append(i)
    i = d.find(b'MSCF\0\0\0\0', i + 1)
o = offs[1]  # первый — интерфейс установщика, второй — пакеты
n = struct.unpack('<I', d[o + 8:o + 12])[0]
open(sys.argv[2], 'wb').write(d[o:o + n])
PY
    cabextract -q -d "$VCX/att" "$VCX/att.cab"
    cabextract -q -d "$VCX/min" "$VCX/att/a12" 2>/dev/null
  fi
  cat "$VCX/min/$2"
}
while read -r name url dsha member msha; do
  case "$name" in ''|'#'*) continue ;; esac
  f="$DL/$(basename "$url")"
  if [ ! -f "$f" ] || [ "$(sha "$f")" != "$dsha" ]; then
    curl -sfL -o "$f.part" "$url"
    mv "$f.part" "$f"
  fi
  [ "$(sha "$f")" = "$dsha" ] || { echo "$url: SHA256 не тот — файл удалён"; rm -f "$f"; exit 1; }
  if [ "$member" = "-" ]; then
    cp "$f" "$OUT/ocr/$name"
  elif [ "${member#vcredist:}" != "$member" ]; then
    vcredist "$f" "${member#vcredist:}" > "$OUT/ocr/$name"
  else
    unzip -p "$f" "$member" > "$OUT/ocr/$name"
  fi
  [ "$(sha "$OUT/ocr/$name")" = "$msha" ] || { echo "$name: SHA256 не тот"; exit 1; }
done < ocr/files.txt
[ -n "$VCX" ] && rm -rf "$VCX"
cp ocr/*-LICENSE.txt "$OUT/ocr/"
# Список ocr для удаления установщиком (только свои файлы, как у zapret).
OLIST="$PWD/dist/ocr-delete.nsh"
(cd "$OUT" && find ocr -type f | sed 's|/|\\|g; s|.*|  Delete "$INSTDIR\\&"|') > "$OLIST"

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
# Список файлов zapret для удаления (удаляем только свои файлы, не папку целиком).
ZLIST="$PWD/dist/zapret-delete.nsh"
(cd "$OUT" && find zapret -type f | sed 's|/|\\|g; s|.*|  Delete "$INSTDIR\\&"|') > "$ZLIST"
# Версия из четырёх чисел для свойств файла: 1.2 -> 1.2.0.0
V4="$(printf '%s' "$VERSION" | awk -F. '{for(i=NF+1;i<=4;i++)$i=0; print $1"."$2"."$3"."$4}' OFS=.)"
makensis -V2 -DVERSION="$VERSION" -DVERSION4="$V4" -DZLIST="$ZLIST" -DOLIST="$OLIST" installer.nsi
ls -l dist/AlbionJournal.zip dist/AlbionJournal.zip.sig dist/AlbionJournalSetup-"$VERSION".exe | awk '{print "готово:", $NF, $5, "байт"}'
echo "выпуск: тег v$VERSION в VAnatoliyV/albion-zone-fix, вложения AlbionJournal.zip, AlbionJournal.zip.sig и AlbionJournalSetup-$VERSION.exe"
