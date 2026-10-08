package zonecard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"albionzonefix/internal/avalon"
)

// C1: строка размера («7/7 N», «20/20 Н») и одни секунды («26 C») — не время.
func TestTolerantTimeNotSizeRow(t *testing.T) {
	for _, l := range []string{"7/7 N", "20/20 Н", "7 / 7 M", "26 C", "18 с"} {
		if d := tolerantTime(l); d != 0 {
			t.Errorf("%q: %v", l, d)
		}
	}
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	for _, lines := range [][]string{
		{"nyTb AeanoH0 a", "Tiros-Odoxlum", "7/7 N"},
		{"Путь Авалона в", "Tiros-Odoxlum", "20/20 Н"},
		{"Путь Авалона в", "Tiros-Odoxlum", "7/7", "26 C"},
	} {
		r, err := Identify(d, lines, at)
		if err != nil || r.Tooltip.Left != 0 {
			t.Errorf("%q: %+v %v", lines, r.Tooltip, err)
		}
	}
}

// C1: искажённое время — только в строке с «закро/через/closes»/часами или
// сразу после признака с названием; на карту такое время не идёт.
func TestTolerantTimeContextAndNotSent(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	// Время в строке не сразу после блока признака и без «через» — не время.
	r, err := Identify(d, []string{"Путь Авалона в", "Tiros-Odoxlum", "нет", "Тип зоны", "6 q 17 N"}, at)
	if err != nil || r.Tooltip.Left != 0 {
		t.Fatalf("далеко от признака: %+v %v", r.Tooltip, err)
	}
	// Сразу после названия — время, но искажённое: на карту не идёт.
	r, err = Identify(d, []string{"Путь Авалона в", "Tiros-Odoxlum", "6 q 17 N"}, at)
	if err != nil || r.Tooltip.Left != 6*time.Hour+17*time.Minute || !r.Tooltip.TimeLoose {
		t.Fatalf("после названия: %+v %v", r.Tooltip, err)
	}
	if _, why := ByButton(r, &avalon.Place{Zone: "TNL-001"}, d); why != WhyNoTime {
		t.Fatalf("искажённое время ушло на карту: %q", why)
	}
	if Good(Shot{Result: r}) {
		t.Fatal("искажённое время — не готовый итог")
	}
	// С «через» — тоже искажённое, на карту не идёт.
	r, _ = Identify(d, []string{"Путь Авалона в", "Tiros-Odoxlum", "7/7", "Закроется через 6 q 17 N"}, at)
	if r.Tooltip.Left != 6*time.Hour+17*time.Minute || !r.Tooltip.TimeLoose {
		t.Fatalf("с «через»: %+v", r.Tooltip)
	}
	// Строгое время — как раньше, на карту идёт.
	r, _ = Identify(d, []string{"Путь Авалона в", "Tiros-Odoxlum", "Закроется через 6 ч 26 м"}, at)
	if r.Tooltip.TimeLoose {
		t.Fatal("строгое время помечено искажённым")
	}
	// M5: «3 4 5 M» без «через» — не 3 ч 5 м.
	if d := tolerantTime("3 4 5 M"); d != 0 {
		t.Errorf("3 4 5 M: %v", d)
	}
}

// I2: строгое опознание важнее нестрогого на другом языке.
func TestChooseStrictBeatsLoose(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	by := map[string][]string{
		"ru":    {"Путь Авалона в", "Tiros-Odoxlurn", "Закроется через 6 ч 26 м"},
		"en-US": {"nyTb AeanoH0 a", "TirosOdoxIurn", "6 q 17 N"},
	}
	r, err := Choose(d, by, []string{"en-US", "ru"}, at)
	if err != nil || r.Lang != "ru" || r.Loose || r.Tooltip.Left != 6*time.Hour+26*time.Minute {
		t.Fatalf("%+v %v", r, err)
	}
}

