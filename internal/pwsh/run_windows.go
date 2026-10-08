//go:build windows

package pwsh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"albionzonefix/internal/procutil"

	"golang.org/x/sys/windows"
)

const supported = true

// PowerShell — полный путь к Windows PowerShell 5.1 (не поиск по PATH:
// программа с правами администратора не должна запускать первый попавшийся
// powershell.exe). Программа 64-битная, System32 — настоящий.
func PowerShell() string {
	dir, err := windows.GetSystemDirectory()
	if err != nil || dir == "" {
		dir = filepath.Join(os.Getenv("SystemRoot"), "System32")
	}
	return filepath.Join(dir, "WindowsPowerShell", "v1.0", "powershell.exe")
}

func command(ctx context.Context) *exec.Cmd {
	if ctx == nil {
		return exec.Command(PowerShell(), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "-")
	}
	return exec.CommandContext(ctx, PowerShell(), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "-")
}

// RunScript запускает разовый PowerShell со скриптом script (через
// стандартный ввод) и переменными env, без окна, в объекте задания
// программы, с таймаутом (процесс убивается). Отдаёт стандартный вывод.
func RunScript(ctx context.Context, script string, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := command(ctx)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(Stdin(script))
	procutil.Hide(cmd)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	procutil.BindToJob(cmd)
	err := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return []byte(out.String()), errors.New("PowerShell не ответил за отведённое время")
	}
	if err != nil {
		if e := strings.TrimSpace(errOut.String()); e != "" {
			if r := []rune(e); len(r) > 300 {
				e = string(r[:300]) + "…"
			}
			err = fmt.Errorf("%v: %s", err, e)
		}
	}
	return []byte(out.String()), err
}

// proc — рабочий powershell.exe.
type proc struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  io.ReadCloser
	mu   sync.Mutex
	tail []byte // последние байты stderr
	once sync.Once
}

func (p *proc) Write(b []byte) (int, error) { return p.in.Write(b) }
func (p *proc) Read(b []byte) (int, error)  { return p.out.Read(b) }

func (p *proc) Tail() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.tail)
}

func (p *proc) Kill() {
	p.once.Do(func() {
		p.in.Close()
		p.cmd.Process.Kill()
		go p.cmd.Wait() // забрать процесс и закрыть трубы
	})
}

// startProc запускает рабочего: без окна, в объекте задания (не осиротеет
// при выходе программы), скрипт WorkerScript — первыми строками ввода.
func startProc() (Proc, error) {
	cmd := command(nil)
	procutil.Hide(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	procutil.BindToJob(cmd)
	p := &proc{cmd: cmd, in: in, out: out}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := errPipe.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.tail = append(p.tail, buf[:n]...)
				if len(p.tail) > 2048 {
					p.tail = append([]byte(nil), p.tail[len(p.tail)-2048:]...)
				}
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	// Скрипт больше буфера трубы: пишем в своей горутине, чтобы зависший
	// PowerShell не держал запуск (его поймает ReadyTimeout).
	go io.WriteString(in, Stdin(WorkerScript))
	return p, nil
}
