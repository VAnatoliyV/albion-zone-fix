package zonecard

import (
	"context"
	"testing"
	"time"
)

// I2: один язык OCR видел нестабильный путь, другой (с лучшей оценкой) — нет.
func TestFix1I2UnstableAcrossLanguages(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	byLang := map[string][]string{
		"en-US": {"Road of Avalon to", "Qiient-Al-Vynsis", "Closes in 5 h 3 m"},
		"ru":    {"Нестабильные Пути в", "Qiient-Al-Vynsls", "5 ч 3 м"},
	}
	r, err := Choose(d, byLang, []string{"ru", "en-US"}, at)
	if err != nil || !r.Tooltip.Unstable {
		t.Fatalf("%+v %v", r.Tooltip, err)
	}
}

// I2: снимки одного нажатия — первый увидел нестабильный путь, второй — нет.
func TestFix1I2UnstableAcrossShots(t *testing.T) {
	d := dict(t)
	n := 0
	r := NewRunner(RunnerConfig{
		Capture: func(string) (Snap, error) { return Snap{}, nil },
		Recognize: func(_ context.Context, _ string, langs []string) (map[string][]string, error) {
			n++
			if n == 1 {
				return map[string][]string{"en-US": {"Unstable Roads to", "Qiient-Al-Vy"}}, nil
			}
			return map[string][]string{"en-US": {"Road of Avalon to", "Qiient-Al-Vynsis", "Closes in 5 h 3 m"}}, nil
		},
		Pick:  func([]string) []string { return []string{"en-US"} },
		Dict:  d,
		Retry: true,
		Sleep: func(time.Duration) {},
	})
	s := r.Run(context.Background())
	if n < 2 || s.Kind != "" || !s.Result.Tooltip.Unstable {
		t.Fatalf("вызовов %d: %+v %q", n, s.Result.Tooltip, s.Kind)
	}
}
