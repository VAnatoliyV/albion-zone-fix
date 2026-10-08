package receiver

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"albionzonefix/internal/procutil"
)

// ExeName — приёмник рядом с программой.
const ExeName = "acp-prices.exe"

// PidFile — в каталоге данных: pid приёмника, который мы запустили. Нужен,
// чтобы после перезапуска программы остановить приёмник, оставшийся
// работать при выключенном «останавливать всё при выходе».
const PidFile = "acp-prices.pid"

// ourMarker — по этой строке страницы /status отличаем свой приёмник от
// чужой программы на том же порту.
const ourMarker = "Свои цены"

// ErrBusy — порт занят не нашим приёмником.
var ErrBusy = errors.New("порт приёмника занят другой программой")

// Manager запускает и останавливает acp-prices.exe.
//
// Живой ли приёмник — по TCP-порту (Up), а не по процессу: так виден и
// приёмник, оставшийся от прошлого запуска.
type Manager struct {
	Exe     string    // путь к acp-prices.exe
	DataDir string    // -data
	Addr    string    // 127.0.0.1:7777
	Log     io.Writer // куда писать вывод приёмника; *os.File наследуется без трубы
	Env     []string  // добавка к окружению (в тестах)

	mu      sync.Mutex
	cmd     *exec.Cmd
	jobbed  bool // запущен внутри объекта задания (умрёт с программой)
	keep    bool // оставлять работать после выхода
	lastErr string
	adopted bool // порт занят нашим приёмником не из этого запуска

	// Подменяются в тестах.
	inspect func(pid int) (image string, created int64, ok bool)
	kill    func(pid int)
}

// NewManager: exe — полный путь, dataDir — каталог данных.
func NewManager(exe, dataDir string, log io.Writer) *Manager {
	if log == nil {
		log = io.Discard
	}
	return &Manager{Exe: exe, DataDir: dataDir, Addr: Addr, Log: log,
		inspect: procutil.Inspect, kill: func(pid int) {
			if p, err := os.FindProcess(pid); err == nil {
				p.Kill()
			}
		}}
}

