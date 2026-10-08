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
// место неизвестно. Уход — только подтверждённый: CONNECT к другому
// игровому серверу или ChangeCluster, за которым пришёл CONNECT или Join.

const (
	// awayAfter — после ухода без Join дольше этого место неизвестно.
	// Обычный переход ChangeCluster → Join занимает 1–5 с: проход при этом
	// не теряется (прошлое место хранится до Join), только вкладка и
	// карточка на это время говорят «не знаю, где ты».
	awayAfter = 3 * time.Second
	// leaveGap — новый сигнал ухода позже этого после последнего сигнала
	// без Join — уже другой уход. Больше, чем ждёт учёт переходов до
	// «вылета» (30 с): повторы одного перехода сюда не попадают.
	leaveGap = 30 * time.Second
	// ccForget — ChangeCluster засчитывается уходом, только если за это
	// время пришёл CONNECT к другому серверу или Join. Иначе переход
	// отменён или отклонён (портал в бою, зона полна): игрок на месте.
	ccForget = 10 * time.Second
)

type leaveCounter struct {
	n       int       // подтверждённых уходов с последнего Join
	leftAt  time.Time // первый подтверждённый уход с последнего Join
	last    time.Time // последний сигнал текущего ухода
	target  string    // новый игровой сервер текущего ухода ("" — CONNECT не видели)
	replied bool      // target ответил (уже не просто повтор CONNECT)
	joined  string    // сервер последнего Join
	pend    time.Time // ChangeCluster ждёт подтверждения (нулевое — нет)
}

// on учитывает событие. Join отдаёт число уходов перед ним и обнуляет счёт.
func (l *leaveCounter) on(e game.Ev) (leavesBeforeJoin int) {
	l.expire(e.T)
	switch e.Kind {
	case game.ChangeCluster:
		switch {
		case !l.pend.IsZero():
			// повтор ChangeCluster, ждущего подтверждения
		case l.n == 0,
			l.target != "" && e.Server == l.target, // уходим с сервера, куда перешли, а Join там не было
			e.T.Sub(l.last) > leaveGap:
			l.pend = e.T
		default:
			l.last = e.T // повтор текущего ухода
		}
	case game.Connect:
		if !l.pend.IsZero() {
			if l.n == 0 && e.Server == l.joined {
				return 0 // переподключение к тому же серверу не подтверждает
			}
			l.confirm(l.pend, e.Server)
			return 0
		}
		switch {
		case l.n == 0:
			if e.Server != l.joined { // к тому же серверу — переподключение
				l.confirm(e.T, e.Server)
			}
		case l.target == "":
			l.target, l.replied, l.last = e.Server, false, e.T
		case e.Server == l.target:
			l.last = e.T // повтор CONNECT
		case !l.replied:
			// Прошлый новый сервер так и не ответил: клиент сменил цель
			// того же перехода (как zones.Tracker).
			l.target, l.last = e.Server, e.T
		default:
			// Уже были на одном новом сервере и без Join идём на следующий.
			l.confirm(e.T, e.Server)
		}
	case game.Reply:
		if l.n > 0 && e.Server == l.target {
			l.replied = true
		}
	case game.Join:
		n := l.n
		if !l.pend.IsZero() {
			n++ // Join подтвердил ChangeCluster
		}
		*l = leaveCounter{joined: e.Server}
		return n
	}
	return 0
}

// expire забывает ChangeCluster без подтверждения дольше ccForget.
func (l *leaveCounter) expire(now time.Time) {
	if !l.pend.IsZero() && now.Sub(l.pend) > ccForget {
		l.pend = time.Time{}
	}
}

// confirm — подтверждённый уход, начатый в t, на новый сервер target.
func (l *leaveCounter) confirm(t time.Time, target string) {
	if l.n == 0 {
		l.leftAt = t
	}
	l.n++
	l.last, l.target, l.replied, l.pend = t, target, false, time.Time{}
}

// away — после подтверждённого ухода нет Join дольше awayAfter: место
// неизвестно.
func (l *leaveCounter) away(now time.Time) bool {
	return l.n > 0 && now.Sub(l.leftAt) >= awayAfter
}
