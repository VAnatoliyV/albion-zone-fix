#!/usr/bin/env python3
"""Фразы тултипа портала дорог Авалона из данных игры (ao-bin-dumps).

Источник — localization.xml из github.com/ao-data/ao-bin-dumps (TMX, все
языки клиента). Результат — общий tooltip-phrases.json для мака и Windows:
  {"<язык>": {"marker": [...], "unstable": [...], "closes": [...],
              "party": [...], "biome": [...], "notMarker": [...],
              "free": [...],
              "units": {"d": [...], "h": [...], "m": [...], "s": [...]}}}

  python3 gen.py [путь/к/localization.xml] [выход.json]
"""
import json, re, sys
import xml.etree.ElementTree as ET

SRC = sys.argv[1] if len(sys.argv) > 1 else \
    '/Users/anatoliivoronin/Developer/albion-fame/дампы/localization.xml'
OUT = sys.argv[2] if len(sys.argv) > 2 else \
    '/Users/anatoliivoronin/Developer/albion-fame/tooltip-phrases.json'

LANGS = {'EN-US': 'en', 'RU-RU': 'ru', 'ES-ES': 'es', 'PL-PL': 'pl', 'DE-DE': 'de',
         'TR-TR': 'tr', 'FR-FR': 'fr', 'PT-BR': 'pt', 'IT-IT': 'it',
         'AR-SA': 'ar', 'ID-ID': 'id', 'JA-JP': 'ja', 'KO-KR': 'ko',
         'ZH-CN': 'zh-cn', 'ZH-TW': 'zh-tw'}

FIELDS = {
    # «Road of Avalon to» — заголовок тултипа портала дорог.
    'marker': ['@ROADS_OF_AVALON_TOOLTIP_TITLE'],
    # «Unstable Roads to» — портал в один конец из Брецилиена/Мглы.
    'unstable': ['@PROXIMITY_TOOLTIP_MISTS_CITY_EXIT_SUB_TITLE'],
    # «Closes in» (тултип портала) и «Closes in {0}» (общая строка).
    'closes': ['@ROADS_OF_AVALON_TOOLTIP_CLOSE_TIME', '@GENERIC_CLOSES_IN',
               '@PROXIMITY_TOOLTIP_MISTS_CLOSES_IN',
               '@PROXIMITY_TOOLTIP_MISTS_CITY_EXIT_CLOSES_IN',
               '@PROXIMITY_TOOLTIP_MISTS_DUNGEON_EXIT_CLOSES_IN'],
    # «Closes to your party in {0}» — у нестабильного пути.
    'party': ['@PROXIMITY_TOOLTIP_MISTS_CITY_EXIT_CLOSES_IN',
              '@PROXIMITY_TOOLTIP_MISTS_DUNGEON_EXIT_CLOSES_IN'],
    # Не признак: строка «Biome: Roads of Avalon» в тултипе выхода из зоны.
    # biome — подпись «Biome» (строку с ней не берём), notMarker — значение
    # «Roads of Avalon» (у ko/zh оно совпадает с заголовком портала!).
    'biome': ['@PROXIMITY_TOOLTIP_EXIT_BIOME'],
    'notMarker': ['@PROXIMITY_TOOLTIP_EXIT_BIOME_ROADS'],
    # «Free to use» — вместо времени у бесплатного портала.
    'free': ['@ROADS_OF_AVALON_TOOLTIP_FREE_TIME'],
}
UNITS = {'d': '@GENERIC_TIME_DAYS_SHORT', 'h': '@GENERIC_TIME_HOURS_SHORT',
         'm': '@GENERIC_TIME_MINUTES_SHORT', 's': '@GENERIC_TIME_SECONDS_SHORT'}

XML_LANG = '{http://www.w3.org/XML/1998/namespace}lang'
want = set(sum(FIELDS.values(), [])) | set(UNITS.values())
loc = {}
for _, el in ET.iterparse(SRC):
    if el.tag == 'tu':
        if el.get('tuid') in want:
            loc[el.get('tuid')] = {t.get(XML_LANG): (t.findtext('seg') or '')
                                   for t in el.findall('tuv')}
        el.clear()
missing = want - set(loc)
if missing:
    sys.exit('нет ключей: %s' % sorted(missing))


def pieces(s):
    """Строка без [icon:…] и {0}: куски текста вокруг подстановки."""
    s = re.sub(r'\[icon:[^\]]*\]', '', s)
    out = []
    for p in s.split('{0}'):
        p = p.strip().rstrip(':：。.').strip()
        if p:
            out.append(p)
    return out


res = {}
for code, lang in LANGS.items():
    e = {}
    for f, keys in FIELDS.items():
        seen = []
        for k in keys:
            for p in pieces(loc[k].get(code, '')):
                if p not in seen:
                    seen.append(p)
        e[f] = seen
    e['units'] = {u: [loc[k][code].strip()] for u, k in UNITS.items()}
    for f in ('marker', 'unstable', 'closes'):
        if not e[f]:
            sys.exit('%s: пусто %s' % (lang, f))
    res[lang] = e

with open(OUT, 'w', encoding='utf-8') as fh:
    json.dump(res, fh, ensure_ascii=False, indent=1)
    fh.write('\n')
print('записано', OUT, len(res), 'языков')
