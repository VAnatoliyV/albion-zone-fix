# Albion Journal для Windows

Программа под Windows для игроков Albion Online — версия мак-приложения
Albion Journal, выросшая из Albion Zone Fix: сбор цен (свои цены и отправка
в Albion Online Data Project), счётчик фейма, серебра и урона и Zone Fix —
для тех, у кого при переходе между локациями висит чёрный экран или выкидывает
из игры (часто у провайдеров в России).

- **Сбор цен и счётчик**: встроенный разборщик форка
  [albiondata-client](https://github.com/ao-data/albiondata-client) получает те же
  пакеты, что и Zone Fix. Отправку в ADP и счётчик можно выключить.
- **Zone Fix — статистика переходов**: какая локация сколько грузилась, сколько потом молчал
  сервер (чёрный экран после загрузки), где выкинуло.
- **Обход фильтров провайдера** только для трафика Albion (UDP 5055/5056) на движке
  [zapret](https://github.com/bol-van/zapret) из сборки
  [Flowseal](https://github.com/Flowseal/zapret-discord-youtube); 7 стратегий
  и автоподбор лучшей по статистике.
- **Запись пакетов** игры для разбора.

Скачать: раздел **Releases**, файл `AlbionJournal.zip`. Инструкция — `README-RU.txt` внутри.
Данные программы — в `%AppData%\Albion Journal\` (файлы Zone Fix из папки
программы переносятся туда при первом запуске).

Программа неофициальная, файлы и память игры не трогает. Albion Online —
товарный знак Sandbox Interactive GmbH.

## Сборка

Go 1.25+, на любой ОС: `./собрать.sh` → `dist/AlbionJournal.zip`.
Нужен форк сборщика рядом: `../albiondata-client` (подключён через `replace` в go.mod)
и таблица предметов `../items_by_id.json`.
Разбор присланной записи: `go run . -replay запись.azf`.
