// Пакет datadir — каталог данных Albion Journal и перенос файлов Zone Fix,
// которые раньше лежали рядом с программой.
package datadir

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Name — имя папки в %AppData%.
const Name = "Albion Journal"

// Dir — каталог данных; создаётся, если его нет. В Windows это
// %AppData%\Albion Journal. На маке и в Linux (разработка, -replay) —
// отдельная папка, чтобы не задеть данные мак-приложения с тем же именем.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, dirName)
	return d, os.MkdirAll(d, 0755)
}

// Migrate переносит файлы Zone Fix из старой папки (рядом с exe) в новую.
// Файл, который в новой папке уже есть, не трогаем: там свежее.
// Возвращает имена перенесённых файлов.
func Migrate(oldDir, newDir string) ([]string, error) {
	if same(oldDir, newDir) {
		return nil, nil
	}
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var moved []string
	var firstErr error
	for _, e := range entries {
		if e.IsDir() || !ours(e.Name()) {
			continue
		}
		src, dst := filepath.Join(oldDir, e.Name()), filepath.Join(newDir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := move(src, dst); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		moved = append(moved, e.Name())
	}
	return moved, firstErr
}

// ours — файлы, которые Zone Fix создавал рядом с собой.
func ours(name string) bool {
	switch name {
	case "переходы.jsonl", "проверки.jsonl", "трассировки.jsonl", "strategies.json":
		return true
	}
	return strings.HasPrefix(name, "запись-") && strings.HasSuffix(name, ".azf")
}

func same(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && strings.EqualFold(aa, bb)
}

// move — переименование, а если папки на разных дисках — копия и удаление.
func move(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		in.Close()
		return err
	}
	_, err = io.Copy(out, in)
	in.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(src)
}
