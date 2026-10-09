package ocr

import "testing"

// Время, прочитанное только цифрами и единицами, на языках клиента:
// часы ч/h, у немцев St, у турок sa; минуты м/m, у турок dk.
func TestCompactTimeLangs(t *testing.T) {
	for in, want := range map[string]string{
		"3с чд 7 ч 05 м": "7ч05м",
		"13с 7ч05м":      "7ч05м",
		"246ч26 м":       "6ч26м", // прилипшая цифра от соседнего слова
		"6h26m":          "6ч26м",
		"7 st 5 m":       "7ч05м", // немецкий
		"7st05m":         "7ч05м",
		"7 sa 5 dk":      "7ч05м", // турецкий
		"49m27s":         "49м27с",
		"7/7":            "",
		"3 6426m":        "",
	} {
		if got := compactTime(in); got != want {
			t.Errorf("%q → %q, ждали %q", in, got, want)
		}
	}
}
