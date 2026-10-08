package update

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Файлы, без которых папка — не Albion Journal.
const (
	MainExe     = "AlbionJournal.exe"
	ReceiverExe = "acp-prices.exe"
)

// maxUnpacked — предел распакованного (защита от zip-бомбы).
const maxUnpacked = 1 << 30

// Extract распаковывает zip в dest (dest очищается). Имена, выходящие за
// dest («..», абсолютные пути, диски), — ошибка.
func Extract(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	var total int64
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
			return fmt.Errorf("в архиве чужой путь: %q", f.Name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return fmt.Errorf("в архиве чужой путь: %q", f.Name)
			}
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if rel, err := filepath.Rel(dest, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("в архиве чужой путь: %q", f.Name)
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			return fmt.Errorf("в архиве не файл: %q", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		n, err := extractOne(f, target, maxUnpacked-total)
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}

func extractOne(f *zip.File, target string, limit int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(rc, limit+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > limit {
		err = errors.New("архив распаковывается слишком большим")
	}
	return n, err
}

// FindRoot — папка программы внутри распакованного: dir или единственная
// папка в нём (собрать.sh кладёт всё в AlbionJournal/). В ней обязаны
// быть AlbionJournal.exe и acp-prices.exe.
func FindRoot(dir string) (string, error) {
	if hasProgram(dir) {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() && hasProgram(filepath.Join(dir, e.Name())) {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("внутри обновления нет %s и %s", MainExe, ReceiverExe)
}

func hasProgram(dir string) bool {
	for _, n := range []string{MainExe, ReceiverExe} {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}
