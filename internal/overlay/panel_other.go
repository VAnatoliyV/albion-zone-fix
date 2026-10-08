//go:build !windows

package overlay

import "albionzonefix/internal/zonecard"

// Panel — на других системах панели нет (сборка разработчика на маке).
type Panel struct{ logf func(string, ...any) }

// New — панель-заглушка.
func New(logf func(string, ...any)) *Panel { return &Panel{logf: logf} }

// Show только пишет в журнал, что панель показалась бы.
func (p *Panel) Show(card zonecard.Panel, corner string, secs int) {
	if p.logf != nil {
		p.logf("панель поверх игры (не Windows, не показана): %s, угол %s, %d с", card.Title, corner, secs)
	}
}

// Close — ничего.
func (p *Panel) Close() {}
