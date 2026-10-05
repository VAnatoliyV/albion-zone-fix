//go:build windows

package trace

import "albionzonefix/internal/sniff"

// OpenICMP ловит ICMP «время истекло» и «недоступен» драйвером WinDivert в
// режиме наблюдения: системные сокеты Windows и файрвол тут ненадёжны.
func OpenICMP(binDir string) (<-chan Msg, func(), error) {
	d, err := sniff.OpenFilter(binDir, "icmp and (icmp.Type == 11 or icmp.Type == 3)")
	if err != nil {
		return nil, nil, err
	}
	out := make(chan Msg, 64)
	go d.RunRaw(func(b []byte) {
		if m, ok := ParseICMP(b); ok {
			select {
			case out <- m:
			default:
			}
		}
	})
	return out, d.Close, nil
}
