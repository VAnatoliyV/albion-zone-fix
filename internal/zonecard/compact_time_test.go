package zonecard

import (
	"testing"
	"time"
)

// Мелкий текст распознавание слепляет: «чере7ч05м», «р7ч05м». Часы и
// минуты подряд — сами по себе признак строки времени (строки из опыта с
// уменьшенными снимками тестера, 9 октября 2026).
func TestCompactTime(t *testing.T) {
	for _, c := range []struct {
		lines []string
		left  time.Duration
	}{
		{[]string{"Путь Аволона в", "Pasos-Avosam", "Анeт", "Хакротся чере7ч05м"}, 7*time.Hour + 5*time.Minute},
		{[]string{"Путь Аволоно в", "Pasos-Avosam", "Hет", "Х3оратся р7ч05м"}, 7*time.Hour + 5*time.Minute},
		{[]string{"Road of Avalon to", "Pasos-Avosam", "Closesin6h26m"}, 6*time.Hour + 26*time.Minute},
		// слишком искажено — время не выдумываем
		{[]string{"Пут. Аволоно в", "Fleos-Aluttum", "3 6426M"}, 0},
		// размер портала — не время
		{[]string{"Путь Авалона в", "Pasos-Avosam", "7/7"}, 0},
	} {
		tt, ok := ParseTooltip(c.lines)
		if !ok {
			t.Fatalf("%q: не разобрано", c.lines)
		}
		if tt.Left != c.left || (c.left > 0 && tt.TimeLoose) {
			t.Errorf("%q: время %v (нестрого %v), ждали %v", c.lines, tt.Left, tt.TimeLoose, c.left)
		}
	}
}

// Время, найденное чтением только цифр и единиц (≈), — нестрогое: на карту
// только после подтверждения вторым снимком.
func TestHintTimeIsLoose(t *testing.T) {
	tt, ok := ParseTooltip([]string{"Путь Аволоно в", "Pasos-Avosam", "HET", "2н ч7ч05 и", HintTime + "7ч05м"})
	if !ok || tt.Left != 7*time.Hour+5*time.Minute || !tt.TimeLoose {
		t.Fatalf("время %v, нестрого %v (ok %v)", tt.Left, tt.TimeLoose, ok)
	}
	// Строгое время в тултипе есть — подсказка не мешает.
	tt, _ = ParseTooltip([]string{"Путь Авалона в", "Pasos-Avosam", "Закроется через 7 ч 05 м", HintTime + "6ч26м"})
	if tt.Left != 7*time.Hour+5*time.Minute || tt.TimeLoose {
		t.Fatalf("строгое: %v, нестрого %v", tt.Left, tt.TimeLoose)
	}
}

func TestPortalNames(t *testing.T) {
	names := Default().PortalNames()
	has := map[string]bool{}
	for _, n := range names {
		has[n] = true
	}
	if len(names) < 700 || !has["Pasos-Avosam"] || !has["Eldon Hill"] || has["Arena1"] {
		t.Fatalf("названий %d, Pasos-Avosam %v, Eldon Hill %v, Arena1 %v", len(names), has["Pasos-Avosam"], has["Eldon Hill"], has["Arena1"])
	}
}

// Слепленное время немецкого и турецкого клиента (часы St / sa, минуты dk).
func TestCompactTimeDeTr(t *testing.T) {
	for _, lines := range [][]string{
		{"Straße von Avalon nach", "Pasos-Avosam", "Schliet in7St05m"},
		{"Straße von Avalon nach", "Pasos-Avosam", "Sch1ie8t i7St5m"},
		{"Avalon Yolu çıkışı:", "Pasos-Avosam", "Kapanmasna kalan7sa05dk"},
		{"Avalon Yolu çıkışı:", "Pasos-Avosam", "Kpanmsn klan 7sa5dk"},
	} {
		tt, ok := ParseTooltip(lines)
		if !ok || tt.Left != 7*time.Hour+5*time.Minute {
			t.Errorf("%q: время %v (ok %v, нестрого %v)", lines, tt.Left, ok, tt.TimeLoose)
		}
	}
}
