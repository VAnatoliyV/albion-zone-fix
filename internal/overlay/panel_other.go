//go:build !windows

package overlay

import "albionzonefix/internal/zonecard"

// Panel — на других системах панели нет (сборка разработчика на маке).
type Panel struct{ logf func(string, ...any) }

// New — панель-заглушка.
func New(logf func(string, ...any)) *Panel { return &Panel{logf: logf} }

// Show только пишет в журнал, что панель показалась бы.
func (p *Panel) Show(card zonecard.Panel, corner string, secs int, fallback func()) bool {
	if p.logf != nil {
		p.logf("панель поверх игры (не Windows, не показана): %s, угол %s, %d с", card.Title, corner, secs)
	}
	return true
}

// OK — заглушка не ломается.
func (p *Panel) OK() bool { return true }

// Mark — ничего.
func (p *Panel) Mark() {}

// Close — ничего.
func (p *Panel) Close() {}
