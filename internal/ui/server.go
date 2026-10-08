// Пакет ui — страница программы и её данные на 127.0.0.1. Страница зашита
// в exe; показывает её окно WebView2 (internal/desktop), а если его нет —
// обычный браузер.
package ui

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"albionzonefix/internal/app"
	"albionzonefix/internal/i18n"
	"albionzonefix/internal/ownprices"
	"albionzonefix/internal/receiver"
	"albionzonefix/internal/settings"
	"albionzonefix/internal/shared"
	"albionzonefix/internal/support"
	"albionzonefix/internal/update"
)

//go:embed web
var webFS embed.FS

// TokenHeader — заголовок с ключом страницы. Без него POST не принимается:
// страницы чужих сайтов в браузере могут слать запросы на 127.0.0.1, но
// прочитать ключ из нашей страницы не могут.
const TokenHeader = "X-AJ-Token"

// Options — то, что серверу даёт программа.
type Options struct {
	DataDir      string // каталог данных
	LogPath      string // журнал (по нему видно, готов ли сайт приёмника)
	SessionFile  string // файл сессии счётчика
	ReceiverAddr string // порт приёмника; пусто — receiver.Addr
	Shared       *shared.Loader
	Token        string // пусто — сгенерировать
	Addr         string // где слушать; пусто — 127.0.0.1 на свободном порту

	// OnSettings вызывается после сохранения настроек (автозапуск, трей).
	// Ошибка уходит странице; хук сам решает, что откатить.
	OnSettings func(old, cur settings.Settings) error
	OnShow     func()           // вторая копия программы просит показать окно
	OpenURL    func(url string) // открыть адрес в браузере системы
	OpenFolder func(dir string) // открыть папку в Проводнике
	// OpenMap — окно «Карта Авалона» с текущей зоной (второе окно WebView2,
	// без него — браузер).
	OpenMap func()
	Lang    func() string // язык системы; nil — i18n.System

	Version string  // версия программы (для окна и сведений)
	Update  Updater // автообновление; nil — нет
	// OnUpdateRestart — «Перезапустить сейчас»: запустить установку и, только
	// если она пошла, остановить своё и выйти. Ошибка — ничего не тронуто.
	OnUpdateRestart func() error
	// SupportInfo — текст «Скопировать сведения для поддержки».
	SupportInfo func() string
}

// Updater — то, что странице нужно от update.Updater.
type Updater interface {
	Status() update.Status
	Check()
}

// Server — запущенный сервер страницы.
type Server struct {
	URL   string
	Token string

	a   *app.App
	o   Options
	srv *http.Server
	ln  net.Listener
}

// PageState — всё, что страница берёт раз в две секунды.
type PageState struct {
	app.State
	Lang      string `json:"lang"`
	Receiver  bool   `json:"receiver"`
	SiteReady bool   `json:"siteReady"`
	Version   string `json:"version"`
	// Update — состояние автообновления; nil — обновлений нет.
	Update *update.Status `json:"update,omitempty"`
}

