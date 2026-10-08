package support

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanDropsGameLinesThenTakesTail(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("[программа] служебная %d", i))
		lines = append(lines, "Персонаж рядом: Vasya "+Game[i%len(Game)])
	}
	got := Clean(lines, 40)
	if len(got) != 40 || got[39] != "[программа] служебная 59" || got[0] != "[программа] служебная 20" {
		t.Fatalf("%d строк: %q … %q", len(got), got[0], got[len(got)-1])
	}
	for _, w := range Game {
		if got := Clean([]string{"x " + w + " y", "служебная"}, 40); len(got) != 1 || got[0] != "служебная" {
			t.Errorf("%q не выкинута: %v", w, got)
		}
	}
	if got := Clean([]string{"a\x1b[1;31mb\x1b[0mc\x1b[2K\r"}, 40); got[0] != "abc" {
		t.Fatalf("ANSI: %q", got[0])
	}
}

func TestAnonymize(t *testing.T) {
	home := `C:\Users\Анатолий`
	in := `данные: C:\Users\Анатолий\AppData\Roaming\Albion Journal; c:\users\анатолий\x; C:\Users\Other`
	got := Anonymize(in, home)
	if strings.Contains(got, "Анатолий") || strings.Contains(strings.ToLower(got), "анатолий") {
		t.Fatalf("имя осталось: %s", got)
	}
	if !strings.Contains(got, `~\AppData\Roaming`) || !strings.Contains(got, `C:\Users\Other`) {
		t.Fatalf("%s", got)
	}
	if Anonymize("abc", "") != "abc" || Anonymize("abc", `\`) != "abc" {
		t.Fatal("пустой дом")
	}
	if got := Anonymize(`C:\Users\Bob\x`, `C:\Users\Bob\`); got != `~\x` {
		t.Fatalf("слэш в конце: %s", got)
	}
}

func TestInfo(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home", "vasya")
	log := filepath.Join(dir, "albion-journal.log")
	content := "[программа] запуск, данные: " + home + "/AppData\n" +
		"[сборщик] Updating player Vasya GUID\n" +
		"[сборщик] урон 1234 от Petya\n" +
		"[программа] страница: http://127.0.0.1:5000/\n\n"
	os.WriteFile(log, []byte(content), 0644)
	got := Info("1.0.1", "Windows 11 (10.0.26100)", log, home)
	if !strings.HasPrefix(got, "Albion Journal 1.0.1") || !strings.Contains(got, "Windows 11") {
		t.Fatal(got)
	}
	if strings.Contains(got, "vasya") || strings.Contains(got, "Vasya") || strings.Contains(got, "Petya") {
		t.Fatalf("личное в сведениях:\n%s", got)
	}
	if !strings.Contains(got, "~/AppData") || !strings.HasSuffix(got, "страница: http://127.0.0.1:5000/") {
		t.Fatalf("%s", got)
	}
	if Tail(filepath.Join(dir, "нет.log"))[0] != "(журнала нет)" {
		t.Fatal("нет журнала")
	}
}

func TestTailReadsOnlyEnd(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "big.log")
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&b, "строка номер %06d с хвостом для объёма\n", i)
	}
	os.WriteFile(log, []byte(b.String()), 0644)
	lines := Tail(log)
	if len(lines) == 0 || lines[len(lines)-1] != "строка номер 019999 с хвостом для объёма" {
		t.Fatal("конец")
	}
	if !strings.HasPrefix(lines[0], "строка номер ") {
		t.Fatalf("первая строка обрезана: %q", lines[0])
	}
}
