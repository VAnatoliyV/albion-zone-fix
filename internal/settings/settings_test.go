package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsWhenNoFile(t *testing.T) {
	s := Open(t.TempDir())
	if s.Get() != Default() {
		t.Fatalf("ждал умолчания, получил %+v", s.Get())
	}
	if !s.Get().ShareADP || !s.Get().SessionStats || !s.Get().AutoUpdate || !s.Get().MapSend {
		t.Fatal("ADP, счётчик и автообновление по умолчанию включены")
	}
	if s.Get().BuiltinBypass || s.Get().StartWithWindows {
		t.Fatal("встроенный обход и автозапуск по умолчанию выключены")
	}
}

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	v := s.Get()
	v.ShareADP, v.Language = false, "es"
	if err := s.Set(v); err != nil {
		t.Fatal(err)
	}
	if got := Open(dir).Get(); got != v {
		t.Fatalf("после перечтения %+v, ждал %+v", got, v)
	}
}

func TestMissingFieldsKeepDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{"shareADP": false}`), 0644)
	got := Open(dir).Get()
	// Файл прошлой версии без autoUpdate — автообновление включено.
	if got.ShareADP || !got.SessionStats || !got.CollectOnStart || !got.AutoUpdate || !got.MapSend {
		t.Fatalf("%+v", got)
	}
}

func TestBrokenFileGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{не json`), 0644)
	if Open(dir).Get() != Default() {
		t.Fatal("битый файл должен давать умолчания")
	}
}

func TestZoneCardDefaultsForOldFile(t *testing.T) {
	dir := t.TempDir()
	// Файл от этапа 3: полей карточки в нём нет — берутся по умолчанию.
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{"language":"ru","mapSend":false}`), 0644)
	s := Open(dir).Get()
	if s.MapSend || s.ZoneKey != "xbutton1" || !s.ZoneNotify || !s.NotifyChests || !s.NotifyRes || !s.NotifyDng ||
		!s.NotifyPortal || s.NotifyOrder != "chestsFirst" || !s.BlackWarn {
		t.Fatalf("%+v", s)
	}
}

func TestStage5DefaultsLikeMac(t *testing.T) {
	dir := t.TempDir()
	// Файл версии 1.0.1: полей этапа 5 нет — как у мака по умолчанию.
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{"language":"ru"}`), 0644)
	s := Open(dir).Get()
	if s.Skin != SkinPlain || !s.LogoAnim {
		t.Fatalf("оформление: %+v", s)
	}
	if s.ResetOnZone || s.ShowWithGame || s.StartWithGame || s.QuitWithGame || s.WatchGame() {
		t.Fatalf("сброс при смене зоны и «вместе с игрой» по умолчанию выключены: %+v", s)
	}
}

func TestSkinSavedAndNormalized(t *testing.T) {
	dir := t.TempDir()
	st := Open(dir)
	v := st.Get()
	if v.Skin != SkinPlain {
		t.Fatalf("новые настройки — обычное оформление: %q", v.Skin)
	}
	v.Skin, v.LogoAnim, v.QuitWithGame = SkinPixel, false, true
	if err := st.Set(v); err != nil {
		t.Fatal(err)
	}
	got := Open(dir).Get()
	if got.Skin != SkinPixel || got.LogoAnim || !got.WatchGame() {
		t.Fatalf("%+v", got)
	}
	for in, want := range map[string]string{"plain": SkinPlain, "pixel": SkinPixel, "": SkinPlain, "neon": SkinPlain} {
		if NormalizeSkin(in) != want {
			t.Errorf("NormalizeSkin(%q)=%q", in, NormalizeSkin(in))
		}
	}
}

// Обычное оформление по умолчанию; уже выбранное пиксельное не трогаем.
func TestSkinDefaultPlainKeepsSaved(t *testing.T) {
	for body, want := range map[string]string{
		``:                  SkinPlain, // пустой файл — битый, берутся настройки по умолчанию
		`{}`:                SkinPlain,
		`{"language":"ru"}`: SkinPlain, // старый файл без ключа
		`{"language":"ru","skin":"pixel","themeV2":true}`: SkinPixel, // выбрано после перехода
		`{"skin":"plain"}`: SkinPlain,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0644)
		if got := Open(dir).Get().Skin; got != want {
			t.Errorf("%q: %q, а надо %q", body, got, want)
		}
	}
	if got := Open(t.TempDir()).Get().Skin; got != SkinPlain {
		t.Errorf("нет файла: %q", got)
	}
}

// Разовый переход на обычное оформление: в старом файле skin всегда
// "pixel" (его писала любая запись настроек) — один раз ставим обычное,
// дальше выбор пользователя держится.
func TestThemeV2Migration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	os.WriteFile(path, []byte(`{"language":"es","skin":"pixel","mapSend":false}`), 0644)
	s := Open(dir).Get()
	if s.Skin != SkinPlain || !s.ThemeV2 || s.Language != "es" || s.MapSend {
		t.Fatalf("переход: %+v", s)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"themeV2": true`) || !strings.Contains(string(b), `"skin": "plain"`) {
		t.Fatalf("переход не записан: %s", b)
	}
	// Пользователь вернул пиксельное — после перезапуска оно и осталось.
	st := Open(dir)
	v := st.Get()
	v.Skin = SkinPixel
	if err := st.Set(v); err != nil {
		t.Fatal(err)
	}
	if got := Open(dir).Get(); got.Skin != SkinPixel || !got.ThemeV2 {
		t.Fatalf("выбор после перехода: %+v", got)
	}
}
