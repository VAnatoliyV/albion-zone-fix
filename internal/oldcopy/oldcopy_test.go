package oldcopy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func init() { retryDelay = time.Millisecond }

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
}

// release — папка как из zip: набор, памятки, zapret\bin.
func release(t *testing.T, dir string) {
	t.Helper()
	for _, f := range []string{ExeName, RecvName, ItemsName, "README-RU.txt", "LICENSES.txt"} {
		touch(t, filepath.Join(dir, f))
	}
	touch(t, filepath.Join(dir, ZapretDir, "bin", BypassName))
	touch(t, filepath.Join(dir, ZapretDir, "bin", "WinDivert64.sys"))
	for _, f := range ocrFiles {
		touch(t, filepath.Join(dir, OCRDir, f))
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// root — временная папка (с раскрытыми ссылками: на маке /var → /private/var).
func root(t *testing.T) string {
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func procEnv(ps ...Proc) Env {
	return Env{Procs: func() ([]Proc, error) { return ps, nil }, Self: 1}
}

func dirs(cs []Copy) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Dir)
	}
	sort.Strings(out)
	return out
}

func TestFindOursOnly(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Program Files", "Albion Journal")
	release(t, inst)
	old := filepath.Join(r, "Загрузки", "AlbionJournal", "AlbionJournal")
	release(t, old)
	// Чужой AlbionJournal.exe без набора (другая программа с тем же именем).
	foreign := filepath.Join(r, "чужое")
	touch(t, filepath.Join(foreign, ExeName))
	touch(t, filepath.Join(foreign, RecvName)) // без items_by_id.json

	e := procEnv(
		Proc{PID: 10, Name: ExeName, Exe: filepath.Join(old, ExeName)},
		Proc{PID: 11, Name: ExeName, Exe: filepath.Join(foreign, ExeName)},
		Proc{PID: 12, Name: ExeName, Exe: filepath.Join(inst, ExeName)},
		Proc{PID: 13, Name: RecvName, Exe: filepath.Join(r, "другое", RecvName)}, // не AlbionJournal.exe
	)
	var log []string
	e.Logf = func(f string, a ...any) { log = append(log, f) }
	got := Find(inst, e)
	if len(got) != 1 || got[0].Dir != old || !got[0].Full {
		t.Fatalf("Find: %+v", got)
	}
	if !strings.Contains(strings.Join(log, "|"), "не трогаю") {
		t.Fatalf("причина пропуска чужой папки не в журнале: %v", log)
	}
}

func TestFindInstallDirCaseAndSlash(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	release(t, inst)
	// Та же папка: другой регистр, хвостовой разделитель, «..».
	variants := []string{
		strings.ToUpper(inst),
		inst + string(filepath.Separator),
		filepath.Join(inst, "zapret", "..") + string(filepath.Separator),
	}
	for _, v := range variants {
		e := Env{TaskExe: func() (string, bool) { return filepath.Join(v, ExeName), true }}
		if got := Find(inst, e); len(got) != 0 {
			t.Fatalf("%q принят за старую копию: %+v", v, got)
		}
		// И наоборот: папка установки передана с хвостом.
		if got := Find(v, Env{TaskExe: func() (string, bool) { return filepath.Join(inst, ExeName), true }}); len(got) != 0 {
			t.Fatalf("inst %q: %+v", v, got)
		}
	}
}

func TestFindSymlinkToInstall(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	release(t, inst)
	link := filepath.Join(r, "ссылка")
	if err := os.Symlink(inst, link); err != nil {
		t.Skip("ссылки недоступны:", err)
	}
	e := Env{TaskExe: func() (string, bool) { return filepath.Join(link, ExeName), true }}
	if got := Find(inst, e); len(got) != 0 {
		t.Fatalf("ссылка на папку установки — старая копия: %+v", got)
	}
}

