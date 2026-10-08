// Пакет oldcopy — поиск и удаление старых копий Albion Journal в других
// папках (распакованный zip в «Загрузках» и т. п.). Зовёт установщик:
// AlbionJournal.exe -find-old / -remove-old <папка установки>.
//
// Зачем: старая копия, запущенная (или поднятая автозапуском) из другой
// папки, держит знак «программа уже запущена» — новая показывает её окно.
//
// Что считается старой копией: папка exe запущенного AlbionJournal.exe или
// папка программы из задачи автозапуска, в которой лежат все три файла
// набора (AlbionJournal.exe, acp-prices.exe, items_by_id.json). Не трогаем:
// папку установки (и папки внутри неё или над ней), корень диска, системные
// папки.
//
// Что удаляем: если в папке нет ничего чужого (только наши файлы и папка
// zapret) — все наши файлы, файлы zapret\bin и пустые папки; иначе (общая
// папка: «Загрузки», профиль) — только файлы набора, остальное не трогаем.
// Процессы закрываем только наши (три имени) и только из этой папки.
//
// Здесь — чистая логика; процессы, окна и задача — за Env и Closer
// (oldcopy_windows.go), чтобы проверять на маке.
package oldcopy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Имена наших файлов.
const (
	ExeName    = "AlbionJournal.exe"
	RecvName   = "acp-prices.exe"
	ItemsName  = "items_by_id.json"
	BypassName = "winws.exe"
	ZapretDir  = "zapret"
)

// setFiles — набор: все три в папке — значит, это наша программа.
var setFiles = []string{ExeName, RecvName, ItemsName}

// extraFiles — наши файлы с обычными именами: удаляем только в папке, где
// нет ничего чужого (uninstall.exe — прежняя установка в другую папку).
var extraFiles = []string{"README-RU.txt", "LICENSES.txt", "TESTER-RU.txt", "uninstall.exe"}

// suffixes — сам файл и следы обновления рядом (.new — недокопированный,
// .old — прежний).
var suffixes = []string{"", ".old", ".new"}

// ParentName — имя папки над копией, которую можно убрать, если она пуста
// (zip распаковывается в AlbionJournal\AlbionJournal).
const ParentName = "AlbionJournal"

// Wait — сколько ждать выхода процессов старой копии.
const Wait = 5 * time.Second

// Proc — процесс: pid, имя и полный путь exe.
type Proc struct {
	PID  uint32
	Name string
	Exe  string
}

// Copy — найденная старая копия.
type Copy struct {
	Dir  string // папка (нормализованный путь)
	Full bool   // в папке только наше: удаляем всё наше; иначе — только набор
}

// Env — что пакету нужно от системы.
type Env struct {
	Procs   func() ([]Proc, error) // процессы с путями exe
	TaskExe func() (string, bool)  // программа из задачи автозапуска
	Keep    []string               // системные папки: сами по себе не трогаем
	Under   []string               // внутри этих папок не трогаем ничего (Windows)
	Self    uint32                 // свой pid
	Logf    func(string, ...any)   // журнал (может быть nil)
}

func (e Env) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
	}
}

// Closer закрывает процессы (Windows: oldcopy_windows.go).
type Closer interface {
	Soft(p Proc)                            // попросить выйти, сохранив сессию
	Kill(p Proc) error                      // завершить
	Wait(ps []Proc, d time.Duration) []Proc // ждать выхода; вернуть живых
}

