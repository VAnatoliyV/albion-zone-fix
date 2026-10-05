// Пакет names — названия локаций по коду (из дампов игры, как в Albion Journal).
package names

import (
	_ "embed"
	"encoding/json"
)

//go:embed zones.json
var raw []byte

func Zones() map[string]string {
	m := map[string]string{}
	json.Unmarshal(raw, &m)
	return m
}
