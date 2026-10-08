package avalon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDirectory(t *testing.T) {
	d := Directory()
	if len(d) < 1000 {
		t.Fatalf("в справочнике %d зон", len(d))
	}
	z, ok := Lookup("TNL-001")
	if !ok || !z.Road || z.Kind != "roads" || z.Tier != 4 || z.Name == "" {
		t.Fatalf("TNL-001: %+v %v", z, ok)
	}
	if z, ok := Lookup("0000"); !ok || z.Road || z.Name != "Thetford" || z.Kind != "city" {
		t.Fatalf("0000: %+v", z)
	}
	// Все коды проходят фильтр для JS без потерь (иначе подсветка не та).
	for c := range d {
		if JSCode(c) != c && !strings.Contains(c, "#") {
			t.Errorf("код %q портится фильтром: %q", c, JSCode(c))
		}
	}
}

func TestRegion(t *testing.T) {
	cases := map[string]string{
		"193.169.238.17:5056": "europe",
		"5.188.125.40:5056":   "americas",
		"5.45.187.3:5056":     "asia",
		"193.169.238.1":       "europe",
		"10.0.0.1:5056":       "",
		"":                    "",
		"193.169.2381.1:5056": "",
	}
	for addr, want := range cases {
		if got := Region(addr); got != want {
			t.Errorf("Region(%q)=%q, ждал %q", addr, got, want)
		}
	}
	if !RegionOK("") || !RegionOK("europe") || RegionOK("americas") || RegionOK("asia") {
		t.Fatal("RegionOK")
	}
}

func TestDecide(t *testing.T) {
	eu := func(z string) Place { return Place{Zone: z, Region: "europe"} }
	pp := func(p Place) *Place { return &p }
	cases := []struct {
		name string
		prev *Place
		cur  Place
		want Skip
	}{
		{"первая зона после запуска", nil, eu("TNL-001"), First},
		{"пустая прошлая", &Place{}, eu("TNL-001"), First},
		{"та же зона", pp(eu("TNL-001")), eu("TNL-001"), Same},
		{"дорога → дорога", pp(eu("TNL-001")), eu("TNL-002"), OK},
		{"город → дорога", pp(eu("0000")), eu("TNL-002"), OK},
		{"сервер неизвестен", pp(Place{Zone: "TNL-001"}), Place{Zone: "TNL-002"}, OK},
		{"Америка", pp(Place{"TNL-001", "americas"}), Place{"TNL-002", "americas"}, NotEurope},
		{"Азия, прошлая неизвестна", pp(Place{"TNL-001", ""}), Place{"TNL-002", "asia"}, NotEurope},
		{"сменил сервер", pp(Place{"TNL-001", "americas"}), eu("TNL-002"), MixedRegion},
		{"не дороги", pp(eu("0000")), eu("0004"), NotRoad},
		{"нет в справочнике", pp(eu("TNL-001")), eu("ZZZ-9"), Unknown},
	}
	for _, c := range cases {
		p, got := Decide(c.prev, c.cur)
		if got != c.want {
			t.Errorf("%s: %v, ждал %v", c.name, got, c.want)
		}
		if got == OK && (p.From != c.prev.Zone || p.To != c.cur.Zone || p.Region != c.cur.Region) {
			t.Errorf("%s: проход %+v", c.name, p)
		}
	}
}

func TestBody(t *testing.T) {
	b, _ := json.Marshal(Body(Pass{From: "TNL-001", To: "TNL-002", Region: "europe"}, "abc"))
	if string(b) != `{"kind":"pass","install":"abc","from":"TNL-001","to":"TNL-002","server":"europe"}` {
		t.Fatal(string(b))
	}
	b, _ = json.Marshal(Body(Pass{From: "TNL-001", To: "TNL-002"}, "abc"))
	if strings.Contains(string(b), "server") {
		t.Fatal("пустой сервер не пишем: " + string(b))
	}
}

func TestNewInstall(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := NewInstall(), NewInstall()
	if !re.MatchString(a) || a == b {
		t.Fatal(a, b)
	}
}

func TestJSCodeAndURLs(t *testing.T) {
	cases := map[string]string{
		"TNL-001":        "TNL-001",
		"@ISLAND@a-1":    "@ISLAND@a-1",
		"x');alert(1)//": "xalert1",
		"Тетфорд":        "",
		"a b\"\\'c":      "abc",
	}
	for in, want := range cases {
		if got := JSCode(in); got != want {
			t.Errorf("JSCode(%q)=%q, ждал %q", in, got, want)
		}
	}
	if MapURL("TNL-001") != "https://vanatoliyv.github.io/albion-craft-profit/#map/here=TNL-001" {
		t.Fatal(MapURL("TNL-001"))
	}
	if MapURL("") != "https://vanatoliyv.github.io/albion-craft-profit/#map" || MapURL("'") != MapURL("") {
		t.Fatal(MapURL(""))
	}
	if HereJS("TNL-001") != "window.avalonHere && window.avalonHere('TNL-001')" || HereJS("');") != "" {
		t.Fatal(HereJS("TNL-001"))
	}
}

// fakeMap — сервер карты для тестов (на настоящий ничего не шлём).
type fakeMap struct {
	mu     sync.Mutex
	code   int
	reply  string
	hits   int
	bodies []string
	ctype  string
}

