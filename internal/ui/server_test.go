package ui

import (
	"encoding/json"
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
}

func start(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{dir: dir, col: &fakeCol{}}
	e.a = app.New(dir, dir, nil)
	e.a.AttachCollector(e.col)
	srv, err := Start(e.a, Options{
		DataDir: dir, LogPath: filepath.Join(dir, "log"), SessionFile: filepath.Join(dir, collector.SessionFileName),
		ReceiverAddr: "127.0.0.1:1", // никто не слушает
		Lang:         func() string { return "ru-RU" },
		OnSettings:   func(old, cur settings.Settings) error { e.hooks = append(e.hooks, cur); return nil },
		OpenURL:      func(u string) { e.opened = append(e.opened, u) },
		OpenFolder:   func(d string) { e.opened = append(e.opened, d) },
		OnShow:       func() { e.shown++ },
	})
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
	for _, p := range []string{"app.js", "style.css", "rabbit.gif", "fonts/PixelifySans-Regular.ttf", "fonts/PixelifySans-SemiBold.ttf"} {
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
	for _, k := range []string{"ago.min", "ago.hour", "ago.day", "st.on", "st.off", "st.ready", "st.loading", "btn.stop", "btn.startCollect",
		"btn.startFame", "own.hintUp", "own.hintDown", "sh.failed", "sh.loading", "sh.local", "sh.remote", "se.noDamageUp", "se.noDamageDown",
		"btn.copied", "btn.copy", "zf.recStop", "zf.rec", "set.shareOn", "set.shareOff"} {
		used[k] = true
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
