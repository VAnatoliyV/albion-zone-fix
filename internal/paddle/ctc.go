package paddle

import "math"

// Вероятность слова по выходу модели (CTC). Жадный декод берёт на каждом
// шаге самую вероятную букву, и на мелком тексте одна ошибка портит слово.
// Если же известно, каким словом строка может быть (название зоны), можно
// спросить модель прямо: насколько вероятно, что здесь написано именно оно —
// с учётом всех способов растянуть буквы по шагам.

// Encode — слово в номера символов словаря chars; false — в слове есть
// символ, которого модель не знает.
func Encode(word string, chars []string) ([]int, bool) {
	idx := make(map[string]int, len(chars))
	for i, c := range chars {
		if i > 0 && c != "" {
			if _, ok := idx[c]; !ok {
				idx[c] = i
			}
		}
	}
	var out []int
	for _, r := range word {
		i, ok := idx[string(r)]
		if !ok {
			return nil, false
		}
		out = append(out, i)
	}
	return out, true
}

func logAdd(a, b float64) float64 {
	if a == math.Inf(-1) {
		return b
	}
	if b == math.Inf(-1) {
		return a
	}
	if a < b {
		a, b = b, a
	}
	return a + math.Log1p(math.Exp(b-a))
}

// CTCLogLik — натуральный логарифм вероятности того, что T шагов probs
// (по C символов) дают слово labels: прямой проход CTC по всем путям
// (пусто между буквами, повторы, обязательное пусто между одинаковыми).
func CTCLogLik(probs []float32, T, C int, labels []int) float64 {
	S := 2*len(labels) + 1
	lp := func(t, c int) float64 { return math.Log(float64(probs[t*C+c]) + 1e-12) }
	ext := func(s int) int {
		if s%2 == 0 {
			return 0
		}
		return labels[s/2]
	}
	inf := math.Inf(-1)
	prev := make([]float64, S)
	cur := make([]float64, S)
	for s := range prev {
		prev[s] = inf
	}
	prev[0] = lp(0, 0)
	if S > 1 {
		prev[1] = lp(0, ext(1))
	}
	for t := 1; t < T; t++ {
		for s := 0; s < S; s++ {
			a := prev[s]
			if s > 0 {
				a = logAdd(a, prev[s-1])
			}
			if s > 1 && s%2 == 1 && ext(s) != ext(s-2) {
				a = logAdd(a, prev[s-2])
			}
			cur[s] = a + lp(t, ext(s))
		}
		prev, cur = cur, prev
	}
	if S == 1 {
		return prev[0]
	}
	return logAdd(prev[S-1], prev[S-2])
}

// GreedyLogLik — логарифм вероятности самого вероятного пути (лучшая буква
// на каждом шаге): верхняя граница, с которой сравнивают слово.
func GreedyLogLik(probs []float32, T, C int) float64 {
	sum := 0.0
	for t := 0; t < T; t++ {
		best := float32(0)
		for _, p := range probs[t*C : (t+1)*C] {
			best = max(best, p)
		}
		sum += math.Log(float64(best) + 1e-12)
	}
	return sum
}

// Allow — какие символы словаря разрешены (runes — строка разрешённых букв).
func Allow(chars []string, runes string) []bool {
	ok := make([]bool, len(chars))
	for i, c := range chars {
		for _, r := range runes {
			if c == string(r) {
				ok[i] = true
			}
		}
	}
	return ok
}

// DecodeAllowed — жадный декод, где на каждом шаге выбор только между пусто
// и разрешёнными символами: для строки времени — цифры и единицы.
func DecodeAllowed(probs []float32, T, C int, chars []string, allowed []bool) string {
	out := make([]byte, 0, T)
	prev := -1
	for t := 0; t < T; t++ {
		row := probs[t*C : (t+1)*C]
		best, p := 0, row[0]
		for c := 1; c < C && c < len(allowed); c++ {
			if allowed[c] && row[c] > p {
				best, p = c, row[c]
			}
		}
		if best != 0 && best != prev {
			out = append(out, chars[best]...)
		}
		prev = best
	}
	return string(out)
}
