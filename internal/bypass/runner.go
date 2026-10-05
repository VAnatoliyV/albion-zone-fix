package bypass

import (
	"bufio"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Runner держит один процесс winws. Включение новой стратегии гасит старую.
type Runner struct {
	binDir string // папка с winws.exe, с разделителем на конце

	mu      sync.Mutex
	cmd     *exec.Cmd
	cur     string // имя стратегии или "off"
	lastErr string
	tail    []string // последние строки вывода winws
}

func NewRunner(binDir string) *Runner {
	if !strings.HasSuffix(binDir, string(filepath.Separator)) {
		binDir += string(filepath.Separator)
	}
	return &Runner{binDir: binDir, cur: "off"}
}

func (r *Runner) Current() string { r.mu.Lock(); defer r.mu.Unlock(); return r.cur }

func (r *Runner) Status() (cur, lastErr string, tail []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur, r.lastErr, append([]string(nil), r.tail...)
}

func (r *Runner) Start(s Strategy) error {
	r.Stop()
	cmd := exec.Command(filepath.Join(r.binDir, "winws.exe"), Args(s, r.binDir)...)
	cmd.Dir = r.binDir
	hideWindow(cmd)
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		r.mu.Lock()
		r.lastErr = "winws не запустился: " + err.Error()
		r.mu.Unlock()
		return errors.New(r.lastErr)
	}
	bindToJob(cmd)
	r.mu.Lock()
	r.cmd, r.cur, r.lastErr, r.tail = cmd, s.Name, "", nil
	r.mu.Unlock()
	go r.watch(cmd, out)
	return nil
}

func (r *Runner) watch(cmd *exec.Cmd, out io.Reader) {
	if out != nil {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			r.mu.Lock()
			r.tail = append(r.tail, sc.Text())
			if len(r.tail) > 20 {
				r.tail = r.tail[len(r.tail)-20:]
			}
			r.mu.Unlock()
		}
	}
	err := cmd.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == cmd { // упал сам, а не мы погасили
		r.cmd, r.cur = nil, "off"
		msg := "winws завершился"
		if err != nil {
			msg += ": " + err.Error()
		}
		if len(r.tail) > 0 {
			msg += " — " + r.tail[len(r.tail)-1]
		}
		r.lastErr = msg
	}
}

func (r *Runner) Stop() {
	r.mu.Lock()
	cmd := r.cmd
	r.cmd, r.cur = nil, "off"
	r.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}
