package app

import (
	"time"

	"albionzonefix/internal/game"
)

// Уходы из локации между входами (Join). В Туманы игра пускает без
// OpJoin с локацией (или вовсе без разобранного Join): место «зависает» на
// прошлом городе, и выход из Туманов на дорогу выглядел проходом
// «5001 → TNL-…», а тултип в Туманах получал from=5001. Поэтому считаем
// уходы: больше одного ухода между двумя Join — была зона, которую мы не
// видели, и это не проход; а пока после ухода нет Join дольше awayAfter —
// место неизвестно.

const (
	// awayAfter — после ухода без Join дольше этого место неизвестно.
	// Обычный переход ChangeCluster → Join занимает 1–5 с: проход при этом
	// не теряется (прошлое место хранится до Join), только вкладка и
	// карточка на это время говорят «не знаю, где ты».
	awayAfter = 3 * time.Second
	// leaveGap — новый сигнал ухода позже этого после прошлого ухода без
	// Join — уже другой уход. Больше, чем ждёт учёт переходов до «вылета»
	// (30 с): повторы одного перехода сюда не попадают.
	leaveGap = 30 * time.Second
)

type leaveCounter struct {
	n      int       // уходов с последнего Join
	leftAt time.Time // первый уход с последнего Join
	last   time.Time // последний засчитанный уход
	target string    // новый игровой сервер текущего ухода ("" — CONNECT не видели)
	joined string    // сервер последнего Join
}

// on учитывает событие. Join отдаёт число уходов перед ним и обнуляет счёт.
func (l *leaveCounter) on(e game.Ev) (leavesBeforeJoin int) {
	switch e.Kind {
	case game.ChangeCluster:
		switch {
		case l.n == 0:
			l.start(e.T, "")
		case l.target != "" && e.Server == l.target:
			// Уходим с сервера, куда уже перешли, а Join там не было.
			l.next(e.T, "")
		case e.T.Sub(l.last) > leaveGap:
			l.next(e.T, "")
		}
	case game.Connect:
		switch {
		case l.n == 0:
			if e.Server != l.joined { // к тому же серверу — переподключение
				l.start(e.T, e.Server)
			}
		case l.target == "":
			l.target = e.Server // CONNECT того же ухода
		case e.Server != l.target:
			// Уже перешли на один новый сервер и без Join идём на следующий.
			l.next(e.T, e.Server)
		}
	case game.Join:
		n := l.n
		*l = leaveCounter{joined: e.Server}
		return n
	}
	return 0
}

func (l *leaveCounter) start(t time.Time, target string) {
	l.n, l.leftAt, l.last, l.target = 1, t, t, target
}

func (l *leaveCounter) next(t time.Time, target string) {
	l.n++
	l.last, l.target = t, target
}

// away — после ухода нет Join дольше awayAfter: место неизвестно.
func (l *leaveCounter) away(now time.Time) bool {
	return l.n > 0 && now.Sub(l.leftAt) >= awayAfter
}
