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
	if !s.Get().AutoUpdate || !s.Get().MapSend {
		t.Fatal("автообновление и отправка дорог на карту по умолчанию включены")
	}
	if s.Get().ShareADP || s.Get().SessionStats || s.Get().CollectOnStart {
		t.Fatal("сбор цен, ADP и счётчик в новой установке выключены")
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
	// Файл был — установка не новая: прежние умолчания, цены не выключаются.
	if got := Open(dir).Get(); got != legacy() || !got.CollectOnStart || !got.ShareADP || !got.SessionStats {
		t.Fatalf("битый файл должен давать прежние умолчания: %+v", got)
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
		``:                  SkinPlain, // пустой файл — битый, берутся прежние умолчания
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

// Новая установка: работает только Авалон, и этот выбор сразу в файле —
// следующая версия с другими умолчаниями его не поменяет.
func TestNewInstallOnlyAvalonAndSaved(t *testing.T) {
	dir := t.TempDir()
	got := Open(dir).Get()
	if got.ShareADP || got.SessionStats || got.CollectOnStart {
		t.Fatalf("цены, ADP или счётчик включены: %+v", got)
	}
	if !got.MapSend || got.ZoneShow != ShowNotify || got.ZoneOverlaySec != 5 || got.ZoneOverlayCorner != CornerTopRight {
		t.Fatalf("Авалон и карточка: %+v", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("умолчания не записаны: %v", err)
	}
	if !strings.Contains(string(b), `"collectOnStart": false`) || !strings.Contains(string(b), `"shareADP": false`) {
		t.Fatalf("файл: %s", b)
	}
	if again := Open(dir).Get(); again != got {
		t.Fatalf("второе открытие: %+v", again)
	}
}

// Уже установленная программа: выбор в файле не меняется, а ключей,
// которых в старом файле нет, — прежние умолчания (всё включено).
func TestExistingFileKeepsChoice(t *testing.T) {
	for body, want := range map[string][3]bool{
		`{"language":"ru"}`: {true, true, true},
		`{"shareADP":true,"sessionStats":true,"collectOnStart":true}`:    {true, true, true},
		`{"shareADP":false,"sessionStats":true,"collectOnStart":false}`:  {false, true, false},
		`{"shareADP":true,"sessionStats":false,"collectOnStart":true}`:   {true, false, true},
		`{"shareADP":false,"sessionStats":false,"collectOnStart":false}`: {false, false, false},
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0644)
		s := Open(dir).Get()
		if got := [3]bool{s.ShareADP, s.SessionStats, s.CollectOnStart}; got != want {
			t.Errorf("%s: %v, а надо %v", body, got, want)
		}
	}
}

// Переход на способ показа: старый переключатель «уведомлением» решает.
func TestZoneShowFromOldNotifyToggle(t *testing.T) {
	for body, want := range map[string]string{
		`{"language":"ru"}`:                       ShowNotify, // ключа не было — было включено
		`{"zoneNotify":true}`:                     ShowNotify,
		`{"zoneNotify":false}`:                    ShowOff,
		`{"zoneNotify":false,"zoneShow":"panel"}`: ShowPanel,
		`{"zoneNotify":true,"zoneShow":"off"}`:    ShowOff,
		`{"zoneShow":"neon"}`:                     ShowNotify,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0644)
		s := Open(dir).Get()
		if s.ZoneShow != want || s.ZoneNotify != (want == ShowNotify) {
			t.Errorf("%s: %q (zoneNotify %v), а надо %q", body, s.ZoneShow, s.ZoneNotify, want)
		}
	}
}

func TestOverlayNormalize(t *testing.T) {
	s := Settings{ZoneShow: "x", ZoneOverlaySec: 0, ZoneOverlayCorner: "middle"}.Normalize()
	if s.ZoneShow != ShowNotify || !s.ZoneNotify || s.ZoneOverlaySec != 5 || s.ZoneOverlayCorner != CornerTopRight {
		t.Fatalf("%+v", s)
	}
	for in, want := range map[int]int{-3: 5, 1: 2, 2: 2, 8: 8, 30: 30, 99: 30} {
		if got := ClampOverlaySec(in); got != want {
			t.Errorf("ClampOverlaySec(%d)=%d", in, got)
		}
	}
	for _, c := range []string{CornerTopRight, CornerBottomRight, CornerTopLeft, CornerBottomLeft} {
		if NormalizeCorner(c) != c {
			t.Errorf("угол %q", c)
		}
	}
	p := Settings{ZoneShow: ShowPanel, ZoneNotify: true}.Normalize()
	if p.ZoneNotify {
		t.Fatal("панель — не уведомление")
	}
}