func (f *fakeMap) start(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path != "/roads/report" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		f.hits++
		f.bodies = append(f.bodies, string(b))
		f.ctype = r.Header.Get("Content-Type")
		w.WriteHeader(f.code)
		io.WriteString(w, f.reply)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestSendResults(t *testing.T) {
	cases := []struct {
		code      int
		reply     string
		want, why string
	}{
		{200, `{"ok":true}`, ResOK, ""},
		{429, `{"why":"limit"}`, ResLimit, ""},
		{429, `{"why":"iplive"}`, ResIPLive, ""},
		{429, ``, ResLimit, ""},
		{422, `{"why":"region"}`, ResRegion, ""},
		{422, `{"why":"unknown zone"}`, ResRefused, "unknown zone"},
		{400, `не json`, ResRefused, "400"},
		{503, `{"why":"full"}`, ResRefused, "full"},
		{502, `bad gateway`, ResOffline, ""},
	}
	for _, c := range cases {
		f := &fakeMap{code: c.code, reply: c.reply}
		s := f.start(t)
		res, why := Send(context.Background(), s.Client(), s.URL, Body(Pass{From: "TNL-001", To: "TNL-002", Region: "europe"}, "id"))
		if res != c.want || why != c.why {
			t.Errorf("%d %s: %s %q, ждал %s %q", c.code, c.reply, res, why, c.want, c.why)
		}
		if f.ctype != "application/json" || !strings.Contains(f.bodies[0], `"kind":"pass"`) {
			t.Errorf("запрос: %s %s", f.ctype, f.bodies)
		}
	}
	// Сеть: никто не слушает.
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL
	s.Close()
	if res, _ := Send(context.Background(), http.DefaultClient, url, Report{}); res != ResOffline {
		t.Fatalf("без сети: %s", res)
	}
}

func TestSendTimeout(t *testing.T) {
	block := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer s.Close()
	defer close(block)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if res, _ := Send(ctx, s.Client(), s.URL, Report{}); res != ResOffline {
		t.Fatalf("долгий ответ: %s", res)
	}
}

func TestReporterPausesAfterOffline(t *testing.T) {
	now := time.Unix(10000, 0)
	f := &fakeMap{code: 502}
	s := f.start(t)
	installs := 0
	r := newReporter(ReporterConfig{URL: s.URL, Client: s.Client(), Now: func() time.Time { return now },
		Install: func() string { installs++; return "id-1" }})
	p := Pass{From: "TNL-001", To: "TNL-002", Region: "europe"}
	if st := r.Handle(context.Background(), p); st.Result != ResOffline || !st.RetryAt.Equal(now.Add(OfflinePause)) {
		t.Fatalf("%+v", st)
	}
	// Через минуту — не шлём вовсе, честно говорим, когда попробуем.
	now = now.Add(time.Minute)
	f.code = 200
	if st := r.Handle(context.Background(), p); st.Result != ResPaused || f.hits != 1 {
		t.Fatalf("%+v, запросов %d", st, f.hits)
	}
	// Пауза прошла — шлём.
	now = now.Add(OfflinePause)
	st := r.Handle(context.Background(), p)
	if st.Result != ResOK || f.hits != 2 || st.FromName == "" || st.ToName == "" || !st.RetryAt.IsZero() {
		t.Fatalf("%+v, запросов %d", st, f.hits)
	}
	if last := r.Last(); last == nil || last.Result != ResOK || last.From != "TNL-001" {
		t.Fatalf("%+v", last)
	}
	if !strings.Contains(f.bodies[1], `"install":"id-1"`) || installs != 2 {
		t.Fatal(f.bodies, installs)
	}
}

func TestReporterLimitPauseAndIPLiveNoPause(t *testing.T) {
	now := time.Unix(10000, 0)
	f := &fakeMap{code: 429, reply: `{"why":"iplive"}`}
	s := f.start(t)
	r := newReporter(ReporterConfig{URL: s.URL, Client: s.Client(), Now: func() time.Time { return now }})
	p := Pass{From: "TNL-001", To: "TNL-002"}
	r.Handle(context.Background(), p)
	r.Handle(context.Background(), p)
	if f.hits != 2 {
		t.Fatalf("iplive не должен ставить паузу: %d", f.hits)
	}
	f.reply = `{"why":"limit"}`
	if st := r.Handle(context.Background(), p); st.Result != ResLimit || !st.RetryAt.Equal(now.Add(LimitPause)) {
		t.Fatalf("%+v", st)
	}
	r.Handle(context.Background(), p)
	if f.hits != 3 {
		t.Fatalf("после лимита шлём через %v: %d", LimitPause, f.hits)
	}
}

func TestReporterOfferAndNote(t *testing.T) {
	f := &fakeMap{code: 200}
	s := f.start(t)
	r := NewReporter(ReporterConfig{URL: s.URL, Client: s.Client()})
	r.Note(Pass{From: "TNL-001", To: "TNL-002", Region: "asia"}, ResRegion)
	if l := r.Last(); l.Result != ResRegion || f.hits != 0 {
		t.Fatalf("%+v", l)
	}
	r.Offer(Pass{From: "TNL-001", To: "TNL-002"})
	deadline := time.Now().Add(3 * time.Second)
	for r.Last().Result != ResOK && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if l := r.Last(); l.Result != ResOK {
		t.Fatalf("%+v", l)
	}
}

func TestDefaultClientNoRedirects(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("ушли по редиректу") }))
	defer other.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/roads/report", http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	r := newReporter(ReporterConfig{URL: s.URL})
	if st := r.Handle(context.Background(), Pass{From: "TNL-001", To: "TNL-002"}); st.Result != ResRefused || st.Why != "307" {
		t.Fatalf("%+v", st)
	}
}
