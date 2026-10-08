package ui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"albionzonefix/internal/app"
	"albionzonefix/internal/collector"
	"albionzonefix/internal/i18n"
	"albionzonefix/internal/ownprices"
	"albionzonefix/internal/settings"
	"albionzonefix/internal/update"
)

type fakeCol struct {
	cfg    collector.Config
	resets int
}

func (f *fakeCol) Apply(c collector.Config) error { f.cfg = c; return nil }
func (f *fakeCol) ResetSession()                  { f.resets++ }
func (f *fakeCol) Stats() collector.Stats {
	return collector.Stats{Running: f.cfg.Running(), Session: f.cfg.Running() && f.cfg.Session}
}

type env struct {
	srv    *Server
	a      *app.App
	col    *fakeCol
	dir    string
	hooks  []settings.Settings
	opened []string
	shown  int
	maps   int
}

func start(t *testing.T, opts ...func(*Options)) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{dir: dir, col: &fakeCol{}}
	// Установка прошлой версии: сбор цен, ADP и счётчик включены (новая
	// установка начинает только с Авалона — TestNewInstallOnlyAvalon).
	os.WriteFile(filepath.Join(dir, settings.FileName), []byte(`{"shareADP":true,"sessionStats":true,"collectOnStart":true}`), 0644)
	e.a = app.New(dir, dir, nil)
	e.a.AttachCollector(e.col)
	o := Options{
		DataDir: dir, LogPath: filepath.Join(dir, "log"), SessionFile: filepath.Join(dir, collector.SessionFileName),
		ReceiverAddr: "127.0.0.1:1", // никто не слушает
		Lang:         func() string { return "ru-RU" },
		OnSettings:   func(old, cur settings.Settings) error { e.hooks = append(e.hooks, cur); return nil },
		OpenURL:      func(u string) { e.opened = append(e.opened, u) },
		OpenFolder:   func(d string) { e.opened = append(e.opened, d) },
		OnShow:       func() { e.shown++ },
		OpenMap:      func() { e.maps++ },
	}
	for _, f := range opts {
		f(&o)
	}
	srv, err := Start(e.a, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	e.srv = srv
	return e
}

