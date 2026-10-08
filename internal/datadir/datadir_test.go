package datadir

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMigrateMovesZoneFixFiles(t *testing.T) {
	old, nw := t.TempDir(), t.TempDir()
	write(t, old, "переходы.jsonl", "старые")
	write(t, old, "strategies.json", "[]")
	write(t, old, "запись-2026-10-05_10-00-00.azf", "x")
	write(t, old, "AlbionZoneFix.exe", "exe")
	write(t, old, "README-RU.txt", "readme")
	write(t, old, "проверки.jsonl", "старые проверки")
	write(t, nw, "проверки.jsonl", "новые проверки")

	moved, err := Migrate(old, nw)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(moved)
	want := []string{"strategies.json", "запись-2026-10-05_10-00-00.azf", "переходы.jsonl"}
	if strings.Join(moved, ",") != strings.Join(want, ",") {
		t.Fatalf("перенесено %v, ждал %v", moved, want)
	}
	if read(t, nw, "переходы.jsonl") != "старые" {
		t.Fatal("история не переехала")
	}
	if _, err := os.Stat(filepath.Join(old, "переходы.jsonl")); !os.IsNotExist(err) {
		t.Fatal("старый файл остался")
	}
	if read(t, nw, "проверки.jsonl") != "новые проверки" {
		t.Fatal("затёрт файл, который уже был в новой папке")
	}
	if read(t, old, "проверки.jsonl") != "старые проверки" {
		t.Fatal("старый файл при конфликте надо оставить на месте")
	}
	if _, err := os.Stat(filepath.Join(nw, "AlbionZoneFix.exe")); err == nil {
		t.Fatal("чужие файлы переносить нельзя")
	}
}

func TestMigrateSameDirAndMissingOld(t *testing.T) {
	d := t.TempDir()
	write(t, d, "переходы.jsonl", "x")
	if moved, err := Migrate(d, d); err != nil || moved != nil {
		t.Fatalf("%v %v", moved, err)
	}
	if moved, err := Migrate(filepath.Join(d, "нет"), d); err != nil || moved != nil {
		t.Fatalf("%v %v", moved, err)
	}
}

func TestDirIsCreated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
		t.Fatalf("%s не создан: %v", d, err)
	}
	if !strings.HasPrefix(filepath.Base(d), Name) {
		t.Fatalf("имя папки %q", d)
	}
}
