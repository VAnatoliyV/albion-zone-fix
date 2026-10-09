#!/bin/sh
# Копия общего файла фраз тултипа (../tooltip-phrases.json рядом с
# репозиториями мака и Windows) в internal/zonecard — её встраивает
# go:embed. Звать после gen.py; потом go test ./internal/zonecard.
set -e
here=$(cd "$(dirname "$0")/../.." && pwd)
src=${1:-"$here/../tooltip-phrases.json"}
cp "$src" "$here/internal/zonecard/tooltip-phrases.json"
echo "скопировано: $src → internal/zonecard/tooltip-phrases.json"