func (m *Manager) logf(format string, a ...any) {
	fmt.Fprintf(m.Log, "[программа] %s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
}

// writePid запоминает pid и время создания процесса: pid после перезагрузки
// или долгой работы мог достаться чужой программе.
func (m *Manager) writePid(pid int) {
	var created int64
	if _, c, ok := m.inspect(pid); ok {
		created = c
	}
	os.WriteFile(filepath.Join(m.DataDir, PidFile), []byte(fmt.Sprintf("%d %d", pid, created)), 0644)
}

// pidVerdict решает, можно ли убивать процесс из pid-файла: он должен
// быть нашим acp-prices.exe (путь совпадает, регистр не важен) и, если время
// создания записано, тем же самым запуском.
func pidVerdict(exe, image string, created, recCreated int64) bool {
	if !strings.EqualFold(filepath.Clean(image), filepath.Clean(exe)) {
		return false
	}
	return recCreated == 0 || recCreated == created
}

// leftoverPid читает pid-файл и проверяет процесс. found — файл был.
func (m *Manager) leftoverPid() (pid int, ok, found bool) {
	b, err := os.ReadFile(filepath.Join(m.DataDir, PidFile))
	if err != nil {
		return 0, false, false
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, false, true
	}
	pid, err = strconv.Atoi(f[0])
	if err != nil || pid <= 0 {
		return 0, false, true
	}
	var rec int64
	if len(f) > 1 {
		rec, _ = strconv.ParseInt(f[1], 10, 64)
	}
	image, created, alive := m.inspect(pid)
	return pid, alive && pidVerdict(m.Exe, image, created, rec), true
}

func (m *Manager) dropStalePid() {
	if _, ok, found := m.leftoverPid(); found && !ok {
		os.Remove(filepath.Join(m.DataDir, PidFile))
		m.logf("pid-файл приёмника не подходит (процесс чужой или исчез) — удалил")
	}
}

// Up — слушает ли порт кто-то.
func (m *Manager) Up() bool { return Up(m.Addr) }

// Err — последняя причина, почему приёмник не работает; пусто — всё хорошо.
func (m *Manager) Err() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

// Keep: оставлять ли приёмник работать после выхода из программы. Решает,
// класть ли его в объект задания при следующем запуске. Уже работающий
// приёмник с другим выбором перезапускается: из объекта задания процесс
// вынуть нельзя.
func (m *Manager) Keep(keep bool) {
	m.mu.Lock()
	changed := m.keep != keep
	m.keep = keep
	mismatch := changed && m.cmd != nil && m.jobbed == keep
	m.mu.Unlock()
	if mismatch {
		m.Stop()
		m.Start()
	}
}

// isOurs: на порту отвечает наш приёмник.
func (m *Manager) isOurs() bool {
	c := &http.Client{Timeout: 1500 * time.Millisecond}
	r, err := c.Get("http://" + m.Addr + "/status")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	return r.StatusCode == 200 && strings.Contains(string(b), ourMarker)
}

func (m *Manager) fail(format string, a ...any) error {
	err := fmt.Errorf(format, a...)
	m.mu.Lock()
	m.lastErr = err.Error()
	m.mu.Unlock()
	return err
}

// Start запускает приёмник. Уже работает наш — ничего не делает. Порт занят
// чужим — ErrBusy (и причина в Err).
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.cmd != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	_, port, err := net.SplitHostPort(m.Addr)
	if err != nil {
		return m.fail("адрес приёмника: %v", err)
	}
	if m.Up() {
		if m.isOurs() {
			m.dropStalePid()
			m.mu.Lock()
			m.adopted, m.lastErr = true, ""
			m.mu.Unlock()
			return nil
		}
		m.fail("порт %s занят другой программой — освободите его или закройте её", port)
		return ErrBusy
	}
	if _, err := os.Stat(m.Exe); err != nil {
		return m.fail("нет файла приёмника %s", filepath.Base(m.Exe))
	}
	if err := os.MkdirAll(m.DataDir, 0755); err != nil {
		return m.fail("каталог данных: %v", err)
	}

	// Без -lan: приёмник слушает только этот компьютер.
	cmd := exec.Command(m.Exe, "-port", port, "-data", m.DataDir)
	cmd.Dir = filepath.Dir(m.Exe)
	cmd.Env = append(os.Environ(), m.Env...)
	cmd.Stdout, cmd.Stderr = m.Log, m.Log
	m.mu.Lock()
	jobbed := !m.keep
	m.mu.Unlock()
	if jobbed {
		procutil.Hide(cmd)
	} else {
		procutil.Detach(cmd)
	}
	fmt.Fprintf(m.Log, "[программа] %s запуск приёмника: %s\n", time.Now().Format("2006-01-02 15:04:05"), m.Exe)
	if err := cmd.Start(); err != nil {
		return m.fail("приёмник не запустился: %v", err)
	}
	if jobbed {
		procutil.BindToJob(cmd)
	}
	m.writePid(cmd.Process.Pid)
	m.mu.Lock()
	m.cmd, m.jobbed, m.adopted, m.lastErr = cmd, jobbed, false, ""
	m.mu.Unlock()
	go m.watch(cmd)
	return nil
}

func (m *Manager) watch(cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != cmd { // погасили мы сами
		return
	}
	m.cmd = nil
	os.Remove(filepath.Join(m.DataDir, PidFile))
	msg := "приёмник завершился"
	if err != nil {
		msg += ": " + err.Error()
	}
	m.lastErr = msg
}

// Stop гасит приёмник: свой процесс, а если он остался с прошлого запуска и
// порт наш — по pid из файла. Чужую программу на порту не трогает.
func (m *Manager) Stop() {
	m.mu.Lock()
	cmd := m.cmd
	m.cmd, m.adopted, m.lastErr = nil, false, ""
	m.mu.Unlock()
	pidPath := filepath.Join(m.DataDir, PidFile)
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		os.Remove(pidPath)
		m.waitDown()
		return
	}
	if pid, ok, found := m.leftoverPid(); found {
		if ok {
			m.logf("останавливаю оставшийся приёмник, pid %d", pid)
			m.kill(pid)
		} else {
			m.logf("pid-файл приёмника не подходит (процесс чужой или исчез) — никого не трогаю")
		}
	}
	os.Remove(pidPath)
	m.waitDown()
}

// waitDown ждёт, пока порт освободится (не дольше 2 с).
func (m *Manager) waitDown() {
	for i := 0; i < 20 && m.Up(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
}
