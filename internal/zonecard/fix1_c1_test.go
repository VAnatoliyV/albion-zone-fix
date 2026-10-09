package zonecard

import (
	"testing"
	"time"

	"albionzonefix/internal/avalon"
)

// Правило: любой признак нестабильного пути — на карту не идёт.

var fixHere = &avalon.Place{Zone: "TNL-001", Region: "europe"}

func sendWhy(t *testing.T, lines []string) (Result, string, error) {
	t.Helper()
	r, err := Identify(dict(t), lines, time.Unix(1_800_000_000, 0))
	if err != nil {
		return r, "", err
	}
	_, why := ByButton(r, fixHere, dict(t))
	return r, why, nil
}

// C1: заголовок не прочитан вовсе, а строка времени — «для вашей группы».
func TestFix1C1NoTitlePartyLine(t *testing.T) {
	all := phrasesFor(t)
	for _, l := range ourLangs {
		p := all[l]
		for _, party := range p.Party {
			lines := []string{"Qiient-Al-Vynsis", closesLine(party, timeText(p), l == "tr")}
			r, why, err := sendWhy(t, lines)
			if err == nil && why == "" {
				t.Errorf("%s: %q отправлено бы на карту (%+v)", l, lines, r.Tooltip)
			}
		}
	}
	// Как это читает eslav (без диакритики).
	for _, lines := range [][]string{
		{"Qiient-Al-Vynsis", "Closes to your party in 5 h 3 m"},
		{"Qiient-Al-Vynsis", "Закроется для вашей группы через 5 ч 3 м"},
		{"Qiient-Al-Vynsis", "Zamyka sie dla twojej druzyny za 4 m 18 s"},
	} {
		if _, why, err := sendWhy(t, lines); err == nil && why == "" {
			t.Errorf("%q отправлено бы на карту", lines)
		}
	}
}

// Без заголовка, но с обычным «Закроется через …» — по-прежнему на карту;
// без какой-либо фразы времени — нет (нестабильный путь не исключить).
func TestFix1C1NoTitleNormalCloses(t *testing.T) {
	for _, lines := range [][]string{
		{"Qiient-Al-Vynsis", "Closes in 5 h 3 m"},
		{"Qiient-Al-Vynsis", "Закроется через 5 ч 3 м"},
		{"Qiient-Al-Vynsis", "Schließt in 5 st 3 m"},
	} {
		if _, why, err := sendWhy(t, lines); err != nil || why != "" {
			t.Errorf("%q: %q %v", lines, why, err)
		}
	}
	if _, why, err := sendWhy(t, []string{"Qiient-Al-Vynsis", "5 h 3 m"}); err == nil && why == "" {
		t.Error("без фразы времени отправлено")
	}
}
