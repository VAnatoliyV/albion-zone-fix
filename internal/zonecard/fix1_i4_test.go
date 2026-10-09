package zonecard

import (
	"testing"
)

// I4: значение биома в искажённом eslav виде (без диакритики) и фраза,
// совпадающая с биомом другого языка, — не признак.
func TestFix1I4BiomeFolded(t *testing.T) {
	for _, lines := range [][]string{
		{"Biom:", "Sciezki Awalonu"},
		{"Sciezki Awalonu"},
		{"Stralben von Avalon"},
		{"Strafben von Avalon"},
		{"Strassen von Avalon"},
		{"Jalan Avalon"},
		{"Caminos de Avalon"},
		{"Estradas de Avalon"},
	} {
		if HasMarker(lines) {
			t.Errorf("%q принято за признак", lines)
		}
	}
	// Настоящие заголовки — по-прежнему признак.
	for _, l := range []string{"Straße von Avalon nach", "Ścieżka Awalonu do", "Sciezka Awalonu do", "Jalan Avalon menuju", "Road of Avalon to"} {
		if !HasMarker([]string{l}) {
			t.Errorf("%q не признак", l)
		}
	}
}
