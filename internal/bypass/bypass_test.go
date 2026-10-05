package bypass

import (
	"strings"
	"testing"

	"albionzonefix/internal/zones"
)

func TestArgsOnlyAlbionPorts(t *testing.T) {
	s := Strategy{Name: "x", Args: []string{"--dpi-desync=fake", `--dpi-desync-fake-unknown-udp={BIN}ACTIVE_GAME_UDP.bin`}}
	got := strings.Join(Args(s, `C:\azf\zapret\bin\`), " ")
	for _, want := range []string{"--wf-udp=5055,5056", "--filter-udp=5055,5056", "--dpi-desync=fake",
		`--dpi-desync-fake-unknown-udp=C:\azf\zapret\bin\ACTIVE_GAME_UDP.bin`} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q в %q", want, got)
		}
	}
	if strings.Contains(got, "--wf-tcp") || strings.Contains(got, "443") {
		t.Errorf("перехватывается лишний трафик: %q", got)
	}
}

func TestDefaultStrategiesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range DefaultStrategies() {
		if s.Name == "" || seen[s.Name] || len(s.Args) == 0 {
			t.Fatalf("плохая стратегия %+v", s)
		}
		seen[s.Name] = true
	}
	if len(seen) < 6 {
		t.Fatalf("стратегий %d", len(seen))
	}
}

func tr(strategy string, load float64, ok bool) zones.Transition {
	return zones.Transition{Strategy: strategy, LoadSec: load, AliveSec: 1, OK: ok}
}

func TestPickerTriesEachAndKeepsBest(t *testing.T) {
	list := []Strategy{{Name: "a", Args: []string{"1"}}, {Name: "b", Args: []string{"2"}}, {Name: "c", Args: []string{"3"}}}
	p := NewPicker(list, 2)
	cur := p.Start()
	if cur.Name != "a" {
		t.Fatalf("начали с %s", cur.Name)
	}
	loads := map[string][]float64{"a": {8, 9}, "b": {2, 3}, "c": {1, 1}}
	oks := map[string][]bool{"a": {true, true}, "b": {true, true}, "c": {false, true}} // у c вылет
	for {
		var next *Strategy
		var done bool
		for i := 0; i < 2; i++ {
			next, done = p.OnTransition(tr(cur.Name, loads[cur.Name][i], oks[cur.Name][i]))
		}
		if done {
			break
		}
		if next == nil {
			t.Fatal("после 2 переходов нет следующей")
		}
		cur = *next
	}
	if p.Best().Name != "b" {
		t.Fatalf("лучшая %s, ждали b (у c вылет)", p.Best().Name)
	}
}

func TestPickerIgnoresOtherStrategyTransitions(t *testing.T) {
	p := NewPicker([]Strategy{{Name: "a", Args: []string{"1"}}, {Name: "b", Args: []string{"2"}}}, 1)
	p.Start()
	if next, done := p.OnTransition(tr("off", 5, true)); next != nil || done {
		t.Fatal("переход без обхода засчитан стратегии")
	}
}
