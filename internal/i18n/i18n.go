// Пакет i18n — надписи программы на трёх языках (ru, en, es), как Loc.table
// у мак-версии. Словарь один на всё: страница получает его целиком через
// /api/i18n, трей и сообщения Go берут отсюда же.
package i18n

import (
	"fmt"
	"strings"
)

// Langs — языки по порядку столбцов словаря.
var Langs = []string{"ru", "en", "es"}

// Default — язык для всех, чей системный язык нам не знаком (как у мака).
const Default = "en"

// Resolve выбирает язык: из настроек, если он знаком, иначе системный,
// иначе английский. system — код вроде "ru-RU", "es_ES.UTF-8", "en".
func Resolve(setting, system string) string {
	if col(setting) >= 0 {
		return setting
	}
	s := strings.ToLower(system)
	if len(s) >= 2 && col(s[:2]) >= 0 {
		return s[:2]
	}
	return Default
}

func col(lang string) int {
	for i, l := range Langs {
		if l == lang {
			return i
		}
	}
	return -1
}

// T — надпись на языке lang. Ключ без перевода возвращается как есть:
// это заметно на экране и чинится быстрее, чем пустая строка.
func T(lang, key string) string {
	row, ok := Table[key]
	if !ok {
		return key
	}
	c := col(lang)
	if c < 0 {
		c = col(Default)
	}
	return row[c]
}

// Tf — то же с подстановкой. В словаре только %s, поэтому числа и всё
// прочее сначала превращаем в строки.
func Tf(lang, key string, args ...any) string {
	ss := make([]any, len(args))
	for i, a := range args {
		ss[i] = fmt.Sprint(a)
	}
	return fmt.Sprintf(T(lang, key), ss...)
}

// ForLang — весь словарь на одном языке: ключ → надпись. Отдаётся странице.
func ForLang(lang string) map[string]string {
	c := col(lang)
	if c < 0 {
		c = col(Default)
	}
	out := make(map[string]string, len(Table))
	for k, row := range Table {
		out[k] = row[c]
	}
	return out
}
