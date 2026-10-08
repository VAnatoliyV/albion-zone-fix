package zonecard

import (
	"strings"
	"testing"
	"time"

	"albionzonefix/internal/settings"
)

func roadShot(t *testing.T, at time.Time, left time.Duration) Shot {
	t.Helper()
	z := dict(t).Exact("Qiient-Al-Vynsis") // дорога убежищ T6: сундуки medium_veteran 2 + small 1
	if z == nil {
		t.Fatal("нет зоны в справочнике")
	}
	return Shot{Result: Result{Tooltip: Tooltip{Read: z.Name, Size: 7, Left: left}, At: at, Portal: true,
		Matches: []Match{{Zone: z, Closeness: 1}}}}
}

// Способ показа × включённые части.
func TestPresentShowTimesParts(t *testing.T) {
	at := time.Unix(1000, 0)
	sh := roadShot(t, at, 3*time.Hour)
	all := settings.Default()
	for _, c := range []struct {
		show         string
		toast, panel bool
	}{
		{settings.ShowNotify, true, false},
		{settings.ShowPanel, false, true},
		{settings.ShowOff, false, false},
		{"", true, false}, // незнакомое — уведомление
	} {
		s := all
		s.ZoneShow = c.show
		p := Present("ru", sh, s, at)
		if (p.Toast != nil) != c.toast || (p.Panel != nil) != c.panel {
			t.Errorf("%q: уведомление %v, панель %v", c.show, p.Toast != nil, p.Panel != nil)
		}
	}

	// Только ресурсы — ни сундуков, ни портала ни там, ни там.
	s := all
	s.NotifyChests, s.NotifyDng, s.NotifyPortal = false, false, false
	s.ZoneShow = settings.ShowNotify
	if b := Present("ru", sh, s, at).Toast.Body; b != "волокно · шкуры" {
		t.Fatalf("уведомление: %q", b)
	}
	s.ZoneShow = settings.ShowPanel
	p := Present("ru", sh, s, at).Panel
	if len(p.Rows) != 1 || p.Rows[0].Label != "на них" || p.Footer != "" {
		t.Fatalf("панель: %+v", p)
	}
	// Всё выключено — только название и вид.
	s.NotifyRes = false
	p = Present("ru", sh, s, at).Panel
	if len(p.Rows) != 0 || p.Footer != "" || p.Title != "Qiient-Al-Vynsis" || p.Subtitle != "дорога убежищ · T6" {
		t.Fatalf("панель без частей: %+v", p)
	}
	s.ZoneShow = settings.ShowNotify
	if tt := Present("ru", sh, s, at).Toast; tt.Body != "" || tt.Title != "Qiient-Al-Vynsis" {
		t.Fatalf("уведомление без частей: %+v", tt)
	}
}

func TestPresentNothingOnFailedShot(t *testing.T) {
	s := settings.Default()
	s.ZoneShow = settings.ShowPanel
	if p := Present("ru", Shot{Kind: ErrKindNoTooltip}, s, time.Now()); p.Toast != nil || p.Panel != nil {
		t.Fatalf("неудачный снимок: %+v", p)
	}
	if p := Present("ru", Shot{}, s, time.Now()); p.Toast != nil || p.Panel != nil {
		t.Fatalf("зона не узнана: %+v", p)
	}
}

func TestPanelRoadOrderAndFooter(t *testing.T) {
	at := time.Unix(1000, 0)
	sh := roadShot(t, at, 4*time.Minute)
	z, r := sh.Result.Zone(), sh.Result
	p := BuildPanel("ru", z, r, allOn, at)
	if len(p.Rows) != 2 || p.Rows[0].Label != "лагеря" || p.Rows[1].Label != "на них" {
		t.Fatalf("сундуки сверху: %+v", p.Rows)
	}
	ch := p.Rows[0].Items
	if len(ch) != 2 || ch[0].Text != "2 × средний ветеранский" || ch[0].Color != ChestColor["medium_veteran"] || ch[1].Text != "1 × малый" {
		t.Fatalf("сундуки: %+v", ch)
	}
	for _, it := range p.Rows[1].Items {
		if !strings.Contains(it.Text, "T") || it.Color == 0 {
			t.Fatalf("ресурс без тира или цвета: %+v", it)
		}
	}
	if p.Footer != "портал на 7 · закроется через 4:00" || !p.Warn || p.Color != QualityColor["roads"] {
		t.Fatalf("низ: %q warn=%v цвет=%x", p.Footer, p.Warn, p.Color)
	}
	o := allOn
	o.ChestsFirst = false
	if p := BuildPanel("en", z, r, o, at.Add(time.Minute)); p.Rows[0].Label != "on them" || p.Warn != true {
		t.Fatalf("ресурсы сверху: %+v", p.Rows)
	}
	if p := BuildPanel("ru", z, r, allOn, at.Add(-time.Hour)); p.Warn {
		t.Fatal("до закрытия больше 5 минут — без подсветки")
	}
}

func TestPanelOpenWorld(t *testing.T) {
	ow := &Zone{Name: "Somewhere", Quality: "black", Tier: 8, Grade: 3, Biome: []string{"HIDE", "ORE", "WOOD"},
		Res: map[string][][]int{"ROCK": {{8, 4}}}, Dungeons: map[string]int{"solo": 2}, Mists: true}
	r := Result{Tooltip: Tooltip{Read: "Somewhere"}, At: time.Unix(0, 0), Matches: []Match{{Zone: ow, Closeness: 0.7}, {Zone: &Zone{Name: "Somewhare"}, Closeness: 0.69}}}
	p := BuildPanel("ru", ow, r, allOn, time.Unix(0, 0))
	if p.Doubt == "" || !strings.Contains(p.Doubt, "Somewhare") {
		t.Fatalf("сомнение: %q", p.Doubt)
	}
	if len(p.Rows) != 2 || p.Rows[0].Label != "ресурсы" || p.Rows[1].Label != "данжи" {
		t.Fatalf("строки: %+v", p.Rows)
	}
	bio := p.Rows[0].Items
	if len(bio) != 3 || !bio[0].Main || bio[1].Main || bio[0].Text != "шкуры" {
		t.Fatalf("биом: %+v", bio)
	}
	if d := p.Rows[1].Items; len(d) != 2 || d[1].Text != "есть выход в город туманов" {
		t.Fatalf("данжи: %+v", d)
	}
	if p.Footer != "" {
		t.Fatalf("время не прочитано — низ пустой: %q", p.Footer)
	}
}
