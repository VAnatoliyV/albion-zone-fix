package i18n

import (
	"strings"
	"testing"
)

func TestEveryKeyHasThreeLanguages(t *testing.T) {
	for k, row := range Table {
		for i, v := range row {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: нет перевода на %s", k, Langs[i])
			}
		}
		// Число подстановок одинаковое во всех языках, иначе страница
		// подставит не то или оставит %s на экране.
		n := strings.Count(row[0], "%s")
		for i := 1; i < 3; i++ {
			if strings.Count(row[i], "%s") != n {
				t.Errorf("%s: в %s %d подстановок, в ru %d", k, Langs[i], strings.Count(row[i], "%s"), n)
			}
		}
	}
}

func TestResolve(t *testing.T) {
	cases := []struct{ set, sys, want string }{
		{"es", "ru-RU", "es"},
		{"", "ru-RU", "ru"},
		{"", "es_ES.UTF-8", "es"},
		{"", "de-DE", "en"},
		{"", "", "en"},
		{"fr", "ru", "ru"},
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
	if T("ru", "нет.такого") != "нет.такого" {
		t.Fatal("ключ без перевода должен возвращаться как есть")
	}
	if Tf("en", "zf.of", 3, 47) != "3 of 47" {
		t.Fatal(Tf("en", "zf.of", 3, 47))
	}
	m := ForLang("es")
	if len(m) != len(Table) || m["tray.quit"] != "Salir" {
		t.Fatal("ForLang")
	}
}
