package zonecard

import (
	"errors"
	"strings"
	"time"
)

// Result — снятая карточка (РезультатЗоны у мака).
type Result struct {
	Tooltip Tooltip
	Matches []Match
	At      time.Time // момент снимка
	// Portal — false: снята только зона без портала (название на карте мира
	// или миникарте).
	Portal bool
	// Lang — язык OCR, на котором прочитано ("ru", "en").
	Lang string
	// Loose — название взято нестрого (SimilarLoose, тултип в игре прочитан
	// сильно искажённым): итог всегда сомнительный.
	Loose bool
}

// Zone — лучшее совпадение; nil — нет.
func (r Result) Zone() *Zone {
	if len(r.Matches) == 0 {
		return nil
	}
	return r.Matches[0].Zone
}

// Doubtful — названия зон похожи как близнецы: мало похоже или второй
// кандидат дышит в затылок.
func (r Result) Doubtful() bool {
	if len(r.Matches) == 0 || r.Loose {
		return true
	}
	first := r.Matches[0].Closeness
	if first < doubtBelow {
		return true
	}
	// Соперник — только зона с другим названием: у города несколько кодов
	// с одним именем (Brecilien 5000 и 5001), это не сомнение.
	for _, m := range r.Matches[1:] {
		if m.Zone != nil && r.Matches[0].Zone != nil && m.Zone.Name == r.Matches[0].Zone.Name {
			continue
		}
		if first-m.Closeness < 0.06 {
			return true
		}
		break
	}
	return false
}

// LeftAt — сколько порталу осталось на момент now (время утекает с момента
// снимка). false — время не прочитано.
func (r Result) LeftAt(now time.Time) (time.Duration, bool) {
	if r.Tooltip.Left <= 0 {
		return 0, false
	}
	return max(0, r.Tooltip.Left-now.Sub(r.At)), true
}

// ClosesAt — когда портал закроется; ноль — время не прочитано.
func (r Result) ClosesAt() time.Time {
	if r.Tooltip.Left <= 0 {
		return time.Time{}
	}
	return r.At.Add(r.Tooltip.Left)
}

// StrictThreshold — порог для случая без портала: на карте мира название
// зоны стоит среди прочего текста, и поймать его можно только строго —
// иначе любое слово превратится в зону.
const StrictThreshold = 0.92

// Ошибки опознания (БедаЗоны у мака).
var (
	ErrNoTooltip = errors.New("под курсором не портал дорог")
)

// UnknownError — тултип прочитан, а зона в справочнике не нашлась.
type UnknownError struct{ Read string }

func (e UnknownError) Error() string { return "не узнал зону: " + e.Read }

// Identify — то, что у мака делает Карточка.снять после распознавания:
// сначала тултип портала, без него — строгий поиск названия среди строк.
func Identify(d *Dict, lines []string, at time.Time) (Result, error) {
	if t, ok := ParseTooltip(lines); ok {
		m := d.Similar(t.Read, 3)
		loose := false
		if len(m) == 0 || m[0].Closeness < doubtBelow {
			// Строго не опозналось — лучшее нестрогое среди строк тултипа
			// (название могло попасть не в ту строку или прочитаться
			// искажённым: «HW.tme Grasskmd» — Highstone Grassland).
			if lm, read := looseAmong(d, lines); looseOK(lm) &&
				(len(m) == 0 || lm[0].Closeness > m[0].Closeness) {
				m, loose, t.Read = lm, true, read
			}
		}
		if len(m) == 0 || m[0].Closeness < NameFloor {
			return Result{}, UnknownError{t.Read}
		}
		return Result{Tooltip: t, Matches: m, At: at, Portal: true, Loose: loose}, nil
	}
	var best []Match
	marker := HasMarker(lines)
	for _, l := range lines {
		c := Clean(l)
		if len([]rune(c)) < 5 {
			continue
		}
		m := d.Similar(c, 3)
		if len(m) > 0 && m[0].Closeness >= StrictThreshold && (len(best) == 0 || m[0].Closeness > best[0].Closeness) {
			best = m
		}
	}
	if len(best) == 0 {
		if marker {
			// Признак тултипа есть, а названия нет вовсе («1bAeanoHa e», «17»):
			// тултип был — не узнан, а не «не портал».
			return Result{}, UnknownError{strings.Join(cleanLines(lines), " | ")}
		}
		return Result{}, ErrNoTooltip
	}
	// Заголовок не прочитан, а название — дорога и рядом «Закроется через …»:
	// это тултип портала дорог, только без строки-признака.
	if best[0].Zone.Road {
		if left := timeLeft(lines); left > 0 {
			return Result{Tooltip: Tooltip{Read: best[0].Zone.Name, Size: portalSize(lines), Left: left},
				Matches: best, At: at, Portal: true}, nil
		}
	}
	return Result{Tooltip: Tooltip{Read: best[0].Zone.Name}, Matches: best, At: at}, nil
}

// cleanLines — непустые строки без мусора распознавания.
func cleanLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if c := Clean(l); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// doubtBelow — ниже этого сходства итог сомнительный (как в Doubtful).
const doubtBelow = 0.82

// looseAmong — лучшее нестрогое совпадение среди строк и сама строка.
func looseAmong(d *Dict, lines []string) ([]Match, string) {
	var best []Match
	read := ""
	for _, l := range lines {
		c := Clean(l)
		m := d.SimilarLoose(c, 3)
		if len(m) > 0 && (len(best) == 0 || m[0].Closeness > best[0].Closeness) {
			best, read = m, c
		}
	}
	return best, read
}

// Choose — OCR прочитал снимок на нескольких языках (язык клиента игры мы
// не знаем). Берём язык, на котором нашёлся тултип портала; если нашёлся
// на нескольких — где название опознано увереннее. Без тултипа — строгий
// поиск названия на любом языке. order — языки по порядку предпочтения.
func Choose(d *Dict, byLang map[string][]string, order []string, at time.Time) (Result, error) {
	var (
		best    Result
		bestErr error = ErrNoTooltip
		found   bool
	)
	score := func(r Result) float64 {
		s := r.Matches[0].Closeness
		if r.Portal {
			s += 10 // тултип портала важнее строгого поиска по названию
		}
		if !r.Loose {
			s += 4 // строгое опознание важнее нестрогого на другом языке
		}
		if r.Tooltip.TimeLoose {
			s -= 0.5 // и строгое время — нестрогого
		}
		return s
	}
	for _, lang := range order {
		lines, ok := byLang[lang]
		if !ok {
			continue
		}
		r, err := Identify(d, lines, at)
		if err != nil {
			// «Не узнал зону» — честнее, чем «не портал»: тултип-то был.
			var u UnknownError
			if errors.As(err, &u) && !found {
				bestErr = err
			}
			continue
		}
		r.Lang = lang
		if !found || score(r) > score(best) {
			best, found = r, true
		}
	}
	if !found {
		return Result{}, bestErr
	}
	return best, nil
}
