package zonecard

import (
	"time"

	"albionzonefix/internal/zones"
)

// Пороги «чёрного экрана» в одном переходе (учёт Zone Fix):
const (
	// StallAfter — новый сервер не ответил на CONNECT за столько: тут же
	// уведомление «похоже на чёрный экран».
	StallAfter = 2 * time.Second
	// SilentAfter — после входа в зону сервер молчал столько (или вовсе не
	// ожил): чёрный экран после загрузки.
	SilentAfter = 10 * time.Second
	// MinHistory — меньше переходов в зону — риск не показываем: это ещё не
	// вероятность, а случайность.
	MinHistory = 3
	// SlowReply — в истории ответ сервера считается от начала перехода
	// (запрос смены кластера), а не от CONNECT, поэтому порог с запасом.
	SlowReply = 3 * time.Second
)

// IsBlack — был ли в переходе чёрный экран: вылет; новый сервер ответил
// позже SlowReply от начала перехода; после входа сервер молчал SilentAfter и дольше или так
// и не ожил (−1).
func IsBlack(tr zones.Transition) bool {
	if !tr.OK {
		return true
	}
	if tr.ReplySec >= SlowReply.Seconds() {
		return true
	}
	return tr.AliveSec < 0 || tr.AliveSec >= SilentAfter.Seconds()
}

// Risk — по истории переходов этого человека: в скольких из переходов в
// зону code был чёрный экран. Переход «в зону» — удачный вход в неё (To)
// или вылет по дороге туда (Want: портал в неё снимали карточкой перед
// переходом; иначе цель вылета неизвестна и он не считается).
func Risk(trs []zones.Transition, code string) (black, total int) {
	if code == "" {
		return 0, 0
	}
	for _, tr := range trs {
		if tr.To != code && !(tr.To == "" && tr.Want == code) {
			continue
		}
		total++
		if IsBlack(tr) {
			black++
		}
	}
	return black, total
}

// ShowRisk — показывать ли строку риска в карточке.
func ShowRisk(total int) bool { return total >= MinHistory }