func TestFindNestingRootSystem(t *testing.T) {
	r := root(t)
	// Папка установки внутри старой копии и старая копия внутри установки.
	outer := filepath.Join(r, "Игры")
	release(t, outer)
	inst := filepath.Join(outer, "Albion Journal")
	release(t, inst)
	inner := filepath.Join(inst, "старая")
	release(t, inner)
	sys := filepath.Join(r, "Program Files")
	release(t, sys)
	win := filepath.Join(r, "Windows")
	release(t, filepath.Join(win, "x"))

	e := procEnv(
		Proc{PID: 2, Exe: filepath.Join(outer, ExeName)},
		Proc{PID: 3, Exe: filepath.Join(inner, ExeName)},
		Proc{PID: 4, Exe: filepath.Join(sys, ExeName)},
		Proc{PID: 5, Exe: filepath.Join(win, "x", ExeName)},
		Proc{PID: 6, Exe: string(filepath.Separator) + ExeName}, // корень
		Proc{PID: 7, Exe: ExeName},                              // не полный путь
	)
	e.Keep = []string{sys}
	e.Under = []string{win}
	if got := Find(inst, e); len(got) != 0 {
		t.Fatalf("Find: %+v", got)
	}
	if !isRoot(string(filepath.Separator)) || isRoot(r) {
		t.Fatal("isRoot")
	}
}

func TestFindDuplicatesProcessAndTask(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	old := filepath.Join(r, "AlbionJournal")
	release(t, old)
	e := procEnv(
		Proc{PID: 2, Exe: filepath.Join(old, ExeName)},
		Proc{PID: 3, Exe: filepath.Join(strings.ToUpper(old), ExeName)},
	)
	e.TaskExe = func() (string, bool) { return `"` + filepath.Join(old, ExeName) + `"`, true }
	got := Find(inst, e)
	if len(got) != 1 || got[0].Dir != old {
		t.Fatalf("дубли: %+v", got)
	}
	// Только задача (путь в кавычках), процесса нет.
	got = Find(inst, Env{TaskExe: func() (string, bool) { return `"` + filepath.Join(old, ExeName) + `"`, true }})
	if len(got) != 1 {
		t.Fatalf("только задача: %+v", got)
	}
}

func TestFindFullOrShared(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	old := filepath.Join(r, "Загрузки")
	release(t, old)
	touch(t, filepath.Join(old, ExeName+".new"))
	touch(t, filepath.Join(old, "TESTER-RU.txt"))
	touch(t, filepath.Join(old, "uninstall.exe"))
	e := Env{TaskExe: func() (string, bool) { return filepath.Join(old, ExeName), true }}
	if got := Find(inst, e); len(got) != 1 || !got[0].Full {
		t.Fatalf("только наше: %+v", got)
	}
	touch(t, filepath.Join(old, "отпуск.jpg"))
	if got := Find(inst, e); len(got) != 1 || got[0].Full {
		t.Fatalf("с чужим файлом: %+v", got)
	}
	// Папка zapret ссылкой — не наша.
	os.Remove(filepath.Join(old, "отпуск.jpg"))
	os.RemoveAll(filepath.Join(old, ZapretDir))
	if err := os.Symlink(r, filepath.Join(old, ZapretDir)); err == nil {
		if got := Find(inst, e); len(got) != 1 || got[0].Full {
			t.Fatalf("zapret ссылкой: %+v", got)
		}
	}
}

