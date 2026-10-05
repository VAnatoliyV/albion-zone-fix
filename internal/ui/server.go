// Пакет ui — окно программы: страница в браузере на 127.0.0.1.
package ui

import (
	_ "embed"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"albionzonefix/internal/app"
)

//go:embed index.html
var page []byte

// Start поднимает сервер на свободном порту и возвращает адрес страницы.
func Start(a *app.App) (string, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a.State())
	})
	mux.HandleFunc("/api/bypass", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		reply(w, a.SetBypass(r.FormValue("mode")))
	})
	mux.HandleFunc("/api/record", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		if r.FormValue("stop") != "" {
			a.StopRecord()
			reply(w, nil)
			return
		}
		reply(w, a.StartRecord(10*time.Minute))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	go http.Serve(ln, mux)
	return "http://" + ln.Addr().String() + "/", nil
}

func reply(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Write([]byte(`{"ok":true}`))
}
