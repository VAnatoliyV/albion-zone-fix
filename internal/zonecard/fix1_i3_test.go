package zonecard

import (
	"testing"
	"time"
)

// I3: единицы языка — только в тултипе этого языка; больше суток — не время.
func TestFix1I3UnitsByLanguage(t *testing.T) {
	for _, c := range []struct {
		lines []string
		want  time.Duration
	}{
		{[]string{"Road of Avalon to", "Qiient-Al-Vynsis", "6 g 17 m"}, 17 * time.Minute},
		{[]string{"Путь Авалона в", "Qiient-Al-Vynsis", "Закроется через 2 t 17 м"}, 17 * time.Minute},
		{[]string{"Road of Avalon to", "Qiient-Al-Vynsis", "Closes in 1 d 2 h"}, 0},
		{[]string{"Avalon Yolu çıkışı", "Qiient-Al-Vynsis", "Kapanmasına kalan 5 sa 3 dk"}, 5*time.Hour + 3*time.Minute},
		{[]string{"Straße von Avalon nach", "Qiient-Al-Vynsis", "Schließt in 2 st 7 m"}, 2*time.Hour + 7*time.Minute},
	} {
		tt, ok := ParseTooltip(c.lines)
		if !ok || tt.Left != c.want {
			t.Errorf("%q: %v (ok=%v), ждал %v", c.lines, tt.Left, ok, c.want)
		}
	}
}
