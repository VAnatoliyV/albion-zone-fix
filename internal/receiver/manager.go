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
}

// NewManager: exe — полный путь, dataDir — каталог данных.
func NewManager(exe, dataDir string, log io.Writer) *Manager {
	if log == nil {
		log = io.Discard
	}
	return &Manager{Exe: exe, DataDir: dataDir, Addr: Addr, Log: log}
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
	os.WriteFile(filepath.Join(m.DataDir, PidFile), []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
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
	b, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err == nil && pid > 0 && m.Up() && m.isOurs() {
		if p, err := os.FindProcess(pid); err == nil {
			p.Kill()
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
