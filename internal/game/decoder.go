// Пакет game превращает UDP-пакеты Albion в события, нужные для учёта
// переходов между локациями: клиент попросил смену кластера, сервер ответил
// входом в локацию, с нового сервера пошли события.
package game

import (
	"regexp"
	"strings"
	"time"

	"albionzonefix/internal/photon"
)

// Коды операций Albion (как в albiondata-client, client/operations.go).
// Настоящий код лежит в параметре 253, байт кода в команде Photon общий.
const (
	OpJoin          = 2
	OpChangeCluster = 41
)

// Packet — один UDP-пакет игры: время, направление, удалённый адрес и нагрузка.
type Packet struct {
	T       time.Time
	Out     bool   // от нас к серверу
	Addr    string // удалённый ip:port
	Payload []byte
}

type Kind int

const (
	ChangeCluster Kind = iota + 1 // клиент уходит из локации
	Join                          // сервер впустил в локацию
	Incoming                      // любое событие с сервера (для «когда ожило»)
	Connect                       // клиент открывает соединение с игровым сервером (Photon CONNECT)
	Disconnect                    // клиент сам закрыл соединение с игровым сервером
	Reply                         // первый пакет с игрового сервера после CONNECT
)

// Команды Photon в заголовке пакета.
const (
	cmdConnect    = 2
	cmdDisconnect = 4
	gamePort      = ":5056"
)

type Ev struct {
	T        time.Time
	Kind     Kind
	Server   string
	Location string
}

// Decoder держит отдельный разборщик Photon на каждый сервер и направление:
// у Photon фрагменты собираются внутри соединения, смешивать их нельзя.
type Decoder struct {
	out     func(Ev)
	parsers map[string]*photon.PhotonParser
	replied map[string]bool // сервер уже ответил после последнего CONNECT
	cur     Packet
}

func NewDecoder(out func(Ev)) *Decoder {
	return &Decoder{out: out, parsers: map[string]*photon.PhotonParser{}, replied: map[string]bool{}}
}

func (d *Decoder) Feed(p Packet) {
	d.transport(p)
	key := p.Addr
	if p.Out {
		key = ">" + key
	}
	pr := d.parsers[key]
	if pr == nil {
		pr = photon.NewPhotonParser(d.onRequest, d.onResponse, d.onEvent)
		d.parsers[key] = pr
	}
	d.cur = p
	defer func() { recover() }() // битый пакет не должен ронять наблюдение
	pr.ReceivePacket(p.Payload)
}

func opOf(code byte, params map[byte]interface{}) int {
	switch v := params[253].(type) {
	case byte:
		return int(v)
	case int16:
		return int(v)
	case uint16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	}
	return int(code)
}

func (d *Decoder) onRequest(code byte, params map[byte]interface{}) {
	if opOf(code, params) == OpChangeCluster {
		d.out(Ev{T: d.cur.T, Kind: ChangeCluster, Server: d.cur.Addr})
	}
}

func (d *Decoder) onResponse(code byte, rc int16, _ string, params map[byte]interface{}) {
	if rc != 0 || opOf(code, params) != OpJoin {
		return
	}
	s, _ := params[8].(string)
	if loc := NormalizeLocation(s); loc != "" {
		d.out(Ev{T: d.cur.T, Kind: Join, Server: d.cur.Addr, Location: loc})
	}
}

func (d *Decoder) onEvent(_ byte, _ map[byte]interface{}) {
	if !d.cur.Out {
		d.out(Ev{T: d.cur.T, Kind: Incoming, Server: d.cur.Addr})
	}
}

var (
	reIsland  = regexp.MustCompile(`(?i)@island@[0-9a-f-]{36}`)
	reNumeric = regexp.MustCompile(`^[0-9]{3,6}$`)
)

// NormalizeLocation — как normalizeLocationID в albiondata-client: оставляет
// только правдоподобные коды локаций, остальное пустой строкой.
func NormalizeLocation(v string) string {
	s := strings.TrimSpace(strings.Trim(v, ",."))
	if s == "" {
		return ""
	}
	if m := reIsland.FindString(s); m != "" {
		return "@ISLAND@" + m[len("@island@"):]
	}
	if reNumeric.MatchString(s) {
		return s
	}
	ls := strings.ToLower(s)
	if strings.HasPrefix(ls, "island-player-") || strings.HasPrefix(ls, "@player-island") ||
		strings.HasPrefix(ls, "@island-") || strings.HasPrefix(s, "BLACKBANK-") ||
		strings.HasSuffix(s, "-HellDen") || strings.HasSuffix(s, "-Auction2") {
		return s
	}
	return ""
}

// transport смотрит на сам заголовок Photon: в живой игре переход начинается
// с CONNECT к новому игровому серверу (порт 5056), и если сервер не отвечает,
// никаких операций разобрать не удаётся — видно только это.
func (d *Decoder) transport(p Packet) {
	if !strings.HasSuffix(p.Addr, gamePort) {
		return
	}
	if !p.Out {
		if !d.replied[p.Addr] {
			d.replied[p.Addr] = true
			d.out(Ev{T: p.T, Kind: Reply, Server: p.Addr})
		}
		return
	}
	b := p.Payload
	// Клиентская команда без своего номера пира: 0xFFFF, хотя бы одна команда.
	// «Фальшивки» zapret начинаются с мусора и сюда не проходят.
	if len(b) < 24 || b[0] != 0xff || b[1] != 0xff || b[3] < 1 {
		return
	}
	switch b[12] {
	case cmdConnect:
		d.replied[p.Addr] = false
		d.out(Ev{T: p.T, Kind: Connect, Server: p.Addr})
	case cmdDisconnect:
		d.out(Ev{T: p.T, Kind: Disconnect, Server: p.Addr})
	}
}