func TestTargets(t *testing.T) {
	r := root(t)
	old := filepath.Join(r, "Загрузки", "AlbionJournal")
	inst := filepath.Join(r, "Albion Journal")
	c := Copy{Dir: old, Full: true}
	ps := []Proc{
		{PID: 1, Name: ExeName, Exe: filepath.Join(old, ExeName)},                         // сам себе — нет
		{PID: 2, Name: ExeName, Exe: filepath.Join(old, ExeName)},                         // да
		{PID: 3, Name: RecvName, Exe: filepath.Join(old, RecvName)},                       // да
		{PID: 4, Name: BypassName, Exe: filepath.Join(old, ZapretDir, "bin", BypassName)}, // да
		{PID: 5, Name: BypassName, Exe: filepath.Join(old, BypassName)},                   // winws не из zapret\bin
		{PID: 6, Name: "AlbionJournalSetup-1.0.5.exe", Exe: filepath.Join(old, "AlbionJournalSetup-1.0.5.exe")},
		{PID: 7, Name: "Albion-Online.exe", Exe: filepath.Join(old, "Albion-Online.exe")},
		{PID: 8, Name: ExeName, Exe: filepath.Join(inst, ExeName)},                              // новая копия
		{PID: 9, Name: ExeName, Exe: filepath.Join(old, "sub", ExeName)},                        // не ровно в папке
		{PID: 10, Name: RecvName, Exe: filepath.Join(filepath.Dir(old), RecvName)},              // папка выше
		{PID: 11, Name: ExeName, Exe: filepath.Join(strings.ToUpper(old), "albionjournal.EXE")}, // регистр
	}
	got := Targets(c, ps, 1)
	var ids []uint32
	for _, p := range got {
		ids = append(ids, p.PID)
	}
	if fmt.Sprint(ids) != "[2 11 3 4]" {
		t.Fatalf("Targets: %v", ids)
	}
	// Папка не целиком наша — winws не трогаем.
	c.Full = false
	for _, p := range Targets(c, ps, 1) {
		if p.PID == 4 {
			t.Fatal("winws закрыт в общей папке")
		}
	}
}

func TestDeleteFullCopyAndParent(t *testing.T) {
	r := root(t)
	parent := filepath.Join(r, "Загрузки", "AlbionJournal")
	old := filepath.Join(parent, "AlbionJournal")
	release(t, old)
	touch(t, filepath.Join(old, ExeName+".new"))
	touch(t, filepath.Join(old, ZapretDir, "bin", "cygwin1.dll.new"))
	touch(t, filepath.Join(r, "Загрузки", "фото.jpg"))
	c := Copy{Dir: old, Full: true}
	if !Delete(c, Env{}) {
		t.Fatal("exe остался")
	}
	if exists(old) || exists(parent) {
		t.Fatal("пустые папки остались")
	}
	if !exists(filepath.Join(r, "Загрузки", "фото.jpg")) {
		t.Fatal("удалено чужое выше папки")
	}
}

func TestDeleteKeepsForeign(t *testing.T) {
	r := root(t)
	old := filepath.Join(r, "Игры", "Журнал")
	release(t, old)
	touch(t, filepath.Join(old, ZapretDir, "lists", "мои.txt")) // своё внутри zapret
	c := Copy{Dir: old, Full: true}
	Delete(c, Env{})
	if exists(filepath.Join(old, ExeName)) || exists(filepath.Join(old, ZapretDir, "bin")) {
		t.Fatal("наше не удалено")
	}
	if !exists(filepath.Join(old, ZapretDir, "lists", "мои.txt")) {
		t.Fatal("удалён чужой файл в zapret")
	}

	// Общая папка: только набор, остальное на месте, папка остаётся.
	shared := filepath.Join(r, "Downloads")
	release(t, shared)
	touch(t, filepath.Join(shared, "чужое.exe"))
	touch(t, filepath.Join(shared, RecvName+".old"))
	if !Delete(Copy{Dir: shared, Full: false}, Env{}) {
		t.Fatal("exe остался")
	}
	for _, f := range []string{ExeName, RecvName, ItemsName, RecvName + ".old"} {
		if exists(filepath.Join(shared, f)) {
			t.Fatalf("%s не удалён", f)
		}
	}
	for _, f := range []string{"чужое.exe", "README-RU.txt", "LICENSES.txt", filepath.Join(ZapretDir, "bin", BypassName)} {
		if !exists(filepath.Join(shared, f)) {
			t.Fatalf("%s удалён в общей папке", f)
		}
	}
}