// SessionReply — вкладка «Сессия»: настройка, работает ли счётчик и данные.
type SessionReply struct {
	Enabled bool            `json:"enabled"`
	Running bool            `json:"running"`
	Exists  bool            `json:"exists"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Start поднимает сервер. Возвращается, когда порт уже слушают.
func Start(a *app.App, o Options) (*Server, error) {
	if o.ReceiverAddr == "" {
		o.ReceiverAddr = receiver.Addr
	}
	if o.Shared == nil {
		addr := o.ReceiverAddr
		o.Shared = shared.New(func() bool { return receiver.Up(addr) })
	}
	if o.Lang == nil {
		o.Lang = i18n.System
	}
	if o.Token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		o.Token = hex.EncodeToString(b)
	}
	if o.Addr == "" {
		o.Addr = "127.0.0.1:0"
	}
	s := &Server{a: a, o: o, Token: o.Token}
	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return nil, err
	}
	s.ln = ln
	s.URL = "http://" + ln.Addr().String() + "/"
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln)
	return s, nil
}

// Close гасит сервер.
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

// Handler — все пути страницы (отдельно — для тестов).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	files := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			s.page(w)
			return
		}
		w.Header().Set("Cache-Control", "no-cache") // файлы меняются с каждой сборкой
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { s.json(w, s.state()) })
	mux.HandleFunc("GET /api/i18n", func(w http.ResponseWriter, r *http.Request) {
		lang := r.URL.Query().Get("lang")
		if lang == "" {
			lang = s.lang()
		}
		s.json(w, map[string]any{"lang": lang, "t": i18n.ForLang(lang)})
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) { s.json(w, s.a.Settings()) })
	mux.HandleFunc("POST /api/settings", s.setSettings)
	mux.HandleFunc("GET /api/own", func(w http.ResponseWriter, r *http.Request) {
		s.json(w, ownprices.Read(filepath.Join(s.o.DataDir, ownprices.FileName)))
	})
	mux.HandleFunc("GET /api/shared", func(w http.ResponseWriter, r *http.Request) {
		s.json(w, s.o.Shared.Get(r.Context(), r.URL.Query().Get("force") != ""))
	})
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) { s.json(w, s.session()) })
	mux.HandleFunc("POST /api/session/reset", func(w http.ResponseWriter, r *http.Request) { reply(w, s.a.ResetSession()) })
	mux.HandleFunc("POST /api/collect", func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.a.SetCollecting(r.FormValue("on") == "1"))
	})
	mux.HandleFunc("POST /api/receiver", func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.a.SetReceiver(r.FormValue("on") == "1"))
	})
	mux.HandleFunc("POST /api/open", func(w http.ResponseWriter, r *http.Request) {
		switch r.FormValue("what") {
		case "site":
			if s.o.OpenURL != nil {
				s.o.OpenURL(receiver.SiteURL)
			}
		case "discord":
			if s.o.OpenURL != nil {
				s.o.OpenURL(support.DiscordURL)
			}
		case "map":
			if s.o.OpenMap != nil {
				s.o.OpenMap()
			}
		case "data":
			if s.o.OpenFolder != nil {
				s.o.OpenFolder(s.o.DataDir)
			}
		default:
			reply(w, errors.New("что открыть?"))
			return
		}
		reply(w, nil)
	})
	mux.HandleFunc("POST /api/show", func(w http.ResponseWriter, r *http.Request) {
		if s.o.OnShow != nil {
			s.o.OnShow()
		}
		reply(w, nil)
	})
	mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if s.o.Update == nil {
			reply(w, errors.New("обновления выключены"))
			return
		}
		s.o.Update.Check()
		reply(w, nil)
	})
	mux.HandleFunc("POST /api/update/restart", func(w http.ResponseWriter, r *http.Request) {
		if s.o.OnUpdateRestart == nil {
			reply(w, errors.New("обновления выключены"))
			return
		}
		reply(w, s.o.OnUpdateRestart())
	})
	// Сведения для поддержки — POST: в них хвост журнала, отдаём только
	// своей странице (с ключом).
	mux.HandleFunc("POST /api/support", func(w http.ResponseWriter, r *http.Request) {
		text := ""
		if s.o.SupportInfo != nil {
			text = s.o.SupportInfo()
		}
		s.json(w, map[string]string{"text": text})
	})
	mux.HandleFunc("POST /api/bypass", func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.a.SetBypass(r.FormValue("mode")))
	})
	mux.HandleFunc("POST /api/probe", func(w http.ResponseWriter, r *http.Request) { reply(w, s.a.RunProbe()) })
	mux.HandleFunc("POST /api/trace", func(w http.ResponseWriter, r *http.Request) { reply(w, s.a.RunTrace()) })
	mux.HandleFunc("POST /api/record", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("stop") != "" {
			s.a.StopRecord()
			reply(w, nil)
			return
		}
		reply(w, s.a.StartRecord(10*time.Minute))
	})
	return s.guard(mux)
}

// guard: запросы только на наш адрес (защита от подмены DNS — чужой сайт
// под своим именем на 127.0.0.1), POST — только с ключом страницы.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "чужой адрес", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(TokenHeader) != s.Token {
			http.Error(w, "нет ключа страницы", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) page(w http.ResponseWriter) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	b = bytes.Replace(b, []byte("{{TOKEN}}"), []byte(s.Token), 1)
	// Оформление и кролик — сразу в разметке, чтобы при открытии окно не
	// мигало пиксельным видом до первого ответа /api/state.
	set := s.a.Settings()
	b = bytes.Replace(b, []byte("{{SKIN}}"), []byte(settings.NormalizeSkin(set.Skin)), 1)
	logo := "rabbit.png"
	if set.LogoAnim {
		logo = "rabbit.gif"
	}
	b = bytes.Replace(b, []byte("{{LOGO}}"), []byte(logo), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (s *Server) lang() string { return i18n.Resolve(s.a.Settings().Language, s.o.Lang()) }

func (s *Server) state() PageState {
	st := PageState{State: s.a.State(), Lang: s.lang(), Version: s.o.Version}
	if s.o.Update != nil {
		u := s.o.Update.Status()
		st.Update = &u
	}
	st.Receiver = receiver.Up(s.o.ReceiverAddr)
	st.SiteReady = st.Receiver && receiver.SiteReady(s.o.LogPath)
	return st
}

func (s *Server) session() SessionReply {
	st := s.a.State()
	rep := SessionReply{Enabled: st.Settings.SessionStats, Running: st.Collector.Running && st.Collector.Session}
	if s.o.SessionFile == "" {
		return rep
	}
	b, err := os.ReadFile(s.o.SessionFile)
	if err != nil {
		if !os.IsNotExist(err) {
			rep.Error = err.Error()
		}
		return rep
	}
	if !json.Valid(b) {
		rep.Error = "файл сессии битый"
		return rep
	}
	rep.Exists, rep.Data = true, b
	return rep
}

func (s *Server) setSettings(w http.ResponseWriter, r *http.Request) {
	old := s.a.Settings()
	cur := old
	// Поверх текущих: поля, которых нет в запросе, не трогаем.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&cur); err != nil {
		reply(w, err)
		return
	}
	cur.Language = strings.TrimSpace(cur.Language)
	if err := s.a.SetSettings(cur); err != nil {
		reply(w, err)
		return
	}
	var hookErr error
	if s.o.OnSettings != nil {
		hookErr = s.o.OnSettings(old, cur)
	}
	w.Header().Set("Content-Type", "application/json")
	if hookErr != nil {
		w.WriteHeader(http.StatusBadRequest)
	}
	resp := map[string]any{"settings": s.a.Settings()}
	if hookErr != nil {
		resp["error"] = hookErr.Error()
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
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
