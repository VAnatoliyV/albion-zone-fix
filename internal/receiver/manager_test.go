package receiver

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Подставной приёмник: тест запускает сам себя с FAKE_RECEIVER=...; настоящий
// acp-prices.exe для Manager неотличим (порт, -data, страница /status).
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_RECEIVER"); mode != "" {
		port := flag.String("port", "", "")
		data := flag.String("data", "", "")
		flag.Parse()
		os.WriteFile(filepath.Join(*data, "fake-args.txt"), []byte(strings.Join(os.Args[1:], " ")), 0644)
		if mode == "crash" {
			fmt.Println("Не смог занять порт")
			os.Exit(1)
		}
		http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "<h1>Свои цены собираются</h1>")
		})
		ln, err := net.Listen("tcp", "127.0.0.1:"+*port)
		if err != nil {
			os.Exit(2)
		}
		fmt.Println("Сайт готов")
		http.Serve(ln, nil)
		return
	}
	os.Exit(m.Run())
}

func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func newMgr(t *testing.T, mode string) *Manager {
	exe, _ := os.Executable()
	m := NewManager(exe, t.TempDir(), nil)
	m.Addr = freeAddr(t)
	m.Env = []string{"FAKE_RECEIVER=" + mode}
	t.Cleanup(m.Stop)
	return m
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}

func TestStartStop(t *testing.T) {
	m := newMgr(t, "ok")
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "порт открыт", m.Up)
	if m.Err() != "" {
		t.Fatalf("ошибка: %s", m.Err())
	}
	args, _ := os.ReadFile(filepath.Join(m.DataDir, "fake-args.txt"))
	_, port, _ := net.SplitHostPort(m.Addr)
	if got := string(args); got != "-port "+port+" -data "+m.DataDir || strings.Contains(got, "-lan") {
		t.Fatalf("аргументы: %q", got)
	}
	if _, err := os.Stat(filepath.Join(m.DataDir, PidFile)); err != nil {
		t.Fatal("нет pid-файла")
	}
	if err := m.Start(); err != nil { // второй раз — без второго процесса
		t.Fatal(err)
	}
	m.Stop()
	if m.Up() {
		t.Fatal("после Stop порт всё ещё открыт")
	}
	if _, err := os.Stat(filepath.Join(m.DataDir, PidFile)); err == nil {
		t.Fatal("pid-файл остался")
	}
}

func TestBusyByStranger(t *testing.T) {
	m := newMgr(t, "ok")
	ln, err := net.Listen("tcp", m.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "чужой сервис") }))
	if err := m.Start(); err != ErrBusy {
		t.Fatalf("ждали ErrBusy, получили %v", err)
	}
	_, port, _ := net.SplitHostPort(m.Addr)
	if !strings.Contains(m.Err(), "занят") || !strings.Contains(m.Err(), port) {
		t.Fatalf("причина: %q", m.Err())
	}
	m.Stop() // чужое не трогаем
	if !m.Up() {
		t.Fatal("Stop обрушил чужую программу")
	}
}

func TestMissingExe(t *testing.T) {
	m := newMgr(t, "ok")
	m.Exe = filepath.Join(t.TempDir(), "нет.exe")
	if err := m.Start(); err == nil || !strings.Contains(m.Err(), "нет файла") {
		t.Fatalf("err=%v, Err=%q", err, m.Err())
	}
}

func TestCrashIsReported(t *testing.T) {
	m := newMgr(t, "crash")
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "причина падения", func() bool { return m.Err() != "" })
	if !strings.Contains(m.Err(), "завершился") {
		t.Fatalf("причина: %q", m.Err())
	}
	// После падения можно запустить снова.
	m.Env = []string{"FAKE_RECEIVER=ok"}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "порт открыт", m.Up)
	if m.Err() != "" {
		t.Fatalf("ошибка не сброшена: %s", m.Err())
	}
}

