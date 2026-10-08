package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// makeZip пишет zip с файлами имя → содержимое.
func makeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for n, c := range files {
		fw, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(c))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestExtractAndFindRoot(t *testing.T) {
	dir := t.TempDir()
	z := filepath.Join(dir, "a.zip")
	makeZip(t, z, map[string]string{
		"AlbionJournal/AlbionJournal.exe":    "1.0.1",
		"AlbionJournal/acp-prices.exe":       "приёмник",
		"AlbionJournal/zapret/bin/winws.exe": "winws",
		"AlbionJournal/README-RU.txt":        "читай",
	})
	out := filepath.Join(dir, "out")
	os.MkdirAll(filepath.Join(out, "мусор"), 0755)
	if err := Extract(z, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "мусор")); !os.IsNotExist(err) {
		t.Fatal("старое содержимое папки не убрано")
	}
	root, err := FindRoot(out)
	if err != nil || root != filepath.Join(out, "AlbionJournal") {
		t.Fatalf("%s %v", root, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "zapret", "bin", "winws.exe")); string(b) != "winws" {
		t.Fatal("вложенная папка")
	}
}

func TestExtractRejectsEscape(t *testing.T) {
	for _, name := range []string{"../evil.exe", "a/../../evil.exe", "/abs.exe", `..\evil.exe`, "C:/evil.exe"} {
		dir := t.TempDir()
		z := filepath.Join(dir, "a.zip")
		makeZip(t, z, map[string]string{name: "x"})
		if err := Extract(z, filepath.Join(dir, "out")); err == nil {
			t.Errorf("%q: выход за папку не замечен", name)
		}
		if _, err := os.Stat(filepath.Join(dir, "evil.exe")); err == nil {
			t.Errorf("%q: файл записан вне папки", name)
		}
	}
}

func TestFindRootNeedsBothExes(t *testing.T) {
	dir := t.TempDir()
	z := filepath.Join(dir, "a.zip")
	makeZip(t, z, map[string]string{"AlbionJournal/AlbionJournal.exe": "1.0.1"})
	Extract(z, filepath.Join(dir, "out"))
	if _, err := FindRoot(filepath.Join(dir, "out")); err == nil {
		t.Fatal("без acp-prices.exe — не программа")
	}
}
