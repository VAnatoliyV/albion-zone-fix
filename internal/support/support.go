// Пакет support — «Скопировать сведения для поддержки»: версия программы,
// Windows и хвост журнала без игровых строк и без имени пользователя в
// путях. Повторяет Поддержка из мак-версии (app/Обновление.swift).
package support

import (
	"io"
	"os"
	"regexp"
	"strings"
)

// DiscordURL — сервер поддержки (там Ticket Tool). Telegram не добавляем —
// решение автора.
const DiscordURL = "https://discord.gg/5pV9ZqZMve"

// Lines — сколько последних строк журнала берём.
const Lines = 40

// Game — строки об игре (чужие и свои имена, серебро, урон) в тикет не
// идут: поддержке они не нужны, а людям незачем ими светить. Тот же список,
// что у мака (Поддержка.игровые).
var Game = []string{"Персонаж рядом", "имя", "баланс", "Серебро", "урон",
	"лечение", "Фейм", "Свободка", "Updating player", "Поднял"}

// Info — текст для тикета.
func Info(version, osVersion, logPath, home string) string {
	lines := []string{
		"Albion Journal " + version + " (Windows)",
		osVersion,
		"",
		"--- albion-journal.log (последние 40) ---",
	}
	lines = append(lines, Clean(Tail(logPath), Lines)...)
	return Anonymize(strings.Join(lines, "\n"), home)
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// Clean убирает цветовые коды терминала и игровые события, потом берёт
// последние n. Именно в таком порядке: иначе от 40 строк журнала после
// чистки могло бы остаться пять.
func Clean(lines []string, n int) []string {
	var out []string
	for _, l := range lines {
		l = strings.TrimRight(ansi.ReplaceAllString(l, ""), "\r")
		if isGame(l) {
			continue
		}
		out = append(out, l)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func isGame(l string) bool {
	for _, w := range Game {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// Anonymize заменяет домашний каталог (%USERPROFILE%) на «~». Регистр букв
// в путях Windows бывает разный — сравниваем без учёта регистра.
func Anonymize(text, home string) string {
	home = strings.TrimRight(home, `\/`)
	if home == "" || len(home) < 3 {
		return text
	}
	lt, lh := strings.ToLower(text), strings.ToLower(home)
	if len(lt) != len(text) { // ToLower поменял длину (редкие буквы) — точное совпадение
		return strings.ReplaceAll(text, home, "~")
	}
	var b strings.Builder
	for {
		i := strings.Index(lt, lh)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:i])
		b.WriteString("~")
		text, lt = text[i+len(home):], lt[i+len(home):]
	}
}

// Tail — последние строки файла (всё, что влезло в последние 256 КБ).
// Читаем только конец: журнал бывает большим.
func Tail(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{"(журнала нет)"}
	}
	defer f.Close()
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return []string{"(журнал не читается)"}
	}
	const take = 256 << 10
	start := int64(0)
	if end > take {
		start = end - take
	}
	f.Seek(start, io.SeekStart)
	b, _ := io.ReadAll(f)
	all := strings.Split(string(b), "\n")
	if start > 0 && len(all) > 0 {
		all = all[1:] // обрезанная строка
	}
	for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
		all = all[:len(all)-1]
	}
	return all
}
