// Пакет probe проверяет, отвечают ли игровые серверы Albion на подключение.
// Шлёт тот же пакет CONNECT, что и игра (снят с записи 5 октября 2026), и ждёт
// любого ответа. Сразу после ответа вежливо закрывает соединение.
package probe

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	connectHex    = "ffff00010000000f45a67b5902ff01040000002c00000001000004b000008000000000020000000000000000000013880000000200000002"
	disconnectHex = "ffff00010000295245a67b5904ff02020000000c00000001"
	attempts      = 2
)

// Серверы Европы на 5 октября 2026: live03-win-01…47, все в 193.169.238.0/24.
// Сначала спрашиваем DNS, этот список — на случай, если имена не разрешаются.
var fallbackIPs = []string{"193.169.238.210", "193.169.238.37", "193.169.238.124", "193.169.238.235", "193.169.238.56",
	"193.169.238.157", "193.169.238.72", "193.169.238.102", "193.169.238.97", "193.169.238.141", "193.169.238.110",
	"193.169.238.103", "193.169.238.70", "193.169.238.48", "193.169.238.31", "193.169.238.206", "193.169.238.178",
	"193.169.238.165", "193.169.238.66", "193.169.238.186", "193.169.238.167", "193.169.238.242", "193.169.238.94",
	"193.169.238.198", "193.169.238.36", "193.169.238.104", "193.169.238.177", "193.169.238.83", "193.169.238.65",
	"193.169.238.49", "193.169.238.76", "193.169.238.236", "193.169.238.90", "193.169.238.17", "193.169.238.126",
	"193.169.238.60", "193.169.238.81", "193.169.238.88", "193.169.238.109", "193.169.238.121", "193.169.238.188",
	"193.169.238.40", "193.169.238.86", "193.169.238.164", "193.169.238.77", "193.169.238.248", "193.169.238.43"}

type Target struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

type Result struct {
	Target
	OK bool `json:"ok"`
	Ms int  `json:"ms"`
}

// ConnectPacket — пакет подключения Photon, как у игры, со случайным кодом соединения.
func ConnectPacket() []byte { return connectPacket() }

func connectPacket() []byte {
	p, _ := hex.DecodeString(connectHex)
	rand.Read(p[8:12]) // свой код соединения, как у настоящего клиента
	return p
}

// Targets — игровые серверы Европы: из DNS, а не вышло — из списка.
func Targets() []Target {
	var out []Target
	for i := 1; i <= 60; i++ {
		h := fmt.Sprintf("live03-win-%02d.ams.albiononline.com", i)
		ips, err := net.LookupHost(h)
		if err != nil || len(ips) == 0 {
			continue
		}
		out = append(out, Target{Name: fmt.Sprintf("win-%02d", i), Addr: net.JoinHostPort(ips[0], "5056")})
	}
	if len(out) == 0 {
		for i, ip := range fallbackIPs {
			out = append(out, Target{Name: fmt.Sprintf("win-%02d", i+1), Addr: net.JoinHostPort(ip, "5056")})
		}
	}
	return out
}

func one(t Target, wait time.Duration) Result {
	r := Result{Target: t, Ms: -1}
	ua, err := net.ResolveUDPAddr("udp", t.Addr)
	if err != nil {
		return r
	}
	c, err := net.DialUDP("udp", nil, ua)
	if err != nil {
		return r
	}
	defer c.Close()
	buf := make([]byte, 2048)
	for a := 0; a < attempts; a++ {
		p := connectPacket()
		start := time.Now()
		if _, err := c.Write(p); err != nil {
			return r
		}
		c.SetReadDeadline(time.Now().Add(wait))
		if _, err := c.Read(buf); err == nil {
			r.OK, r.Ms = true, int(time.Since(start).Milliseconds())
			d, _ := hex.DecodeString(disconnectHex)
			copy(d[8:12], p[8:12])
			c.Write(d)
			return r
		}
	}
	return r
}

// Run опрашивает серверы по 8 одновременно; порядок результата — как у целей.
func Run(targets []Target, wait time.Duration) []Result {
	res := make([]Result, len(targets))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, t Target) {
			defer wg.Done()
			defer func() { <-sem }()
			res[i] = one(t, wait)
		}(i, t)
	}
	wg.Wait()
	sort.SliceStable(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res
}