func (e *env) get(t *testing.T, path string, v any) *http.Response {
	t.Helper()
	r, err := http.Get(e.srv.URL + strings.TrimPrefix(path, "/"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if v != nil {
		if err := json.NewDecoder(r.Body).Decode(v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	return r
}

func (e *env) post(t *testing.T, path string, form url.Values, token bool) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+strings.TrimPrefix(path, "/"), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token {
		req.Header.Set(TokenHeader, e.srv.Token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	return r
}

func TestPageHasTokenAndAssets(t *testing.T) {
	e := start(t)
	r, _ := http.Get(e.srv.URL)
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), `content="`+e.srv.Token+`"`) || strings.Contains(string(b), "{{TOKEN}}") {
		t.Fatal("ключ страницы не подставлен")
	}
	for _, p := range []string{"app.js", "style.css", "rabbit.gif", "rabbit.png", "fonts/PixelifySans-Regular.ttf", "fonts/PixelifySans-SemiBold.ttf"} {
		if r := e.get(t, p, nil); r.StatusCode != 200 {
			t.Fatalf("%s: %d", p, r.StatusCode)
		}
	}
}

func TestPostNeedsToken(t *testing.T) {
	e := start(t)
	if r := e.post(t, "/api/collect", url.Values{"on": {"0"}}, false); r.StatusCode != 403 {
		t.Fatalf("без ключа: %d", r.StatusCode)
	}
	if !e.a.Collecting() {
		t.Fatal("запрос без ключа сработал")
	}
	if r := e.post(t, "/api/collect", url.Values{"on": {"0"}}, true); r.StatusCode != 200 || e.a.Collecting() || e.col.cfg.Prices {
		t.Fatalf("с ключом сбор не остановился: %d", r.StatusCode)
	}
	e.post(t, "/api/collect", url.Values{"on": {"1"}}, true)
	if !e.a.Collecting() || !e.col.cfg.Prices {
		t.Fatal("сбор не запустился")
	}
}

func TestForeignHostRejected(t *testing.T) {
	e := start(t)
	req, _ := http.NewRequest("GET", e.srv.URL+"api/state", nil)
	req.Host = "evil.example:80"
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatalf("чужой Host: %d", r.StatusCode)
	}
}

func TestStateAndLanguage(t *testing.T) {
	e := start(t)
	var st map[string]any
	e.get(t, "/api/state", &st)
	if st["lang"] != "ru" || st["receiver"] != false || st["siteReady"] != false || st["collecting"] != true {
		t.Fatalf("состояние: lang=%v receiver=%v site=%v collecting=%v", st["lang"], st["receiver"], st["siteReady"], st["collecting"])
	}
	col := st["collector"].(map[string]any)
	if col["running"] != true || col["session"] != true {
		t.Fatalf("collector: %v", col)
	}
	var tr struct {
		Lang string            `json:"lang"`
		T    map[string]string `json:"t"`
	}
	e.get(t, "/api/i18n?lang=es", &tr)
	if tr.Lang != "es" || tr.T["tab.zonefix"] != "Cambios de zona" {
		t.Fatalf("%+v", tr.Lang)
	}
	e.get(t, "/api/i18n", &tr)
	if tr.Lang != "ru" {
		t.Fatal("без lang — язык программы")
	}
}

func postJSON(t *testing.T, e *env, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"api/settings", strings.NewReader(body))
	req.Header.Set(TokenHeader, e.srv.Token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var out map[string]any
	json.NewDecoder(r.Body).Decode(&out)
	return r.StatusCode, out
}

func TestSettingsPatch(t *testing.T) {
	e := start(t)
	code, out := postJSON(t, e, `{"sessionStats":false,"language":"en"}`)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	s := e.a.Settings()
	if s.SessionStats || s.Language != "en" || !s.ShareADP || !s.CollectOnStart {
		t.Fatalf("частичное обновление испортило настройки: %+v", s)
	}
	if e.col.cfg.Session || !e.col.cfg.Prices {
		t.Fatalf("счётчик не выключился в разборщике: %+v", e.col.cfg)
	}
	if len(e.hooks) != 1 || e.hooks[0].Language != "en" {
		t.Fatalf("хук настроек: %+v", e.hooks)
	}
	var st map[string]any
	e.get(t, "/api/state", &st)
	if st["lang"] != "en" {
		t.Fatal("язык из настроек не применился")
	}
	// перечитывается из файла
	if settings.Open(e.dir).Get().Language != "en" {
		t.Fatal("не сохранено")
	}
	if code, _ := postJSON(t, e, `{не json`); code != 400 {
		t.Fatal("битый JSON принят")
	}
}

func TestSettingsHookErrorReported(t *testing.T) {
	e := start(t)
	e.srv.o.OnSettings = func(old, cur settings.Settings) error { return os.ErrPermission }
	code, out := postJSON(t, e, `{"startWithWindows":true}`)
	if code != 400 || out["error"] == nil || out["settings"] == nil {
		t.Fatalf("%d %v", code, out)
	}
}

func TestBypassGatedBySetting(t *testing.T) {
	e := start(t)
	if r := e.post(t, "/api/bypass", url.Values{"mode": {"auto"}}, true); r.StatusCode != 400 {
		t.Fatalf("встроенный обход по умолчанию выключен, а запрос прошёл: %d", r.StatusCode)
	}
	if r := e.post(t, "/api/bypass", url.Values{"mode": {"off"}}, true); r.StatusCode != 200 {
		t.Fatal("выключить можно всегда")
	}
}

func TestOwnPrices(t *testing.T) {
	e := start(t)
	var o ownprices.Summary
	e.get(t, "/api/own", &o)
	if o.Exists {
		t.Fatal("базы нет")
	}
	os.WriteFile(filepath.Join(e.dir, ownprices.FileName), []byte(`{"prices":{"T4_BAG|Lymhurst|1":{"sell":1200,"sellTs":100}},"seenOrders":7}`), 0644)
	e.get(t, "/api/own", &o)
	if !o.Exists || o.Positions != 1 || o.Orders != 7 || o.Recent[0].Name != "T4_BAG" {
		t.Fatalf("%+v", o)
	}
}

func TestSessionEndpoint(t *testing.T) {
	e := start(t)
	var r SessionReply
	e.get(t, "/api/session", &r)
	if !r.Enabled || !r.Running || r.Exists {
		t.Fatalf("%+v", r)
	}
	os.WriteFile(filepath.Join(e.dir, collector.SessionFileName), []byte(`{"fame":1500,"fighters":[{"name":"Me","damage":10}]}`), 0644)
	e.get(t, "/api/session", &r)
	var data struct{ Fame float64 }
	json.Unmarshal(r.Data, &data)
	if !r.Exists || data.Fame != 1500 {
		t.Fatalf("%+v", r)
	}
	os.WriteFile(filepath.Join(e.dir, collector.SessionFileName), []byte(`{битый`), 0644)
	e.get(t, "/api/session", &r)
	if r.Exists || r.Error == "" {
		t.Fatalf("битый файл: %+v", r)
	}
	if e.post(t, "/api/session/reset", nil, true).StatusCode != 200 || e.col.resets != 1 {
		t.Fatal("сброс")
	}
}

func TestOpenAndShow(t *testing.T) {
	e := start(t)
	e.post(t, "/api/open", url.Values{"what": {"site"}}, true)
	e.post(t, "/api/open", url.Values{"what": {"data"}}, true)
	if r := e.post(t, "/api/open", url.Values{"what": {"x"}}, true); r.StatusCode != 400 {
		t.Fatal("неизвестное открылось")
	}
	if len(e.opened) != 2 || e.opened[0] != "http://localhost:7777/" || e.opened[1] != e.dir {
		t.Fatalf("%v", e.opened)
	}
	e.post(t, "/api/show", nil, true)
	if e.shown != 1 {
		t.Fatal("show")
	}
	if r := e.post(t, "/api/open", url.Values{"what": {"map"}}, true); r.StatusCode != 200 || e.maps != 1 {
		t.Fatal("карта не открылась")
	}
}

func TestMapSettingAndInstallNotFromPage(t *testing.T) {
	e := start(t)
	id := e.a.MapInstall()
	if !e.a.Settings().MapSend {
		t.Fatal("отправка на карту по умолчанию включена")
	}
	if code, out := postJSON(t, e, `{"mapSend":false,"mapInstall":"подмена"}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	s := e.a.Settings()
	if s.MapSend || s.MapInstall != id {
		t.Fatalf("%+v", s)
	}
	var st map[string]any
	e.get(t, "/api/state", &st)
	if _, ok := st["here"]; ok {
		t.Fatal("зоны ещё нет, а here есть")
	}
}

// Каждая надпись, которую просит страница, есть в словаре, и в словаре нет
// забытых ключей (кроме тех, что берёт Go: трей и сообщения).
func TestPageKeysInDictionary(t *testing.T) {
	html, _ := webFS.ReadFile("web/index.html")
	js, _ := webFS.ReadFile("web/app.js")
	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`data-t(?:itle)?="([^"]+)"`).FindAllStringSubmatch(string(html), -1) {
		used[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`\bt\('([a-zA-Z]+\.[a-zA-Z0-9]+)'`).FindAllStringSubmatch(string(js), -1) {
		used[m[1]] = true
	}
	// ключи, собранные в JS из частей или переданные через переменную
	for _, k := range []string{"ago.min", "ago.hour", "ago.day", "st.on", "st.off", "st.ready", "st.loading", "btn.stop", "btn.start", "btn.startCollect",
		"btn.startFame", "own.hintUp", "own.hintDown", "sh.failed", "sh.loading", "sh.local", "sh.remote", "se.noDamageUp", "se.noDamageDown",
		"btn.copied", "btn.copy", "zf.recStop", "zf.rec", "set.shareOn", "set.shareOff", "sup.copied",
		// строки состояния «Авалон», «выключен» и способ показа карточки
		"st.watching", "st.watchingNoMap", "st.avalonNoCapture", "st.waitingGame", "st.disabled", "btn.enable",
		"show.notifyHint", "show.panelHint", "show.offHint",
		// вкладка «Зона»: типы зон и серверы — через таблицы KIND и REGION
		"kind.roads", "kind.black", "kind.red", "kind.yellow", "kind.safe", "kind.city", "kind.island", "kind.instance", "kind.other",
		"rg.europe", "rg.americas", "rg.asia",
		// заголовок окна карты (Go, internal/desktop)
		"map.window",
		// карточка зоны: подпись дороги, собранная из условия
		"zn.nodes"} {
		used[k] = true
	}
	// Карточка зоны: ключи собираются из кодов справочника и итогов
	// (q.<качество>, res.<ресурс>, map.why.<причина>…); их же берёт Go для
	// уведомления (internal/zonecard).
	for k := range i18n.Table {
		for _, pre := range []string{"q.", "type.", "res.", "camp.", "chest.", "dng.", "map.why.", "ocr.hint."} {
			if strings.HasPrefix(k, pre) {
				used[k] = true
			}
		}
	}
	var missing, unused []string
	for k := range used {
		if _, ok := i18n.Table[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range i18n.Table {
		if !used[k] && !strings.HasPrefix(k, "tray.") && !strings.HasPrefix(k, "msg.") {
			unused = append(unused, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(unused)
	if len(missing) > 0 {
		t.Errorf("нет в словаре: %v", missing)
	}
	if len(unused) > 0 {
		t.Errorf("в словаре, но страница не берёт: %v", unused)
	}
}

type fakeRcv struct {
	on  bool
	err string
}

func (f *fakeRcv) Start() error { f.on = true; return nil }
func (f *fakeRcv) Stop()        { f.on = false }
func (f *fakeRcv) Up() bool     { return f.on }
func (f *fakeRcv) Err() string  { return f.err }
func (f *fakeRcv) Keep(bool)    {}

func TestReceiverEndpointAndError(t *testing.T) {
	e := start(t)
	r := &fakeRcv{err: "порт 7777 занят другой программой"}
	e.a.AttachReceiver(r)
	if resp := e.post(t, "/api/receiver", url.Values{"on": {"1"}}, false); resp.StatusCode != 403 {
		t.Fatalf("без ключа: %d", resp.StatusCode)
	}
	if resp := e.post(t, "/api/receiver", url.Values{"on": {"1"}}, true); resp.StatusCode != 200 || !r.on {
		t.Fatalf("запуск: %d %v", resp.StatusCode, r.on)
	}
	if resp := e.post(t, "/api/receiver", url.Values{"on": {"0"}}, true); resp.StatusCode != 200 || r.on {
		t.Fatalf("остановка: %d %v", resp.StatusCode, r.on)
	}
	var st map[string]any
	e.get(t, "/api/state", &st)
	if st["receiverError"] != "порт 7777 занят другой программой" {
		t.Fatalf("причина в состоянии: %v", st["receiverError"])
	}
}

type fakeUpd struct {
	st     update.Status
	checks int
}

func (f *fakeUpd) Status() update.Status { return f.st }
func (f *fakeUpd) Check()                { f.checks++ }

func TestUpdateAndSupport(t *testing.T) {
	dir := t.TempDir()
	a := app.New(dir, dir, nil)
	u := &fakeUpd{st: update.Status{State: update.StateReady, Current: "1.0.0", Version: "1.0.1",
		Ready: &update.Release{Version: "1.0.1", Notes: "что нового"}}}
	restarts := 0
	var restartErr error
	var opened []string
	srv, err := Start(a, Options{
		DataDir: dir, ReceiverAddr: "127.0.0.1:1", Lang: func() string { return "ru" },
		Version: "1.0.0", Update: u,
		OnUpdateRestart: func() error { restarts++; return restartErr },
		SupportInfo:     func() string { return "Albion Journal 1.0.0" },
		OpenURL:         func(s string) { opened = append(opened, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	e := &env{srv: srv, a: a, dir: dir}

	var st PageState
	e.get(t, "/api/state", &st)
	if st.Version != "1.0.0" || st.Update == nil || st.Update.Ready == nil || st.Update.Ready.Notes != "что нового" {
		t.Fatalf("%+v", st.Update)
	}
	if e.post(t, "/api/update/check", nil, false).StatusCode != 403 || u.checks != 0 {
		t.Fatal("проверка без ключа")
	}
	if e.post(t, "/api/update/check", nil, true).StatusCode != 200 || u.checks != 1 {
		t.Fatal("проверка")
	}
	if e.post(t, "/api/update/restart", nil, false).StatusCode != 403 || restarts != 0 {
		t.Fatal("перезапуск без ключа")
	}
	if e.post(t, "/api/update/restart", nil, true).StatusCode != 200 || restarts != 1 {
		t.Fatal("перезапуск")
	}
	restartErr = errors.New("нет прав")
	if e.post(t, "/api/update/restart", nil, true).StatusCode != 400 {
		t.Fatal("ошибка установки должна дойти до страницы")
	}
	e.post(t, "/api/open", url.Values{"what": {"discord"}}, true)
	if len(opened) != 1 || opened[0] != "https://discord.gg/5pV9ZqZMve" {
		t.Fatalf("%v", opened)
	}
	// Сведения — только с ключом (в них хвост журнала).
	if e.post(t, "/api/support", nil, false).StatusCode != 403 {
		t.Fatal("сведения без ключа")
	}
	req, _ := http.NewRequest("POST", srv.URL+"api/support", nil)
	req.Header.Set(TokenHeader, srv.Token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sup map[string]string
	json.NewDecoder(r.Body).Decode(&sup)
	r.Body.Close()
	if sup["text"] != "Albion Journal 1.0.0" {
		t.Fatalf("%v", sup)
	}
	if r, _ := http.Get(srv.URL + "api/support"); r.StatusCode == 200 {
		t.Fatal("GET сведений не должен работать")
	}
}

func TestNoUpdater(t *testing.T) {
	e := start(t)
	var st PageState
	e.get(t, "/api/state", &st)
	if st.Update != nil {
		t.Fatal("без обновлятеля — нет состояния")
	}
	if e.post(t, "/api/update/check", nil, true).StatusCode != 400 || e.post(t, "/api/update/restart", nil, true).StatusCode != 400 {
		t.Fatal("без обновлятеля — ошибка")
	}
}

func TestZoneCardSettingsAndState(t *testing.T) {
	e := start(t)
	if code, out := postJSON(t, e, `{"zoneKey":"F5","notifyOrder":"resourcesFirst","notifyDng":false,"blackWarn":false}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	s := e.a.Settings()
	if s.ZoneKey != "f5" || s.NotifyOrder != "resourcesFirst" || s.NotifyDng || s.BlackWarn || s.ZoneShow != settings.ShowNotify || !s.ZoneNotify {
		t.Fatalf("%+v", s)
	}
	// Незнакомая кнопка и порядок — по умолчанию, а не что прислали.
	postJSON(t, e, `{"zoneKey":"mouse9","notifyOrder":"???"}`)
	if s := e.a.Settings(); s.ZoneKey != "xbutton1" || s.NotifyOrder != "chestsFirst" {
		t.Fatalf("%+v", s)
	}
	var st map[string]any
	e.get(t, "/api/state", &st)
	c, ok := st["card"].(map[string]any)
	if !ok || c["busy"] != false {
		t.Fatalf("карточка в состоянии: %v", st["card"])
	}
}

// Оформление и кролик — сразу в разметке; переключение — через настройки,
// без перезапуска (страница перечитывает их раз в две секунды).
func TestPageSkinAndLogo(t *testing.T) {
	e := start(t)
	page := func() string {
		r, _ := http.Get(e.srv.URL)
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return string(b)
	}
	p := page()
	if !strings.Contains(p, `data-skin="plain"`) || !strings.Contains(p, `src="rabbit.gif"`) || strings.Contains(p, "{{") {
		t.Fatal("по умолчанию — обычное оформление и живой кролик")
	}
	if code, out := postJSON(t, e, `{"skin":"pixel","logoAnim":false,"resetOnZone":true,"showWithGame":true,"startWithGame":true,"quitWithGame":true}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	p = page()
	if !strings.Contains(p, `data-skin="pixel"`) || !strings.Contains(p, `src="rabbit.png"`) {
		t.Fatal("пиксельное оформление и значок вместо гифки не подставлены")
	}
	s := e.a.Settings()
	if !s.ResetOnZone || !s.ShowWithGame || !s.StartWithGame || !s.QuitWithGame {
		t.Fatalf("%+v", s)
	}
	if code, _ := postJSON(t, e, `{"skin":"<script>"}`); code != 200 || e.a.Settings().Skin != settings.SkinPlain {
		t.Fatal("незнакомое оформление должно стать обычным")
	}
}

// Файл для форка не записался — страница получает «сохранено», хук
// настроек (автозапуск и прочее) всё равно вызван.
func TestSettingsSavedWhenForkOptionsFail(t *testing.T) {
	e := start(t)
	path := filepath.Join(e.dir, collector.OptionsFileName)
	os.Remove(path)
	os.MkdirAll(filepath.Join(path, "x"), 0755)
	code, out := postJSON(t, e, `{"resetOnZone":true,"startWithWindows":true}`)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if len(e.hooks) != 1 || !e.hooks[0].ResetOnZone || !e.hooks[0].StartWithWindows {
		t.Fatalf("хук настроек: %+v", e.hooks)
	}
	if !settings.Open(e.dir).Get().ResetOnZone {
		t.Fatal("не сохранено")
	}
}

func TestWindowHiddenInState(t *testing.T) {
	dir := t.TempDir()
	a := app.New(dir, dir, nil)
	hidden := true
	srv, err := Start(a, Options{DataDir: dir, ReceiverAddr: "127.0.0.1:1", WindowHidden: func() bool { return hidden }})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	r, _ := http.Get(srv.URL + "api/state")
	var st map[string]any
	json.NewDecoder(r.Body).Decode(&st)
	r.Body.Close()
	if st["windowHidden"] != true {
		t.Fatalf("windowHidden: %v", st["windowHidden"])
	}
	js, _ := webFS.ReadFile("web/app.js")
	if !strings.Contains(string(js), "window.ajVisible") {
		t.Fatal("страница не принимает сигнал видимости окна")
	}
}

// Способ показа карточки, угол и секунды панели — с проверкой значений.
func TestZoneShowSettings(t *testing.T) {
	e := start(t)
	if code, out := postJSON(t, e, `{"zoneShow":"panel","zoneOverlayCorner":"bottomLeft","zoneOverlaySec":99}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	s := e.a.Settings()
	if s.ZoneShow != settings.ShowPanel || s.ZoneNotify || s.ZoneOverlayCorner != settings.CornerBottomLeft || s.ZoneOverlaySec != settings.OverlaySecMax {
		t.Fatalf("%+v", s)
	}
	postJSON(t, e, `{"zoneShow":"popup","zoneOverlayCorner":"center","zoneOverlaySec":8}`)
	if s := e.a.Settings(); s.ZoneShow != settings.ShowNotify || !s.ZoneNotify || s.ZoneOverlayCorner != settings.CornerTopRight || s.ZoneOverlaySec != 8 {
		t.Fatalf("%+v", s)
	}
	postJSON(t, e, `{"zoneShow":"off"}`)
	if s := settings.Open(e.dir).Get(); s.ZoneShow != settings.ShowOff || s.ZoneNotify {
		t.Fatalf("не сохранено: %+v", s)
	}
}

// «Включить» сбор цен на главной запоминается, «Остановить» — нет.
func TestCollectEnableIsSaved(t *testing.T) {
	e := start(t)
	s := e.a.Settings()
	s.CollectOnStart = false
	e.a.SetSettings(s)
	e.post(t, "/api/collect", url.Values{"on": {"0"}}, true)
	e.post(t, "/api/collect", url.Values{"on": {"1"}, "save": {"1"}}, true)
	if !e.a.Collecting() || !e.col.cfg.Prices || !settings.Open(e.dir).Get().CollectOnStart {
		t.Fatalf("сбор %v, при открытии %v", e.a.Collecting(), settings.Open(e.dir).Get().CollectOnStart)
	}
	e.post(t, "/api/collect", url.Values{"on": {"0"}}, true)
	if e.a.Collecting() || !settings.Open(e.dir).Get().CollectOnStart {
		t.Fatal("остановка — только до выхода")
	}
	// Трей и сторож игры по-прежнему без сохранения.
	s = e.a.Settings()
	s.CollectOnStart = false
	e.a.SetSettings(s)
	e.post(t, "/api/collect", url.Values{"on": {"1"}}, true)
	if !e.a.Collecting() || settings.Open(e.dir).Get().CollectOnStart {
		t.Fatal("без save=1 настройка не меняется")
	}
}