// I3: сундуки, стражи и прочее «Авалонское» — не признак; строка времени —
// не название.
func TestGarbledMarkerRussianNegatives(t *testing.T) {
	for _, l := range [][]string{
		{"Авалонский сундук", "Закроется через 6 ч 26 м"},
		{"Авалонская стража", "5 м 20 с"},
		{"Сундук Авалона", "6 ч 10 м"},
		{"Страж Авалона в", "Tiros-Odoxlum"},
		{"Avalonian Guard a", "Tiros-Odoxlum"},
		{"Chest of Avalon to", "Tiros-Odoxlum"},
	} {
		if HasMarker(l) {
			t.Errorf("лишний признак: %q", l)
		}
	}
	// Строка времени после признака — не название.
	if tt, ok := ParseTooltip([]string{"Путь Авалона в", "Закроется через 6 ч 26 м"}); ok {
		t.Errorf("время взято названием: %+v", tt)
	}
	if tt, ok := ParseTooltip([]string{"—лутьАволоно в", "6 q 17 N"}); ok {
		t.Errorf("искажённое время взято названием: %+v", tt)
	}
}

// M2: строгий признак на поздней строке важнее искажённого на ранней.
func TestStrictMarkerFirst(t *testing.T) {
	tt, ok := ParseTooltip([]string{"Ключ от Авалона в", "Путь Авалона в", "Tiros-Odoxlum", "Закроется через 6 ч 26 м"})
	if !ok || tt.Read != "Tiros-Odoxlum" {
		t.Fatalf("%+v %v", tt, ok)
	}
}

// M1: обычные слова — не слабый признак.
func TestWeakMarkerWords(t *testing.T) {
	for _, l := range []string{"available", "провал", "завал", "карнавал", "путь домой", "Путь", "nyTb"} {
		if WeakMarker([]string{l}) {
			t.Errorf("лишний слабый признак: %q", l)
		}
	}
	for _, l := range []string{"Авал", "aval", "AeanoH", "путь авал", "—лутьАволоно"} {
		if !WeakMarker([]string{l}) {
			t.Errorf("нет слабого признака: %q", l)
		}
	}
}

// I1: обрезка без признака тултипа — сразу вся рамка (а не варианты
// обрезки и не готовый итог по названию с чужой панели).
func TestRunnerCropWithoutMarkerReadsFullFrame(t *testing.T) {
	full := []string{"Путь Авалона в", "Fleos-Aluttum", "Закроется через 6 ч 26 м"}
	for name, crop := range map[string][]string{
		"название города": {"Brecilien"},
		"частично":        {"Tiros-Odoxlum"},
		"слабый":          {"Авал", "чат"},
	} {
		r, calls, _ := retryRunner(t, map[string][]string{"/d/zone-capture.png": crop, "/d/zone-capture-full.png": full}, nil)
		r.cfg.Capture = func(p string) (Snap, error) { return Snap{Info: p, Cropped: true}, nil }
		s := r.Run(context.Background())
		if !Good(s) || s.Try != "1/3 вся рамка" {
			t.Errorf("%s: %+v", name, s)
			continue
		}
		if (*calls)[1] != "/d/zone-capture-full.png" {
			t.Errorf("%s: порядок %v", name, *calls)
		}
	}
	// Признак на обрезке есть — сначала её варианты.
	r, calls, _ := retryRunner(t, map[string][]string{"/d/zone-capture.png": {"Путь Авалона в", "Fleos-Aluttum"}, "/d/zone-capture-gray.png": full}, nil)
	r.cfg.Capture = func(p string) (Snap, error) { return Snap{Info: p, Cropped: true}, nil }
	if s := r.Run(context.Background()); !Good(s) || !strings.HasSuffix((*calls)[1], "-gray.png") {
		t.Errorf("варианты обрезки: %+v %v", s, *calls)
	}
}

var _ = errors.New

// Нестрогое время на карточке — с «≈».
func TestLooseTimeShownApprox(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	r, _ := Identify(d, []string{"Путь Авалона в", "Tiros-Odoxlum", "6 q 17 N"}, at)
	z := r.Zone()
	all := ToastOptions{Chests: true, Res: true, Dungeons: true, Portal: true, ChestsFirst: true}
	if b := BuildToast("ru", z, r, all, at).Body; !strings.Contains(b, "через ≈6 ч 17 м") {
		t.Error(b)
	}
	if f := BuildPanel("ru", z, r, all, at).Footer; !strings.Contains(f, "≈6 ч 17 м") {
		t.Error(f)
	}
}
