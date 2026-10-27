package chestlog

import (
	"os"
	"testing"
	"time"
)

func testNames(t *testing.T) *Names {
	b, err := os.ReadFile("testdata/item_loc.json")
	if err != nil {
		t.Fatal(err)
	}
	n, err := ParseNames(b)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestKey(t *testing.T) {
	for _, c := range []struct {
		code string
		ench int
		want string
	}{
		{"T4_FIBER_LEVEL1@1", -1, "T4_FIBER@1"},
		{"T4_FIBER_LEVEL1@1", 1, "T4_FIBER@1"},
		{"T6_MAIN_SWORD@2", -1, "T6_MAIN_SWORD@2"},
		{"T6_MAIN_SWORD", 2, "T6_MAIN_SWORD@2"},
		{"T6_MAIN_SWORD", -1, "T6_MAIN_SWORD@0"},
	} {
		if got := Key(c.code, c.ench); got != c.want {
			t.Errorf("Key(%q,%d)=%q, want %q", c.code, c.ench, got, c.want)
		}
	}
}

func TestReconcileMixedLanguages(t *testing.T) {
	names := testNames(t)
	now := time.Now()
	ts := func(m int) string { return now.Add(time.Duration(m) * time.Minute).Format("01/02/2006 15:04:05") }
	log := "\"Date\"\t\"Player\"\t\"Item\"\t\"Enchantment\"\t\"Quality\"\t\"Amount\"\n" +
		"\"" + ts(10) + "\"\t\"Bob\"\t\"Master's Broadsword\"\t\"2\"\t\"3\"\t\"1\"\n" +
		"\"" + ts(11) + "\"\t\"alice\"\t\"Необычная пенька\"\t\"1\"\t\"1\"\t\"30\"\n" +
		"\"" + ts(12) + "\"\t\"Alice\"\t\"Hemp\"\t\"0\"\t\"1\"\t\"-5\"\n" +
		"\"" + ts(13) + "\"\t\"Carl\"\t\"No Such Thing\"\t\"0\"\t\"1\"\t\"1\"\n" +
		"\"" + ts(14) + "\"\t\"Dave\"\t\"Hemp\"\t\"0\"\t\"1\"\t\"7\"\n"
	rows := Parse(log)
	if len(rows) != 5 {
		t.Fatalf("строк %d, ждали 5", len(rows))
	}
	t0 := now.Unix()
	loot := []Loot{
		{T: t0, By: "Bob", Item: "T6_MAIN_SWORD@2", Name: "Master's Broadsword", N: 1},
		{T: t0, By: "Bob", Item: "T6_HEAD_PLATE_SET1", Name: "Master's Soldier Helmet", N: 1},
		{T: t0, By: "Alice", Item: "T4_FIBER_LEVEL1@1", Name: "Uncommon Hemp", N: 50},
		{T: t0, By: "Alice", Silver: true, N: 9000},
	}
	rep := Reconcile(rows, loot, names)
	if len(rep.Players) != 2 {
		t.Fatalf("игроков %d: %+v", len(rep.Players), rep.Players)
	}
	got := map[string]Player{}
	for _, p := range rep.Players {
		got[p.Name] = p
	}
	if a := got["Alice"]; a.MissingN != 20 || len(a.Missing) != 1 || a.Missing[0].Item != "T4_FIBER_LEVEL1@1" {
		t.Errorf("Alice: %+v", a)
	}
	if b := got["Bob"]; b.MissingN != 1 || b.Missing[0].Item != "T6_HEAD_PLATE_SET1" {
		t.Errorf("Bob: %+v", b)
	}
	if len(rep.Unknown) != 1 || rep.Unknown[0] != "No Such Thing" {
		t.Errorf("unknown: %v", rep.Unknown)
	}
	if len(rep.NoPlayers) != 1 || rep.NoPlayers[0] != "Dave" {
		t.Errorf("noPlayers: %v", rep.NoPlayers)
	}
}

func TestOldDepositsIgnored(t *testing.T) {
	names := testNames(t)
	old := time.Now().Add(-48 * time.Hour).Format("01/02/2006 15:04:05")
	rows := Parse("\"" + old + "\"\t\"Bob\"\t\"Hemp\"\t\"0\"\t\"1\"\t\"5\"")
	rep := Reconcile(rows, []Loot{{T: time.Now().Unix(), By: "Bob", Item: "T4_FIBER", N: 5}}, names)
	if rep.Old != 1 || rep.Players[0].MissingN != 5 {
		t.Fatalf("%+v", rep)
	}
}