func TestDeleteParentOnlyIfNamedAndEmpty(t *testing.T) {
	r := root(t)
	parent := filepath.Join(r, "Распаковано")
	old := filepath.Join(parent, "AlbionJournal")
	release(t, old)
	Delete(Copy{Dir: old, Full: true}, Env{})
	if exists(old) || !exists(parent) {
		t.Fatal("родитель с другим именем удалён или копия осталась")
	}
	parent = filepath.Join(r, "AlbionJournal")
	old = filepath.Join(parent, "AlbionJournal")
	release(t, old)
	touch(t, filepath.Join(parent, "заметка.txt"))
	Delete(Copy{Dir: old, Full: true}, Env{})
	if !exists(filepath.Join(parent, "заметка.txt")) {
		t.Fatal("непустой родитель удалён")
	}
}

type fakeCloser struct{ soft, killed []uint32 }

func (f *fakeCloser) Soft(p Proc)       { f.soft = append(f.soft, p.PID) }
func (f *fakeCloser) Kill(p Proc) error { f.killed = append(f.killed, p.PID); return nil }
func (f *fakeCloser) Wait(ps []Proc, _ time.Duration) []Proc {
	return nil
}

func TestRemove(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Program Files", "Albion Journal")
	release(t, inst)
	old := filepath.Join(r, "Пользователи", "Иван", "Загрузки", "AlbionJournal", "AlbionJournal")
	release(t, old)
	ps := []Proc{
		{PID: 20, Name: ExeName, Exe: filepath.Join(old, ExeName)},
		{PID: 21, Name: RecvName, Exe: filepath.Join(old, RecvName)},
		{PID: 22, Name: BypassName, Exe: filepath.Join(old, ZapretDir, "bin", BypassName)},
		{PID: 23, Name: ExeName, Exe: filepath.Join(inst, ExeName)},
		{PID: 24, Name: RecvName, Exe: filepath.Join(inst, RecvName)},
	}
	e := procEnv(ps...)
	e.Self = 23
	e.TaskExe = func() (string, bool) { return filepath.Join(old, ExeName), true }
	var moved string
	cl := &fakeCloser{}
	if n := Remove(inst, e, cl, func(exe string) error { moved = exe; return nil }); n != 0 {
		t.Fatalf("не убрано: %d", n)
	}
	if len(cl.soft) != 1 || cl.soft[0] != 20 {
		t.Fatalf("мягко: %v", cl.soft)
	}
	if fmt.Sprint(cl.killed) != "[20 21 22]" {
		t.Fatalf("закрыты: %v", cl.killed)
	}
	if exists(filepath.Dir(old)) {
		t.Fatal("старая копия осталась")
	}
	if !exists(filepath.Join(inst, ExeName)) || !exists(filepath.Join(inst, ZapretDir, "bin", BypassName)) {
		t.Fatal("задета папка установки")
	}
	if moved != filepath.Join(inst, ExeName) {
		t.Fatalf("автозапуск: %q", moved)
	}
	// Повторно — искать нечего.
	if got := Find(inst, e); len(got) != 0 {
		t.Fatalf("после удаления: %+v", got)
	}
}

func TestRemoveTaskElsewhereNotRetargeted(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	old := filepath.Join(r, "Старая")
	release(t, old)
	e := procEnv(Proc{PID: 5, Name: ExeName, Exe: filepath.Join(old, ExeName)})
	e.TaskExe = func() (string, bool) { return filepath.Join(inst, ExeName), true }
	called := false
	Remove(inst, e, &fakeCloser{}, func(string) error { called = true; return nil })
	if called {
		t.Fatal("задача на новую копию переписана зря")
	}
}

// Fix round 1.

// noResolve — как junction на Windows: EvalSymlinks его не раскрывает.
func noResolve(t *testing.T) {
	old := resolve
	resolve = func(p string) (string, error) { return p, nil }
	t.Cleanup(func() { resolve = old })
}

