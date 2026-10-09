package i18n

import (
	"strings"
	"testing"
)

func TestEveryKeyHasAllLanguages(t *testing.T) {
	if len(Langs) != 9 {
		t.Fatalf("языков %d, ждал 9", len(Langs))
	}
	for k, row := range Table {
		for i, v := range row {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: нет перевода на %s", k, Langs[i])
			}
		}
		// Число подстановок одинаковое во всех языках, иначе страница
		// подставит не то или оставит %s на экране.
		n := strings.Count(row[0], "%s")
		for i := 1; i < len(row); i++ {
			if strings.Count(row[i], "%s") != n {
				t.Errorf("%s: в %s %d подстановок, в ru %d", k, Langs[i], strings.Count(row[i], "%s"), n)
			}
		}
		// Формы «сколько прошло»: у ru и pl три, у остальных две.
		if strings.HasPrefix(k, "ago.") && strings.Contains(row[0], "|") {
			for i, v := range row {
				want := 2
				if Langs[i] == "ru" || Langs[i] == "pl" {
					want = 3
				}
				if got := len(strings.Split(v, "|")); got != want {
					t.Errorf("%s/%s: форм %d, ждал %d", k, Langs[i], got, want)
				}
			}
		}
	}
}

func TestResolve(t *testing.T) {
	cases := []struct{ set, sys, want string }{
		{"es", "ru-RU", "es"},
		{"", "ru-RU", "ru"},
		{"", "es_ES.UTF-8", "es"},
		{"", "de-DE", "de"},
		{"", "pl-PL", "pl"},
		{"", "tr-TR", "tr"},
		{"", "fr-CA", "fr"},
		{"", "pt-BR", "pt"},
		{"", "pt-PT", "pt"},
		{"", "it-IT", "it"},
		{"", "ja-JP", "en"},
		{"", "", "en"},
		{"xx", "ru", "ru"},
		{"de", "ru", "de"},
		{"", "EN-gb", "en"},
	}
	for _, c := range cases {
		if got := Resolve(c.set, c.sys); got != c.want {
			t.Errorf("Resolve(%q,%q)=%q, ждал %q", c.set, c.sys, got, c.want)
		}
	}
}

func TestTAndForLang(t *testing.T) {
	if T("ru", "tab.zonefix") != "Переходы" || T("es", "tab.own") != "Mis precios" || T("xx", "tab.own") != "My prices" {
		t.Fatal("T")
	}
	if T("de", "tab.own") != "Meine Preise" || T("it", "tray.quit") != "Esci" || T("tr", "set.lang") != "Dil" {
		t.Fatal("T для новых языков")
	}
	if T("ru", "нет.такого") != "нет.такого" {
		t.Fatal("ключ без перевода должен возвращаться как есть")
	}
	if Tf("en", "zf.of", 3, 47) != "3 of 47" || Tf("fr", "zf.of", 3, 47) != "3 sur 47" {
		t.Fatal(Tf("en", "zf.of", 3, 47))
	}
	m := ForLang("es")
	if len(m) != len(Table) || m["tray.quit"] != "Salir" {
		t.Fatal("ForLang")
	}
	if ForLang("pl")["tray.quit"] != "Wyjdź" {
		t.Fatal("ForLang pl")
	}
}
