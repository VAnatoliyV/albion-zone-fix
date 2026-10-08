//go:build !windows

package update

import (
	"fmt"
	"os"
	"path/filepath"
)

// StageDir — на маке (разработка) папки ProgramData нет: обновлятель
// получает каталог из Config.Dir; эта функция для единообразия.
func StageDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Albion Journal Windows dev", "update"), nil
}

// SecureStage — на маке ACL Windows нет: только отказ от символических
// ссылок на месте папки и её родителя и права 0700.
func SecureStage(dir string) error {
	for _, p := range []string{filepath.Dir(filepath.Clean(dir)), dir} {
		if IsReparse(p) {
			return fmt.Errorf("%s — ссылка, а не папка", p)
		}
	}
	return os.MkdirAll(dir, 0700)
}

// IsReparse — путь существует и это символическая ссылка.
func IsReparse(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}
