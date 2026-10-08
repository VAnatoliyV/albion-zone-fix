//go:build windows

package ocr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"albionzonefix/internal/procutil"

	"golang.org/x/sys/windows"
)

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

// RunScript запускает PowerShell со скриптом script и переменными env,
// без окна, в объекте задания программы, с таймаутом (процесс убивается).
// Отдаёт стандартный вывод.
func RunScript(ctx context.Context, script string, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, PowerShell(), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", Encode(script))
	cmd.Env = append(os.Environ(), env...)
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

// Languages — установленные языки OCR Windows.
func Languages(ctx context.Context) ([]string, error) {
	b, err := RunScript(ctx, Script, []string{"AJ_OCR_PATH=", "AJ_OCR_LANGS="})
	o := Parse(b)
	if o.Err != "" {
		return o.Langs, errors.New(o.Err)
	}
	if err != nil && len(o.Langs) == 0 {
		return nil, err
	}
	return o.Langs, nil
}

// Recognize распознаёт PNG на языках langs; ответ — язык → строки.
func Recognize(ctx context.Context, png string, langs []string) (map[string][]string, error) {
	b, err := RunScript(ctx, Script, []string{"AJ_OCR_PATH=" + png, "AJ_OCR_LANGS=" + strings.Join(langs, ",")})
	o := Parse(b)
	if o.Err != "" {
		return o.Lines, errors.New(o.Err)
	}
	if err != nil && len(o.Lines) == 0 {
		return nil, err
	}
	return o.Lines, nil
}
