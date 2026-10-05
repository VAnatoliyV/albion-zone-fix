//go:build !windows

package bypass

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Заглушка winws: печатает аргументы и ждёт, либо падает, если в аргументах «die».
func fakeWinws(t *testing.T) string {
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"args: $*\"\ncase \"$*\" in *die*) echo 'error: boom'; exit 3;; esac\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "winws.exe"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func waitFor(t *testing.T, cond func() bool) {
	for i := 0; i < 50; i++ {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("не дождались")
}

func TestRunnerStartSwitchStop(t *testing.T) {
	r := NewRunner(fakeWinws(t))
	if err := r.Start(Strategy{Name: "a", Args: []string{"--x"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, _, tail := r.Status(); return len(tail) > 0 })
	_, _, tail := r.Status()
	if !strings.Contains(tail[0], "--wf-udp=5055,5056") {
		t.Fatalf("аргументы: %v", tail)
	}
	r.Start(Strategy{Name: "b", Args: []string{"--y"}})
	if r.Current() != "b" {
		t.Fatalf("текущая %s", r.Current())
	}
	r.Stop()
	if r.Current() != "off" {
		t.Fatal("не выключился")
	}
	time.Sleep(100 * time.Millisecond)
	if _, err, _ := r.Status(); err != "" {
		t.Fatalf("наша остановка записана как ошибка: %s", err)
	}
}

func TestRunnerNoticesCrash(t *testing.T) {
	r := NewRunner(fakeWinws(t))
	r.Start(Strategy{Name: "a", Args: []string{"die"}})
	waitFor(t, func() bool { return r.Current() == "off" })
	_, err, _ := r.Status()
	if !strings.Contains(err, "boom") {
		t.Fatalf("причина не видна: %q", err)
	}
}

func TestRunnerMissingBinary(t *testing.T) {
	r := NewRunner(t.TempDir())
	if err := r.Start(Strategy{Name: "a"}); err == nil || r.Current() != "off" {
		t.Fatal("отсутствие winws не замечено")
	}
}
