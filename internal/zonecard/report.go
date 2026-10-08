package zonecard

import (
	"math"

	"albionzonefix/internal/avalon"
)

// Почему портал карточки не ушёл на карту (ПочемуНеОтправлено у мака).
// Коды — для страницы, она переводит их сама (map.why.*).
const (
	WhyNoTime    = "noTime"    // время закрытия не прочитано
	WhyNoPlace   = "noPlace"   // не знаем, где игрок
	WhyNotEurope = "notEurope" // сервер игры не Европа
	WhyDoubt     = "doubt"     // название прочитано неуверенно
	WhySame      = "same"      // портал ведёт в эту же зону
	WhyNotRoad   = "notRoad"   // ни одна из зон не дороги Авалона
)

// ByButton — отчёт tooltip по кнопке, правила как у ОтчётКарты.поКнопке:
// from — текущая зона, to — зона за порталом, closesAt — момент снимка +
// сколько осталось (округлено до секунды), size, server. here == nil —
// входа в зону после запуска не видели. Пустое why — отправлять.
func ByButton(r Result, here *avalon.Place, d *Dict) (avalon.Tip, string) {
	if r.Tooltip.Left <= 0 {
		return avalon.Tip{}, WhyNoTime
	}
	if here == nil || here.Zone == "" {
		return avalon.Tip{}, WhyNoPlace
	}
	// Карта только для Европы. Сервер неизвестен ("") — отправляем, как мак.
	if !avalon.RegionOK(here.Region) {
		return avalon.Tip{}, WhyNotEurope
	}
	to := r.Zone()
	if to == nil || r.Doubtful() {
		return avalon.Tip{}, WhyDoubt
	}
	if to.Code == here.Zone {
		return avalon.Tip{}, WhySame
	}
	from := d.ByCode(here.Zone)
	if !to.Road && (from == nil || !from.Road) {
		return avalon.Tip{}, WhyNotRoad
	}
	closes := float64(r.At.UnixNano())/1e9 + r.Tooltip.Left.Seconds()
	return avalon.Tip{From: here.Zone, To: to.Code, ClosesAt: int64(math.Round(closes)),
		Size: r.Tooltip.Size, Region: here.Region}, ""
}

// DoubtFile — копия снимка сомнительной карточки в каталоге данных
// (перезаписывается): тестер присылает её, если зона опознана неуверенно.
const DoubtFile = "zone-capture-doubt.png"

// Doubtful — карточка сомнительная: тултип прочитан, а зона не узнана
// (ErrKindUnknown) или узнана неуверенно (Result.Doubtful, на карту не
// идёт — WhyDoubt). Тогда снимок копируется в DoubtFile.
func Doubtful(s Shot) bool {
	if s.Kind == ErrKindUnknown {
		return true
	}
	return s.Kind == "" && s.Result.Portal && s.Result.Doubtful()
}
