package update

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Plan — что ставить и куда. Его же получает процесс установки в
// аргументах командной строки (см. Plan.Args и ParseArgs).
type Plan struct {
	Src     string // папка новой версии (…\обновление\ставлю\AlbionJournal)
	Dest    string // папка программы
	Prev    string // куда убрать заменённые файлы (…\обновление\предыдущая)
	PID     int    // программа, выхода которой ждём
	Restart bool   // после установки запустить программу
	DataDir string // каталог данных (pid-файл приёмника, журнал)
}

// ApplyFlag — флаг командной строки процесса установки. Договор между
// старой версией (запускает) и новой (ставит) — менять только совместимо:
//
//	AlbionJournal.exe -apply-update SRC -target DEST -prev PREV -pid N -data DATA [-restart]
const ApplyFlag = "apply-update"

// Args — аргументы для запуска процесса установки.
func (p Plan) Args() []string {
	a := []string{"-" + ApplyFlag, p.Src, "-target", p.Dest, "-prev", p.Prev,
		"-pid", strconv.Itoa(p.PID), "-data", p.DataDir}
	if p.Restart {
		a = append(a, "-restart")
	}
	return a
}

// Env — то, что зависит от Windows; в тестах подделки.
type Env struct {
	// WaitExit ждёт выхода процесса не дольше d; false — не вышел.
	WaitExit func(pid int, d time.Duration) bool
	// StopReceiver гасит наш acp-prices.exe (по pid-файлу, чужой не трогает).
	StopReceiver func()
	// Unblock снимает с файла Mark-of-the-Web (поток Zone.Identifier).
	Unblock func(path string)
	// Start запускает программу после установки.
	Start func(exe string) error
	// Copy копирует файл (nil — copyFile); в тестах — чтобы сломать.
	Copy func(src, dst string) error
	Logf func(format string, a ...any)
}

// WaitTimeout — сколько ждём выхода программы.
const WaitTimeout = 30 * time.Second

// Apply ставит обновление: ждёт выхода программы, гасит приёмник, кладёт
// новые файлы поверх старых (старые — в Prev), при ошибке возвращает всё
// как было. Одинаковые файлы не трогает: так драйвер WinDivert в
// zapret\bin, который Windows может держать открытым, не мешает установке.
// При Restart запускает программу и после отката (прежнюю версию).
func Apply(p Plan, e Env) error {
	logf := e.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	cp := e.Copy
	if cp == nil {
		cp = copyFile
	}
	if p.PID > 0 && e.WaitExit != nil && !e.WaitExit(p.PID, WaitTimeout) {
		logf("программа не закрылась за %v — обновление отложено", WaitTimeout)
		return errors.New("программа не закрылась")
	}
	if e.StopReceiver != nil {
		e.StopReceiver()
	}
	err := replace(p, cp, e.Unblock, logf)
	if err != nil {
		logf("установка не удалась: %v", err)
	} else {
		logf("поставлено в %s", p.Dest)
	}
	if p.Restart && e.Start != nil {
		exe := filepath.Join(p.Dest, MainExe)
		if serr := e.Start(exe); serr != nil {
			logf("не запустить %s: %v", exe, serr)
		}
	}
	return err
}

type moved struct{ dst, prev string }

func replace(p Plan, cp func(src, dst string) error, unblock func(string), logf func(string, ...any)) (err error) {
	if _, err := FindRoot(p.Src); err != nil {
		return err
	}
	if err := os.RemoveAll(p.Prev); err != nil {
		return fmt.Errorf("не очистить %s: %w", p.Prev, err)
	}
	var movedFiles []moved
	var created []string
	var createdDirs []string
	defer func() {
		if err == nil {
			return
		}
		// Откат в обратном порядке: убрать новое, вернуть старое.
		for i := len(created) - 1; i >= 0; i-- {
			os.Remove(created[i])
		}
		for i := len(movedFiles) - 1; i >= 0; i-- {
			m := movedFiles[i]
			if rerr := moveFile(m.prev, m.dst, cp); rerr != nil {
				logf("откат: не вернуть %s: %v", m.dst, rerr)
			}
		}
		for i := len(createdDirs) - 1; i >= 0; i-- {
			os.Remove(createdDirs[i]) // только пустые
		}
		logf("откат: прежние файлы на месте")
	}()

	return filepath.WalkDir(p.Src, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(p.Src, path)
		if err != nil || rel == "." {
			return err
		}
		dst := filepath.Join(p.Dest, rel)
		if d.IsDir() {
			if _, err := os.Stat(dst); os.IsNotExist(err) {
				if err := os.Mkdir(dst, 0755); err != nil {
					return err
				}
				createdDirs = append(createdDirs, dst)
			}
			return nil
		}
		if same(path, dst) {
			return nil
		}
		if _, err := os.Lstat(dst); err == nil {
			prev := filepath.Join(p.Prev, rel)
			if err := os.MkdirAll(filepath.Dir(prev), 0755); err != nil {
				return err
			}
			if err := moveFile(dst, prev, cp); err != nil {
				return fmt.Errorf("не убрать старый %s: %w", rel, err)
			}
			movedFiles = append(movedFiles, moved{dst, prev})
		}
		// Сначала во временный файл рядом, потом переименование: оборванное
		// копирование не оставит полфайла под настоящим именем.
		tmp := dst + ".new"
		if err := cp(path, tmp); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("не скопировать %s: %w", rel, err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("не поставить %s: %w", rel, err)
		}
		created = append(created, dst)
		if unblock != nil {
			unblock(dst)
		}
		return nil
	})
}

// moveFile: переименование, а между дисками (данные на C:, программа на D:)
// — копия и удаление.
func moveFile(src, dst string, cp func(src, dst string) error) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := cp(src, dst); err != nil {
		os.Remove(dst)
		return err
	}
	if err := os.Remove(src); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// same — файлы одинаковые по содержимому.
func same(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil || !sb.Mode().IsRegular() || sa.Size() != sb.Size() {
		return false
	}
	fa, err := os.Open(a)
	if err != nil {
		return false
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false
	}
	defer fb.Close()
	ba, bb := make([]byte, 64<<10), make([]byte, 64<<10)
	for {
		na, ea := io.ReadFull(fa, ba)
		nb, eb := io.ReadFull(fb, bb)
		if na != nb || !bytes.Equal(ba[:na], bb[:nb]) {
			return false
		}
		if ea != nil || eb != nil {
			return (ea == io.EOF || ea == io.ErrUnexpectedEOF) && (eb == io.EOF || eb == io.ErrUnexpectedEOF)
		}
	}
}