func TestFindInstallAliasedLikeJunction(t *testing.T) {
	noResolve(t)
	r := root(t)
	real := filepath.Join(r, "D", "Games", "Albion Journal")
	release(t, real)
	touch(t, filepath.Join(real, "uninstall.exe"))
	alias := filepath.Join(r, "C", "Games")
	os.MkdirAll(filepath.Dir(alias), 0755)
	if err := os.Symlink(filepath.Join(r, "D", "Games"), alias); err != nil {
		t.Skip(err)
	}
	inst := filepath.Join(alias, "Albion Journal") // как $INSTDIR
	// Свой процесс (-find-old) и задача — по «настоящему» пути.
	e := procEnv(Proc{PID: 1, Name: ExeName, Exe: filepath.Join(real, ExeName)})
	e.TaskExe = func() (string, bool) { return filepath.Join(real, ExeName), true }
	if got := Find(inst, e); len(got) != 0 {
		t.Fatalf("новая установка под другим путём принята за старую: %+v", got)
	}
	// Даже без Self: тот же каталог по идентичности файла.
	e.Self = 99
	if got := Find(inst, e); len(got) != 0 {
		t.Fatalf("SameFile: %+v", got)
	}
	// Вложенность тоже по идентичности: старая копия внутри установки.
	inner := filepath.Join(real, "sub")
	release(t, inner)
	e.TaskExe = func() (string, bool) { return filepath.Join(inner, ExeName), true }
	e.Procs = nil
	if got := Find(inst, e); len(got) != 0 {
		t.Fatalf("внутри установки по другому пути: %+v", got)
	}
}

func TestFindSkipsSelfExe(t *testing.T) {
	noResolve(t)
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	other := filepath.Join(r, "где-то")
	release(t, other)
	e := procEnv(Proc{PID: 1, Name: ExeName, Exe: filepath.Join(other, ExeName)}) // Self
	if got := Find(inst, e); len(got) != 0 {
		t.Fatalf("свой процесс — кандидат: %+v", got)
	}
	e = Env{TaskExe: func() (string, bool) { return filepath.Join(other, ExeName), true }, SelfExe: filepath.Join(other, ExeName)}
	if got := Find(inst, e); len(got) != 0 {
		t.Fatalf("папка своего exe — кандидат: %+v", got)
	}
}

func TestZapretOnlyShippedNames(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	old := filepath.Join(r, "Tools")
	release(t, old)
	e := Env{TaskExe: func() (string, bool) { return filepath.Join(old, ExeName), true }}
	if got := Find(inst, e); len(got) != 1 || !got[0].Full {
		t.Fatalf("наш zapret: %+v", got)
	}
	// Свой zapret пользователя: чужой файл в bin — папка не целиком наша.
	touch(t, filepath.Join(old, ZapretDir, "bin", "my-fake.bin"))
	got := Find(inst, e)
	if len(got) != 1 || got[0].Full {
		t.Fatalf("чужой файл в zapret\\bin: %+v", got)
	}
	os.Remove(filepath.Join(old, ZapretDir, "bin", "my-fake.bin"))
	// Чужая папка рядом с bin.
	touch(t, filepath.Join(old, ZapretDir, "lists", "list.txt"))
	if got := Find(inst, e); len(got) != 1 || got[0].Full {
		t.Fatalf("zapret\\lists: %+v", got)
	}
	// Plan удаляет из bin только наши имена, даже при Full.
	c := Copy{Dir: old, Full: true}
	touch(t, filepath.Join(old, ZapretDir, "bin", "my-fake.bin"))
	files, _ := Plan(c)
	for _, f := range files {
		if filepath.Base(f) == "my-fake.bin" {
			t.Fatal("в плане чужой файл zapret\\bin")
		}
	}
}

