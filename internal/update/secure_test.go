package update

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestSDDLTrusted(t *testing.T) {
	cases := map[string]bool{
		stageSDDL: true,
		"O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)":                           true,
		"O:SYG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)":                     true,
		"O:S-1-5-32-544D:P(A;OICI;FA;;;S-1-5-18)(A;OICI;FA;;;S-1-5-32-544)": true,
		"O:BAG:BAD:P(A;OICI;FA;;;SY)":                                       true,
		// Владелец — пользователь (создал папку заранее).
		"O:S-1-5-21-1-2-3-1001G:S-1-5-21-1-2-3-513D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)": false,
		// Наследование не отключено.
		"O:BAG:BAD:AI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)": false,
		"O:BAG:BAD:(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)":   false,
		// Пользователи могут писать.
		"O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1301bf;;;BU)": false,
		"O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICIIO;FA;;;CO)":     false,
		"O:BAG:BAD:P(A;OICI;FA;;;SY)(A;;FA;;;WD)":                           false,
		// Запрет кому-то — тоже не наш формат.
		"O:BAG:BAD:P(D;OICI;FA;;;BU)(A;OICI;FA;;;BA)": false,
		// Нет DACL / пустой / NULL.
		"O:BAG:BA":                    false,
		"O:BAG:BAD:P":                 false,
		"O:BAG:BAD:NO_ACCESS_CONTROL": false,
		"":                            false,
		"мусор":                       false,
		"O:BAG:BAD:P(A;OICI;FA;;;BA":  false,
	}
	for s, want := range cases {
		if got := SDDLTrusted(s); got != want {
			t.Errorf("SDDLTrusted(%q) = %v", s, got)
		}
	}
}

func TestSecureStageRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	elsewhere := filepath.Join(dir, "куда-то")
	os.MkdirAll(elsewhere, 0755)
	// Родитель — ссылка.
	os.Symlink(elsewhere, filepath.Join(dir, "Albion Journal"))
	if err := SecureStage(filepath.Join(dir, "Albion Journal", "update")); err == nil {
		t.Fatal("родитель-ссылка принят")
	}
	// Сама папка — ссылка.
	os.MkdirAll(filepath.Join(dir, "AJ2"), 0755)
	os.Symlink(elsewhere, filepath.Join(dir, "AJ2", "update"))
	if err := SecureStage(filepath.Join(dir, "AJ2", "update")); err == nil {
		t.Fatal("папка-ссылка принята")
	}
	if err := SecureStage(filepath.Join(dir, "AJ3", "update")); err != nil {
		t.Fatal(err)
	}
}

func TestUpdaterStopsWhenStageNotSecure(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.u.CheckSync()
	if h.u.Status().State != StateReady {
		t.Fatal("подготовка")
	}
	c := h.u.c
	c.Secure = func(string) error { return errors.New("папка чужая") }
	u := New(c)
	u.CheckSync()
	if u.Status().State != StateError || g.api.Load() != 1 {
		t.Fatal("в чужую папку не качаем")
	}
	u.ready = h.u.Ready()
	if err := u.Install(true); err == nil || len(h.launched) != 0 {
		t.Fatal("из чужой папки не ставим")
	}
}

func TestApplyRefusesLinks(t *testing.T) {
	// Папка программы содержит ссылку zapret → чужое место.
	p := fixture(t)
	outside := t.TempDir()
	os.RemoveAll(filepath.Join(p.Dest, "zapret"))
	os.Symlink(outside, filepath.Join(p.Dest, "zapret"))
	f := &fakeEnv{}
	if err := Apply(p, f.env(true)); err == nil {
		t.Fatal("ссылка в папке программы пропущена")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("записано мимо папки программы: %v", ents)
	}
	if read(t, p.Dest, MainExe) != "1.0.0" {
		t.Fatal("откат не прошёл")
	}

	// Ссылка в самом обновлении.
	p = fixture(t)
	os.Symlink("/etc/hosts", filepath.Join(p.Src, "hosts.txt"))
	if err := Apply(p, (&fakeEnv{}).env(true)); err == nil {
		t.Fatal("ссылка в обновлении пропущена")
	}

	// Подложенный «AlbionJournal.exe.new» — ссылка на чужой файл: не пишем сквозь.
	p = fixture(t)
	victim := filepath.Join(t.TempDir(), "жертва")
	os.WriteFile(victim, []byte("цело"), 0644)
	os.Symlink(victim, filepath.Join(p.Dest, MainExe+".new"))
	if err := Apply(p, (&fakeEnv{}).env(true)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "цело" || read(t, p.Dest, MainExe) != "1.0.1" {
		t.Fatal("запись прошла по подложенной ссылке")
	}

	// Папка программы сама — ссылка.
	p = fixture(t)
	real := p.Dest + "-настоящая"
	os.Rename(p.Dest, real)
	os.Symlink(real, p.Dest)
	if err := Apply(p, (&fakeEnv{}).env(true)); err == nil {
		t.Fatal("папка программы-ссылка принята")
	}
}

func TestExtractRefusesLinkedParent(t *testing.T) {
	dir := t.TempDir()
	z := filepath.Join(dir, "a.zip")
	makeZip(t, z, map[string]string{"AlbionJournal/AlbionJournal.exe": "x"})
	os.Symlink(t.TempDir(), filepath.Join(dir, "link"))
	if err := Extract(z, filepath.Join(dir, "link", "out")); err == nil {
		t.Fatal("распаковка через ссылку")
	}
}

func TestRedirectFilter(t *testing.T) {
	cases := map[string]bool{
		"https://github.com/VAnatoliyV/albion-zone-fix/releases/download/v1/AlbionJournal.zip": true,
		"https://objects.githubusercontent.com/github-production-release-asset/1":              true,
		"https://release-assets.githubusercontent.com/github-production-release-asset/1":       true,
		"https://api.github.com/repositories/1/releases/latest":                                true,
		"https://github.com:443/x":                     true,
		"http://objects.githubusercontent.com/x":       false,
		"https://evil.example/x":                       false,
		"https://githubusercontent.com.evil.example/x": false,
		"https://evilgithubusercontent.com/x":          false,
		"https://gist.github.com.evil.example/x":       false,
		"https://github.com:8443/x":                    false,
		"https://u@objects.githubusercontent.com/x":    false,
	}
	c := NewClient()
	for s, want := range cases {
		u, _ := url.Parse(s)
		if RedirectAllowed(u) != want {
			t.Errorf("RedirectAllowed(%s) = %v", s, !want)
		}
		req := &http.Request{URL: u}
		if err := c.CheckRedirect(req, []*http.Request{{}}); (err == nil) != want {
			t.Errorf("CheckRedirect(%s): %v", s, err)
		}
	}
	u, _ := url.Parse("https://github.com/x")
	if c.CheckRedirect(&http.Request{URL: u}, make([]*http.Request, 10)) == nil {
		t.Fatal("больше 10 переадресаций")
	}
}