// norm — путь для сравнения: абсолютный, без «..» и хвостового
// разделителя, ссылки раскрыты (на Windows — и короткие имена 8.3).
func norm(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

// same — один и тот же путь (без учёта регистра: так в Windows).
func same(a, b string) bool { return strings.EqualFold(norm(a), norm(b)) }

// within — child лежит внутри parent (не равен ему).
func within(parent, child string) bool {
	p := strings.ToLower(norm(parent))
	c := strings.ToLower(norm(child))
	if !strings.HasSuffix(p, string(filepath.Separator)) {
		p += string(filepath.Separator)
	}
	return len(c) > len(p) && strings.HasPrefix(c, p)
}

// isRoot — корень диска (C:\, /, \\server\share).
func isRoot(p string) bool {
	p = filepath.Clean(p)
	return filepath.Dir(p) == p || p == filepath.VolumeName(p)
}

// isFile — обычный файл (не папка и не ссылка).
func isFile(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

// Find — старые копии для папки установки inst. Причины пропуска — в журнал.
func Find(inst string, e Env) []Copy {
	var cands []string
	if e.Procs != nil {
		ps, err := e.Procs()
		if err != nil {
			e.logf("список процессов: %v", err)
		}
		for _, p := range ps {
			if p.Exe != "" && strings.EqualFold(filepath.Base(p.Exe), ExeName) {
				cands = append(cands, filepath.Dir(p.Exe))
			}
		}
	}
	if e.TaskExe != nil {
		if exe, ok := e.TaskExe(); ok && strings.Trim(exe, `" `) != "" {
			exe = strings.Trim(exe, `" `)
			cands = append(cands, filepath.Dir(exe))
		}
	}
	var out []Copy
	for _, d := range cands {
		c, why := check(d, inst, e)
		if why != "" {
			if why != "-" {
				e.logf("%s: не трогаю — %s", d, why)
			}
			continue
		}
		dup := false
		for _, o := range out {
			dup = dup || strings.EqualFold(o.Dir, c.Dir)
		}
		if !dup {
			out = append(out, c)
		}
	}
	return out
}

// check — годится ли папка d в старые копии. why: "" — годится, "-" — это
// папка установки (молча), иначе — причина.
func check(d, inst string, e Env) (Copy, string) {
	if d == "" || d == "." || !filepath.IsAbs(d) {
		return Copy{}, "путь не полный"
	}
	n := norm(d)
	switch {
	case same(n, inst):
		return Copy{}, "-"
	case isRoot(n):
		return Copy{}, "корень диска"
	case within(inst, n):
		return Copy{}, "внутри папки установки"
	case within(n, inst):
		return Copy{}, "папка установки внутри неё"
	}
	for _, k := range e.Keep {
		if k != "" && same(n, k) {
			return Copy{}, "системная папка"
		}
	}
	for _, u := range e.Under {
		if u != "" && (same(n, u) || within(u, n)) {
			return Copy{}, "внутри системной папки"
		}
	}
	for _, f := range setFiles {
		if !isFile(filepath.Join(n, f)) {
			return Copy{}, "нет " + f + " — не наш набор"
		}
	}
	return Copy{Dir: n, Full: onlyOurs(n)}, ""
}

// ours — имя нашего файла верхнего уровня (с .old/.new).
func ours(name string) bool {
	for _, f := range append(append([]string{}, setFiles...), extraFiles...) {
		for _, s := range suffixes {
			if strings.EqualFold(name, f+s) {
				return true
			}
		}
	}
	return false
}

// onlyOurs — в папке только наши файлы и папка zapret (без ссылок).
func onlyOurs(dir string) bool {
	es, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, de := range es {
		switch {
		case de.Type().IsRegular() && ours(de.Name()):
		case de.Type() == os.ModeDir && strings.EqualFold(de.Name(), ZapretDir):
		default:
			return false
		}
	}
	return true
}

// Targets — какие процессы закрыть для копии c: только AlbionJournal.exe и
// acp-prices.exe из самой папки и (если папка целиком наша) winws.exe из её
// zapret\bin. Свой процесс — никогда.
func Targets(c Copy, ps []Proc, self uint32) []Proc {
	bin := filepath.Join(c.Dir, ZapretDir, "bin")
	var main, rest []Proc
	for _, p := range ps {
		if p.PID == self || p.Exe == "" {
			continue
		}
		base := filepath.Base(p.Exe)
		if p.Name != "" && !strings.EqualFold(p.Name, base) {
			continue
		}
		dir := filepath.Dir(p.Exe)
		switch {
		case strings.EqualFold(base, ExeName) && same(dir, c.Dir):
			main = append(main, p)
		case strings.EqualFold(base, RecvName) && same(dir, c.Dir):
			rest = append(rest, p)
		case strings.EqualFold(base, BypassName) && c.Full && same(dir, bin):
			rest = append(rest, p)
		}
	}
	return append(main, rest...)
}

// Plan — что удалить: файлы и папки (папки — только если пусты, по порядку).
func Plan(c Copy) (files, dirs []string) {
	names := setFiles
	if c.Full {
		names = append(append([]string{}, setFiles...), extraFiles...)
	}
	for _, f := range names {
		for _, s := range suffixes {
			if p := filepath.Join(c.Dir, f+s); isFile(p) {
				files = append(files, p)
			}
		}
	}
	if c.Full {
		zap := filepath.Join(c.Dir, ZapretDir)
		bin := filepath.Join(zap, "bin")
		if fi, err := os.Lstat(zap); err == nil && fi.IsDir() {
			if es, err := os.ReadDir(bin); err == nil {
				for _, de := range es {
					if de.Type().IsRegular() {
						files = append(files, filepath.Join(bin, de.Name()))
					}
				}
			}
			dirs = append(dirs, bin, zap)
		}
	}
	dirs = append(dirs, c.Dir)
	if up := filepath.Dir(c.Dir); !isRoot(up) && strings.EqualFold(filepath.Base(up), ParentName) {
		dirs = append(dirs, up)
	}
	return files, dirs
}

// retryDelay — пауза между попытками удалить файл (только что закрытый
// процесс Windows отпускает не сразу).
var retryDelay = 200 * time.Millisecond

func removeFile(p string) error {
	var err error
	for i := 0; i < 5; i++ {
		if err = os.Remove(p); err == nil || os.IsNotExist(err) {
			return nil
		}
		time.Sleep(retryDelay)
	}
	return err
}

// Delete удаляет по плану. Ошибки — в журнал; вернёт, остался ли exe.
func Delete(c Copy, e Env) (ok bool) {
	files, dirs := Plan(c)
	for _, f := range files {
		if err := removeFile(f); err != nil {
			e.logf("не удалить %s: %v", f, err)
		}
	}
	for _, d := range dirs {
		if err := os.Remove(d); os.IsNotExist(err) {
			continue
		} else if err != nil {
			break // не пуста (чужие файлы) — выше не идём
		}
		e.logf("удалена папка %s", d)
	}
	_, err := os.Lstat(filepath.Join(c.Dir, ExeName))
	return os.IsNotExist(err)
}

// Remove закрывает и удаляет старые копии; retarget переносит задачу
// автозапуска на exe папки установки, если она указывала на старую копию.
// Возвращает число копий, которые убрать не вышло.
func Remove(inst string, e Env, cl Closer, retarget func(exe string) error) (failed int) {
	copies := Find(inst, e)
	if len(copies) == 0 {
		return 0
	}
	var ps []Proc
	if e.Procs != nil {
		ps, _ = e.Procs()
	}
	taskExe := ""
	if e.TaskExe != nil {
		taskExe, _ = e.TaskExe()
		taskExe = strings.Trim(taskExe, `" `)
	}
	for _, c := range copies {
		e.logf("старая копия %s (целиком наша: %v)", c.Dir, c.Full)
		t := Targets(c, ps, e.Self)
		for _, p := range t {
			if strings.EqualFold(filepath.Base(p.Exe), ExeName) {
				cl.Soft(p)
			}
		}
		for _, p := range t {
			if err := cl.Kill(p); err != nil {
				e.logf("не закрыть %s (pid %d): %v", p.Exe, p.PID, err)
			}
		}
		if alive := cl.Wait(t, Wait); len(alive) > 0 {
			e.logf("не вышли за %v: %v", Wait, pids(alive))
		}
		if !Delete(c, e) {
			failed++
			e.logf("%s: программа осталась", c.Dir)
		}
		if taskExe != "" && same(filepath.Dir(taskExe), c.Dir) && retarget != nil {
			exe := filepath.Join(inst, ExeName)
			if err := retarget(exe); err != nil {
				e.logf("автозапуск не перенесён на %s: %v", exe, err)
			} else {
				e.logf("автозапуск перенесён на %s", exe)
			}
		}
	}
	return failed
}

func pids(ps []Proc) string {
	var b strings.Builder
	for i, p := range ps {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s (%d)", filepath.Base(p.Exe), p.PID)
	}
	return b.String()
}