// Приёмник остался от прошлого запуска (выход при выключенном «останавливать
// всё»): новый Manager его подхватывает и по pid может остановить.
func TestAdoptLeftoverAndStopByPid(t *testing.T) {
	m1 := newMgr(t, "ok")
	m1.Keep(true)
	if err := m1.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "порт открыт", m1.Up)
	m1.mu.Lock()
	jobbed := m1.jobbed
	m1.mu.Unlock()
	if jobbed {
		t.Fatal("при Keep приёмник не должен лежать в объекте задания")
	}

	m2 := NewManager(m1.Exe, m1.DataDir, nil)
	m2.Addr = m1.Addr
	m2.inspect = func(int) (string, int64, bool) { return m1.Exe, 0, true } // не-Windows процессов не видит
	if err := m2.Start(); err != nil || m2.Err() != "" {
		t.Fatalf("подхват: %v %q", err, m2.Err())
	}
	m2.Stop()
	if m2.Up() {
		t.Fatal("оставшийся приёмник не остановлен по pid")
	}
}

func TestKeepChangeRestarts(t *testing.T) {
	m := newMgr(t, "ok")
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "порт открыт", m.Up)
	m.mu.Lock()
	first, jobbed := m.cmd, m.jobbed
	m.mu.Unlock()
	if !jobbed {
		t.Fatal("по умолчанию приёмник в объекте задания")
	}
	m.Keep(true)
	waitFor(t, "перезапуск", m.Up)
	m.mu.Lock()
	second, j2 := m.cmd, m.jobbed
	m.mu.Unlock()
	if second == nil || second == first || j2 {
		t.Fatalf("перезапуска без объекта задания не было: %v %v", second == first, j2)
	}
}

func TestPidVerdict(t *testing.T) {
	exe := `C:\AJ\acp-prices.exe`
	cases := []struct {
		image        string
		created, rec int64
		want         bool
	}{
		{`c:\aj\ACP-PRICES.exe`, 5, 5, true},    // регистр не важен
		{`C:\AJ\acp-prices.exe`, 5, 0, true},    // время не записано — по пути
		{`C:\Windows\notepad.exe`, 5, 5, false}, // pid достался чужому
		{`C:\AJ\acp-prices.exe`, 9, 5, false},   // тот же exe, но другой запуск
	}
	for _, c := range cases {
		if got := pidVerdict(exe, c.image, c.created, c.rec); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestStopByPidChecksIdentity(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "acp-prices.exe")
	var killed []int
	mk := func(image string, created int64, alive bool) *Manager {
		m := NewManager(exe, dir, nil)
		m.Addr = freeAddr(t)
		m.inspect = func(int) (string, int64, bool) { return image, created, alive }
		m.kill = func(p int) { killed = append(killed, p) }
		return m
	}
	pidPath := filepath.Join(dir, PidFile)

	os.WriteFile(pidPath, []byte("4242 77"), 0644)
	mk(`C:\Windows\notepad.exe`, 77, true).Stop()
	if len(killed) != 0 {
		t.Fatal("убит чужой процесс с переиспользованным pid")
	}
	if _, err := os.Stat(pidPath); err == nil {
		t.Fatal("устаревший pid-файл остался")
	}

	os.WriteFile(pidPath, []byte("4242 77"), 0644)
	mk(exe, 78, true).Stop() // тот же exe, но другой запуск
	if len(killed) != 0 {
		t.Fatal("убит процесс с другим временем создания")
	}

	os.WriteFile(pidPath, []byte("4242 77"), 0644)
	mk(exe, 0, false).Stop() // процесса нет
	if len(killed) != 0 {
		t.Fatal("убит несуществующий")
	}

	os.WriteFile(pidPath, []byte("4242 77"), 0644)
	mk(exe, 77, true).Stop()
	if len(killed) != 1 || killed[0] != 4242 {
		t.Fatalf("свой процесс не остановлен: %v", killed)
	}

	os.WriteFile(pidPath, []byte("мусор"), 0644)
	mk(exe, 77, true).Stop()
	if len(killed) != 1 {
		t.Fatal("убит по мусорному pid-файлу")
	}
}

func TestAdoptDropsForeignPidFile(t *testing.T) {
	m1 := newMgr(t, "ok")
	m1.Keep(true)
	if err := m1.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "порт открыт", m1.Up)
	m2 := NewManager(m1.Exe, m1.DataDir, nil)
	m2.Addr = m1.Addr
	m2.inspect = func(int) (string, int64, bool) { return `C:\other.exe`, 1, true } // pid уже чужой
	if err := m2.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m1.DataDir, PidFile)); err == nil {
		t.Fatal("чужой pid-файл не удалён при подхвате")
	}
}
