package paddle

import (
	"math"
	"testing"
)

// probsFor — выход модели, где на каждом шаге почти наверняка символ seq[t]
// (0 — пусто), но с долей p у символа alt[t] (если задан).
func probsFor(C int, seq, alt []int, p float32) []float32 {
	out := make([]float32, len(seq)*C)
	for t, c := range seq {
		row := out[t*C : (t+1)*C]
		for i := range row {
			row[i] = 0.001
		}
		row[c] = 1 - p
		if alt != nil && alt[t] >= 0 {
			row[alt[t]] = p
		} else {
			row[c] = 0.99
		}
	}
	return out
}

func TestCTC(t *testing.T) {
	chars := []string{"", "a", "b", "c", "1", "h", " "}
	C := len(chars)
	// «ab»: a a _ b b
	seq := []int{1, 1, 0, 2, 2}
	pr := probsFor(C, seq, nil, 0)
	ab, _ := Encode("ab", chars)
	ba, _ := Encode("ba", chars)
	if l1, l2 := CTCLogLik(pr, len(seq), C, ab), CTCLogLik(pr, len(seq), C, ba); !(l1 > l2+5) {
		t.Errorf("«ab» %.2f должно быть намного вероятнее «ba» %.2f", l1, l2)
	}
	if g, l := GreedyLogLik(pr, len(seq), C), CTCLogLik(pr, len(seq), C, ab); l < g-0.5 {
		t.Errorf("верное слово %.2f почти как лучший путь %.2f", l, g)
	}
	// Повтор буквы требует пустого между: «aa» на a a _ b b невероятно.
	aa, _ := Encode("aa", chars)
	if l := CTCLogLik(pr, len(seq), C, aa); l > -5 {
		t.Errorf("«aa» слишком вероятно: %.2f", l)
	}
	// Неизвестный символ — слово не кодируется.
	if _, ok := Encode("az", chars); ok {
		t.Error("«z» нет в словаре — Encode должен отказать")
	}
	// Чтение только разрешёнными: на шаге, где «b» чуть вероятнее «1»,
	// при разрешённых {1,h,пробел} читается «1».
	seq2 := []int{4, 0, 2, 0, 5}
	alt := []int{-1, -1, 4, -1, -1}
	pr2 := probsFor(C, seq2, alt, 0.4)
	allowed := Allow(chars, "1h ")
	if got := DecodeAllowed(pr2, len(seq2), C, chars, allowed); got != "11h" {
		t.Errorf("разрешённые: %q, ждали «11h»", got)
	}
	if math.IsNaN(CTCLogLik(pr, len(seq), C, nil)) {
		t.Error("пустое слово — не NaN")
	}
}