func TestZapretBinLinkNotFollowed(t *testing.T) {
	r := root(t)
	old := filepath.Join(r, "Old")
	release(t, old)
	os.RemoveAll(filepath.Join(old, ZapretDir, "bin"))
	userBin := filepath.Join(r, "user-zapret", "bin")
	touch(t, filepath.Join(userBin, BypassName))
	if err := os.Symlink(userBin, filepath.Join(old, ZapretDir, "bin")); err != nil {
		t.Skip(err)
	}
	Delete(Copy{Dir: old, Full: true}, Env{})
	if !exists(filepath.Join(userBin, BypassName)) {
		t.Fatal("удалено по ссылке zapret\\bin")
	}
}

func TestDeleteKeepsWellKnownDir(t *testing.T) {
	r := root(t)
	dl := filepath.Join(r, "Downloads")
	release(t, dl)
	Delete(Copy{Dir: dl, Full: true}, Env{})
	if !exists(dl) {
		t.Fatal("удалена сама папка «Загрузки»")
	}
	for _, name := range []string{"AlbionJournal", "Albion Journal"} {
		d := filepath.Join(r, "x", name)
		release(t, d)
		Delete(Copy{Dir: d, Full: true}, Env{})
		if exists(d) {
			t.Fatalf("%s не удалена", name)
		}
	}
}

type orderCloser struct{ log []string }

func (o *orderCloser) Soft(p Proc)       { o.log = append(o.log, "soft") }
func (o *orderCloser) Kill(p Proc) error { o.log = append(o.log, "kill"); return nil }
func (o *orderCloser) Wait(ps []Proc, d time.Duration) []Proc {
	o.log = append(o.log, "wait "+d.String())
	return nil
}

func TestRemoveGraceAfterSoft(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	old := filepath.Join(r, "AlbionJournal")
	release(t, old)
	e := procEnv(Proc{PID: 5, Name: ExeName, Exe: filepath.Join(old, ExeName)})
	cl := &orderCloser{}
	Remove(inst, e, cl, nil)
	if strings.Join(cl.log, ",") != "soft,wait "+SoftGrace.String()+",kill,wait "+Wait.String() {
		t.Fatalf("порядок: %v", cl.log)
	}
}

func TestFindByOrphanReceiver(t *testing.T) {
	r := root(t)
	inst := filepath.Join(r, "Albion Journal")
	old := filepath.Join(r, "AlbionJournal")
	release(t, old)
	e := procEnv(Proc{PID: 7, Name: RecvName, Exe: filepath.Join(old, RecvName)})
	if got := Find(inst, e); len(got) != 1 {
		t.Fatalf("приёмник без программы: %+v", got)
	}
}

// Папка ocr из выпуска — наша; чужой файл в ней — папка не целиком наша.
func TestOCRDir(t *testing.T) {
	r := root(t)
	old := filepath.Join(r, "AlbionJournal")
	release(t, old)
	if !onlyOurs(old) {
		t.Fatal("выпуск с ocr не наш")
	}
	files, dirs := Plan(Copy{Dir: old, Full: true})
	n := 0
	for _, f := range files {
		if filepath.Base(filepath.Dir(f)) == OCRDir {
			n++
		}
	}
	if n != len(ocrFiles) || len(dirs) == 0 {
		t.Errorf("в плане %d файлов ocr, папки %v", n, dirs)
	}
	touch(t, filepath.Join(old, OCRDir, "моё.onnx"))
	if onlyOurs(old) {
		t.Error("чужой файл в ocr — а папка наша")
	}
}

// Список ocrFiles совпадает с тем, что кладёт собрать.sh (ocr/files.txt и
// models-LICENSE.txt).
func TestOCRFilesMatchBuild(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "ocr", "files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"models-LICENSE.txt"}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) > 0 && !strings.HasPrefix(f[0], "#") {
			want = append(want, f[0])
		}
	}
	got := append([]string(nil), ocrFiles...)
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ocrFiles %v, в выпуске %v", got, want)
	}
}
