package zonecard

import (
	"testing"
)

// I1: искажённый заголовок нестабильного пути (3+ ошибки) — не обычный портал.
func TestFix1I1GarbledUnstableTitle(t *testing.T) {
	for _, lines := range [][]string{
		{"Unsfab1e Rcads to", "Qiient-Al-Vynsis", "Closes in 5 h 3 m"},
		{"Niestabllne Scie2ki do", "Qiient-Al-Vynsis", "Zamyka sie za 4 m 18 s"},
		{"Instabi1e Stralbe nach", "Qiient-Al-Vynsis", "Schlielt in 4 m 18 s"},
	} {
		if r, why, err := sendWhy(t, lines); err == nil && why == "" {
			t.Errorf("%q отправлено бы на карту (%+v)", lines, r.Tooltip)
		}
	}
	// Заголовок опознан только нестрого — на карту не идёт.
	if r, why, err := sendWhy(t, []string{"—лутьАволоно в", "Qiient-Al-Vynsis", "Закроется через 5 ч 3 м"}); err == nil && why == "" {
		t.Errorf("нестрогий заголовок отправлен (%+v)", r.Tooltip)
	}
	// Чистый обычный заголовок — отправляется, как раньше.
	if _, why, err := sendWhy(t, []string{"Road of Avalon to", "Qiient-Al-Vynsis", "Closes in 5 h 3 m"}); err != nil || why != "" {
		t.Errorf("обычный портал: %q %v", why, err)
	}
}
