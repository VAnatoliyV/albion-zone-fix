package i18n

// Карточка зоны (этап 4): надписи — как в Loc.table мак-версии (App.swift),
// только %@ и %d заменены на %s. Виды дорог, которые на маке подписаны
// пустой строкой (обычные и «чёрные» дороги), здесь не заведены вовсе:
// пустой перевод словарь не пропускает, а zonecard.Kind и страница
// понимают отсутствие ключа как «без подписи».
func init() {
	for k, v := range cardTable {
		Table[k] = v
	}
}

var cardTable = map[string][3]string{
	// качество зоны
	"q.safe":     {"синяя зона", "safe zone", "zona azul"},
	"q.yellow":   {"жёлтая зона", "yellow zone", "zona amarilla"},
	"q.red":      {"красная зона", "red zone", "zona roja"},
	"q.black":    {"чёрная зона", "black zone", "zona negra"},
	"q.city":     {"город", "city", "ciudad"},
	"q.island":   {"остров", "island", "isla"},
	"q.mists":    {"туманы", "the Mists", "las Nieblas"},
	"q.instance": {"подземелье", "instance", "instancia"},
	"q.roads":    {"дороги Авалона", "Roads of Avalon", "caminos de Avalon"},
	"q.other":    {"зона", "zone", "zona"},
	"q.grade":    {"качество %s", "quality %s", "calidad %s"},

	// виды дорог
	"type.TUNNEL_HIDEOUT":      {"дорога убежищ", "hideout road", "camino de refugios"},
	"type.TUNNEL_HIDEOUT_DEEP": {"глубокая дорога убежищ", "deep hideout road", "camino de refugios profundo"},
	"type.TUNNEL_DEEP":         {"глубокая", "deep", "profunda"},
	"type.TUNNEL_DEEP_RAID":    {"глубокая рейдовая", "deep raid", "profunda de incursión"},
	"type.TUNNEL_ROYAL":        {"королевская", "royal", "real"},
	"type.TUNNEL_ROYAL_RED":    {"королевская красная", "royal red", "real roja"},

	// ресурсы, лагеря, сундуки, подземелья
	"res.FIBER":            {"волокно", "fiber", "fibra"},
	"res.HIDE":             {"шкуры", "hide", "pieles"},
	"res.ORE":              {"руда", "ore", "mineral"},
	"res.ROCK":             {"камень", "stone", "piedra"},
	"res.WOOD":             {"дерево", "wood", "madera"},
	"camp.group":           {"групповой", "group", "grupal"},
	"camp.solo_small":      {"соло малый", "small solo", "solo pequeño"},
	"camp.solo_big":        {"соло большой", "big solo", "solo grande"},
	"camp.raid_small":      {"рейдовый малый", "small raid", "incursión pequeña"},
	"camp.raid_big":        {"рейдовый большой", "big raid", "incursión grande"},
	"chest.small":          {"малый", "small", "pequeño"},
	"chest.small_elite":    {"малый элитный", "small elite", "pequeño élite"},
	"chest.medium_veteran": {"средний ветеранский", "medium veteran", "mediano veterano"},
	"chest.medium_elite":   {"средний элитный", "medium elite", "mediano élite"},
	"dng.group":            {"групповое", "group", "grupal"},
	"dng.solo":             {"соло", "solo", "solo"},
	"dng.raid":             {"рейдовое", "raid", "incursión"},

	// карточка
	"zn.cardTitle":  {"КАРТОЧКА ЗОНЫ", "ZONE CARD", "TARJETA DE ZONA"},
	"zn.hint":       {"Наведись в игре на портал и нажми %s", "Point at a portal in game and press %s", "Apunta a un portal en el juego y pulsa %s"},
	"zn.keyOff":     {"Кнопка карточки выключена в настройках.", "The zone card button is off in the settings.", "El botón de la tarjeta está desactivado en los ajustes."},
	"zn.busy":       {"снимаю…", "capturing…", "capturando…"},
	"zn.shot":       {"снято %s", "captured %s", "capturado %s"},
	"zn.noTooltip":  {"Под курсором не портал дорог. Наведись на воронку и повтори.", "No roads portal under the cursor. Point at one and retry.", "No hay portal bajo el cursor. Apunta a uno y repite."},
	"zn.failed":     {"Снимок не вышел: %s", "Capture failed: %s", "Fallo la captura: %s"},
	"zn.ocrFailed":  {"Распознавание текста не вышло: %s", "Text recognition failed: %s", "Falló el reconocimiento de texto: %s"},
	"zn.unknown":    {"Не узнал зону: «%s»", "Zone not recognised: “%s”", "Zona no reconocida: «%s»"},
	"zn.blank":      {"Снимок пустой — похоже, игра в полноэкранном режиме. Переключи игру в «окно без рамки».", "The capture is empty — the game seems to be in fullscreen mode. Switch the game to borderless window.", "La captura está vacía: parece que el juego está en pantalla completa. Cambia el juego a ventana sin bordes."},
	"zn.blankTitle": {"Карточка зоны: снимок пустой", "Zone card: empty capture", "Tarjeta de zona: captura vacía"},
	"zn.noRef":      {"Справочник зон не прочитался", "Zone reference failed to load", "No se pudo leer el catálogo de zonas"},
	"zn.doubt":      {"Похоже на эту, но уверенности нет — рядом %s", "Looks like this one, but not sure — %s is close", "Parece esta, pero sin certeza — %s está cerca"},
	"zn.points":     {"точки", "spots", "puntos"},
	"zn.biome":      {"ресурсы", "resources", "recursos"},
	"zn.mainRes":    {"основной ресурс биома", "the biome's main resource", "recurso principal del bioma"},
	"zn.nodes":      {"на них", "on them", "en ellos"},
	"zn.camps":      {"лагеря", "camps", "campamentos"},
	"zn.dungeons":   {"данжи", "dungeons", "mazmorras"},
	"zn.mists":      {"есть выход в город туманов", "exit to the Mists city", "salida a la ciudad de las Nieblas"},
	"zn.portal":     {"портал на %s", "portal for %s", "portal para %s"},
	"zn.closes":     {"закроется через %s", "closes in %s", "cierra en %s"},
	"zn.closed":     {"портал уже закрылся", "the portal has closed", "el portal ya se cerró"},
	"t.hm":          {"%s ч %s м", "%s h %s m", "%s h %s min"},
	"zn.none":       {"нет", "none", "no hay"},
	"zn.risk":       {"Чёрный экран был в %s из %s твоих переходов в эту зону", "Black screen in %s of your %s moves into this zone", "Pantalla negra en %s de tus %s pasos a esta zona"},
	"zn.riskNote":   {"по твоей истории переходов — это вероятность, не гарантия", "from your own move history — a probability, not a guarantee", "según tu historial de pasos: es una probabilidad, no una garantía"},
	"zn.privacy":    {"Снимок области у курсора остаётся на компьютере: текст читает OCR Windows, в интернет картинка не уходит.", "The capture of the area around the cursor stays on your computer: Windows OCR reads the text, the image never goes online.", "La captura alrededor del cursor se queda en tu equipo: el OCR de Windows lee el texto y la imagen no sale a internet."},

	// подсказка о языках OCR Windows
	"ocr.hint.none":  {"Распознавать нечем: в Windows нет ни русского, ни английского распознавания текста. Параметры → Время и язык → Язык и регион → «Добавить язык» → Русский (и/или English) — галочка «Базовый ввод»/«Оптическое распознавание символов». Потом перезапусти программу.", "Nothing to read with: Windows has neither Russian nor English text recognition. Settings → Time & language → Language & region → “Add a language” → English (and/or Russian) with “Basic typing”/“Optical character recognition”. Then restart the app.", "No hay con qué leer: Windows no tiene reconocimiento de texto ni en ruso ni en inglés. Configuración → Hora e idioma → Idioma y región → «Agregar un idioma» → Inglés (y/o ruso) con «Escritura básica»/«Reconocimiento óptico de caracteres». Luego reinicia la app."},
	"ocr.hint.noRu":  {"Нет русского распознавания текста: тултип русского клиента игры не прочитается. Установи языковой пакет русского (распознавание текста): Параметры → Время и язык → Язык и регион → «Добавить язык» → Русский. Английский клиент читается и так.", "No Russian text recognition: the tooltip of a Russian game client won't be read. Install the Russian language pack (text recognition): Settings → Time & language → Language & region → “Add a language” → Russian. An English client is read as is.", "No hay reconocimiento de texto en ruso: el tooltip del cliente ruso no se leerá. Instala el paquete de idioma ruso (reconocimiento de texto): Configuración → Hora e idioma → Idioma y región → «Agregar un idioma» → Ruso. El cliente en inglés se lee igual."},
	"ocr.hint.noEn":  {"Нет английского распознавания текста: тултип английского клиента игры не прочитается. Параметры → Время и язык → Язык и регион → «Добавить язык» → English (United States). Русский клиент читается и так.", "No English text recognition: the tooltip of an English game client won't be read. Settings → Time & language → Language & region → “Add a language” → English (United States). A Russian client is read as is.", "No hay reconocimiento de texto en inglés: el tooltip del cliente en inglés no se leerá. Configuración → Hora e idioma → Idioma y región → «Agregar un idioma» → English (United States). El cliente ruso se lee igual."},
	"ocr.hint.check": {"Не удалось проверить языки распознавания текста Windows — карточка попробует русский и английский наудачу. Подробности в журнале.", "Couldn't check the Windows text recognition languages — the card will try Russian and English anyway. Details are in the log.", "No se pudieron comprobar los idiomas de reconocimiento de Windows: la tarjeta probará ruso e inglés de todos modos. Detalles en el registro."},

	// отчёт портала карточки на карту
	"map.why.noTime":    {"🗺 на карту не отправлено: нет времени закрытия", "🗺 not sent to the map: no closing time", "🗺 no enviado al mapa: sin hora de cierre"},
	"map.why.noPlace":   {"🗺 не знаю, где ты — перейди в другую локацию, программа узнает", "🗺 I don't know where you are — move to another location and the app will know", "🗺 no sé dónde estás: cambia de ubicación y la app lo sabrá"},
	"map.why.doubt":     {"🗺 не отправлено: название зоны прочиталось неуверенно", "🗺 not sent: the zone name was read with low confidence", "🗺 no enviado: el nombre de la zona no se leyó con seguridad"},
	"map.why.notRoad":   {"🗺 не отправлено: это не дороги Авалона", "🗺 not sent: this is not the Roads of Avalon", "🗺 no enviado: no son los Caminos de Avalon"},
	"map.why.same":      {"🗺 не отправлено: портал ведёт в эту же зону", "🗺 not sent: the portal leads to this same zone", "🗺 no enviado: el portal lleva a esta misma zona"},
	"map.why.notEurope": {"🗺 карта Авалона пока только для европейского сервера", "🗺 the Avalon map is for the Europe server only for now", "🗺 por ahora el mapa de Avalon es solo para el servidor de Europa"},

	// легенда значков (zoneLegend у мака)
	"lg.title":  {"Что значат значки", "What the icons mean", "Qué significan los iconos"},
	"lg.chests": {"Сундуки — они же лагеря: сундук бывает только в лагере", "Chests — same as camps: a chest only comes from a camp", "Cofres — o sea campamentos: un cofre solo sale de un campamento"},
	"lg.zones":  {"Цвет зоны", "Zone colour", "Color de la zona"},
	"lg.biome":  {"В открытом мире ресурсы задаёт биом зоны: один основной (подсвечен золотом) и два попутных. Сколько там узлов, данные не говорят — только какие.", "In the open world the biome sets the resources: one main (highlighted gold) and two side ones. The data says which, not how many.", "En el mundo abierto el bioma define los recursos: uno principal (en dorado) y dos secundarios. Los datos dicen cuáles, no cuántos."},
	"lg.res":    {"Ресурсы показаны значками предметов: под каждым — разброс тиров узлов и сколько их всего в зоне.", "Resources use item icons: under each one is the tier range and how many nodes the zone has.", "Los recursos usan iconos de objeto: debajo va el rango de niveles y cuántos nodos hay en la zona."},
	"lg.camps":  {"Большой соло-лагерь даёт сразу четыре малых сундука, групповой — средний ветеранский, рейдовый — элитный.", "A big solo camp gives four small chests at once, a group camp a medium veteran, a raid camp an elite one.", "Un campamento solo grande da cuatro cofres pequeños; uno grupal, un mediano veterano; uno de incursión, un élite."},

	// настройки карточки
	"sec.zone":           {"КАРТОЧКА ЗОНЫ", "ZONE CARD", "TARJETA DE ZONA"},
	"set.hotkey":         {"Кнопка карточки зоны", "Zone card button", "Botón de la tarjeta de zona"},
	"key.mouse4":         {"мышь 4 (назад)", "mouse 4 (back)", "ratón 4 (atrás)"},
	"key.mouse5":         {"мышь 5 (вперёд)", "mouse 5 (forward)", "ratón 5 (adelante)"},
	"key.off":            {"выключена", "off", "desactivado"},
	"key.mouse3":         {"средняя кнопка мыши", "middle mouse button", "botón central del ratón"},
	"key.set":            {"Назначить", "Set", "Asignar"},
	"key.cancel":         {"Отмена", "Cancel", "Cancelar"},
	"key.turnOff":        {"Выключить", "Turn off", "Desactivar"},
	"key.waiting":        {"нажми кнопку…", "press a button…", "pulsa un botón…"},
	"key.recHint":        {"Нажми среднюю или боковую кнопку мыши, сочетание с Ctrl, Alt или Win, или F1–F24 (можно и с Shift), Insert, Page Up/Down, Pause, Scroll Lock. Можно не из окна программы. Esc — отмена.", "Press the middle or a side mouse button, a combo with Ctrl, Alt or Win, or F1–F24 (Shift works too), Insert, Page Up/Down, Pause, Scroll Lock. Works outside the app window too. Esc cancels.", "Pulsa el botón central o uno lateral del ratón, una combinación con Ctrl, Alt o Win, o F1–F24 (también con Shift), Insert, Re Pág/Av Pág, Pausa, Bloq Despl. Funciona también fuera de la ventana. Esc cancela."},
	"key.needMod":        {"Нужен Ctrl, Alt или Win: голая клавиша сработает в чате игры, а с одним Shift это просто заглавная буква", "Needs Ctrl, Alt or Win: a bare key would fire in the game chat, and with Shift alone it is just a capital letter", "Hace falta Ctrl, Alt o Win: una tecla sola saltaría en el chat del juego, y con solo Shift es una mayúscula"},
	"key.timeout":        {"Кнопку не нажали за 10 секунд — осталась прежняя.", "No button pressed in 10 seconds — the old one stays.", "No se pulsó nada en 10 segundos: se queda el anterior."},
	"key.failed":         {"Не удалось записать кнопку: %s", "Couldn't record the button: %s", "No se pudo grabar el botón: %s"},
	"key.unsupported":    {"Записать кнопку можно только в Windows.", "Recording a button works only on Windows.", "Grabar un botón solo funciona en Windows."},
	"set.hotkeyHint":     {"Наведи курсор на портал дорог в игре и нажми кнопку: программа снимет область у курсора, прочитает тултип и покажет карточку во вкладке «Зона». Нажатие уходит в игру как обычно. Поверх игры рисуется только панель — если выбрать её ниже.", "Point at a roads portal in game and press the button: the app captures the area around the cursor, reads the tooltip and shows the card in the Zone tab. The press still reaches the game. Nothing is drawn over the game unless you pick the panel below.", "Apunta a un portal de los caminos y pulsa el botón: la app captura la zona del cursor, lee el tooltip y muestra la tarjeta en la pestaña Zona. El juego recibe la pulsación igual. No se dibuja nada sobre el juego salvo el panel, si lo eliges abajo."},
	"set.zoneShow":       {"Способ показа", "How to show it", "Cómo mostrarla"},
	"show.notify":        {"Уведомление", "Notification", "Notificación"},
	"show.panel":         {"Панель поверх игры", "Panel over the game", "Panel sobre el juego"},
	"show.off":           {"Выключено", "Off", "Desactivado"},
	"show.notifyHint":    {"Баннер рисует сама Windows, как уведомление любой программы, — не мы поверх игры. В полноэкранной игре Windows может прятать уведомления («Не беспокоить» во время игры) — тогда карточка только во вкладке.", "Windows draws the banner like any app's notification — not us over the game. In a fullscreen game Windows may hold notifications back (“Do not disturb” while gaming) — then the card is only in the tab.", "El aviso lo dibuja Windows, como el de cualquier app, no nosotros sobre el juego. En un juego a pantalla completa Windows puede ocultarlos («No molestar» al jugar): entonces la tarjeta solo está en la pestaña."},
	"show.panelHint":     {"Маленькая панель в углу экрана с игрой на несколько секунд. Фокус не забирает, клики проходят в игру. Видна, когда игра в окне или в «окне без рамки»; в полноэкранном (эксклюзивном) режиме Windows отдаёт экран игре целиком и панели не видно — переключи игру в «окно без рамки».", "A small panel in a corner of the game's screen for a few seconds. It doesn't take focus, clicks go through to the game. Visible when the game runs windowed or borderless; in exclusive fullscreen Windows gives the whole screen to the game and the panel can't be seen — switch the game to borderless window.", "Un panel pequeño en una esquina de la pantalla del juego durante unos segundos. No quita el foco y los clics pasan al juego. Se ve con el juego en ventana o en ventana sin bordes; en pantalla completa exclusiva Windows da toda la pantalla al juego y el panel no se ve: cambia el juego a ventana sin bordes."},
	"show.offHint":       {"Карточка только во вкладке «Зона».", "The card is only in the Zone tab.", "La tarjeta solo está en la pestaña Zona."},
	"set.corner":         {"Угол экрана", "Screen corner", "Esquina de la pantalla"},
	"corner.topLeft":     {"слева сверху", "top left", "arriba a la izquierda"},
	"corner.topRight":    {"справа сверху", "top right", "arriba a la derecha"},
	"corner.bottomLeft":  {"слева снизу", "bottom left", "abajo a la izquierda"},
	"corner.bottomRight": {"справа снизу", "bottom right", "abajo a la derecha"},
	"set.overlaySec":     {"Секунд на экране", "Seconds on screen", "Segundos en pantalla"},
	"nt.what":            {"Что показывать", "What to show", "Qué mostrar"},
	"nt.chestsFirst":     {"сундуки сверху", "chests first", "cofres primero"},
	"nt.resFirst":        {"ресурсы сверху", "resources first", "recursos primero"},
	"nt.chests":          {"Сундуки", "Chests", "Cofres"},
	"nt.res":             {"Ресурсы", "Resources", "Recursos"},
	"nt.dng":             {"Подземелья", "Dungeons", "Mazmorras"},
	"nt.portal":          {"Портал и время", "Portal and time", "Portal y tiempo"},
	"set.blackWarn":      {"Предупреждать о чёрном экране", "Warn about the black screen", "Avisar de la pantalla negra"},
	"set.blackWarnHint":  {"Новый сервер не ответил за 2 с после перехода — уведомление «Сервер зоны не отвечает». В карточке портала — сколько раз чёрный экран был в твоих переходах в эту зону (от трёх переходов).", "The new server hasn't answered 2 s after a zone change — a “Zone server not responding” notification. The portal card shows how often you got a black screen moving into that zone (from three moves on).", "Si el nuevo servidor no responde 2 s después de cambiar de zona, aviso «El servidor de la zona no responde». La tarjeta del portal muestra cuántas veces tuviste pantalla negra al entrar en esa zona (a partir de tres pasos)."},

	// уведомление о чёрном экране (Go)
	"msg.blackTitle": {"Сервер зоны не отвечает", "Zone server not responding", "El servidor de la zona no responde"},
	"msg.blackBody":  {"Похоже на чёрный экран: новый сервер молчит после подключения.", "Looks like a black screen: the new server is silent after connecting.", "Parece pantalla negra: el nuevo servidor no responde tras conectar."},
	"msg.blackFrom":  {"Похоже на чёрный экран: новый сервер молчит после перехода из %s.", "Looks like a black screen: the new server is silent after leaving %s.", "Parece pantalla negra: el nuevo servidor no responde tras salir de %s."},
}
