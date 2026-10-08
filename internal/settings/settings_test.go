package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsWhenNoFile(t *testing.T) {
	s := Open(t.TempDir())
	if s.Get() != Default() {
		t.Fatalf("ждал умолчания, получил %+v", s.Get())
	}
	if !s.Get().ShareADP || !s.Get().SessionStats || !s.Get().AutoUpdate {
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
	if got.ShareADP || !got.SessionStats || !got.CollectOnStart || !got.AutoUpdate {
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
