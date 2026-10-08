package update

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Состояния для страницы (Status.State).
const (
	StateIdle        = "idle"        // ещё не проверяли
	StateChecking    = "checking"    // спрашиваем GitHub
	StateLatest      = "latest"      // у тебя последняя (Version — наша)
	StateDownloading = "downloading" // качаем (Version — какую)
	StateReady       = "ready"       // скачано и проверено
	StateError       = "error"       // не удалось проверить или скачать
	StateDev         = "dev"         // сборка без версии — обновления выключены
)

// Сроки как у мака.
const (
	FirstDelay  = 30 * time.Second
	Interval    = 6 * time.Hour
	MinInterval = time.Hour
	NetTimeout  = 15 * time.Second // ответ API и тишина при скачивании
	maxZip      = 300 << 20
)

// Status — что показывает страница.
type Status struct {
	State   string   `json:"state"`
	Version string   `json:"version,omitempty"`
	Current string   `json:"current"`
	Ready   *Release `json:"ready,omitempty"`
	// NoWrite — в папку программы не записать: ставить некуда.
	NoWrite bool `json:"noWrite"`
	// InstallFailed — «Перезапустить сейчас» не сработало (подробности в журнале).
	InstallFailed bool `json:"installFailed"`
	// Installing — процесс установки запущен, программа вот-вот закроется.
	Installing bool `json:"installing"`
}

// Config — всё, что нужно обновлятелю. Пустые поля — значения для Windows.
type Config struct {
	// Dir — каталог скачивания и распаковки. В Windows — StageDir():
	// %ProgramData%\Albion Journal\update, доступный на запись только
	// SYSTEM и администраторам (SecureStage), потому что отсюда файлы с
	// правами администратора копируются в папку программы и запускается
	// установщик. Каталог данных пользователя (%AppData%) для этого не годится.
	Dir        string
	Current    string // версия этой сборки
	ProgramDir string // папка, где лежит AlbionJournal.exe
	DataDir    string // каталог данных (передаётся установщику)
	PID        int    // наш pid (0 — os.Getpid)

	PublicKey ed25519.PublicKey // nil — PublicKey()
	APIURL    string            // пусто — APIURL
	Client    *http.Client      // nil — свой с таймаутами
	Auto      func() bool       // «Обновлять автоматически»; nil — всегда да
	Logf      func(string, ...any)
	Launch    func(exe string, args ...string) error // nil — StartDetached
	AllowURL  func(string) bool                      // nil — AllowedURL
	Writable  func(dir string) bool                  // nil — Writable
	Secure    func(dir string) error                 // nil — SecureStage
	TempDir   string                                 // пусто — os.TempDir()
	Now       func() time.Time                       // nil — time.Now
}

// Updater следит за выпусками и держит скачанное обновление наготове.
type Updater struct {
	c Config

	mu            sync.Mutex
	state         string
	version       string
	ready         *Release
	noWrite       bool
	installFailed bool
	busy          bool // проверка или загрузка идёт
	installing    bool
	stop          chan struct{}
}

// Имена в каталоге обновления.
const (
	zipFile   = ZipName
	sigFile   = SigName
	metaFile  = "готово.json"
	checkFile = "проверка.txt"
	checkDir  = "проверка"
	stageDir  = "ставлю"
	prevDir   = "предыдущая"
)

// New готовит обновлятель; проверки начинает Start.
func New(c Config) *Updater {
	if c.PublicKey == nil {
		c.PublicKey = PublicKey()
	}
	if c.APIURL == "" {
		c.APIURL = APIURL
	}
	if c.Client == nil {
		c.Client = NewClient()
	}
	if c.Auto == nil {
		c.Auto = func() bool { return true }
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	if c.Secure == nil {
		c.Secure = SecureStage
	}
	if c.Launch == nil {
		c.Launch = StartDetached
	}
	if c.AllowURL == nil {
		c.AllowURL = AllowedURL
	}
	if c.Writable == nil {
		c.Writable = Writable
	}
	if c.TempDir == "" {
		c.TempDir = os.TempDir()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.PID == 0 {
		c.PID = os.Getpid()
	}
	u := &Updater{c: c, state: StateIdle, stop: make(chan struct{})}
	if !ValidVersion(c.Current) {
		u.state = StateDev
	}
	return u
}

// NewClient — HTTP с таймаутами (15 с на соединение и ответ) и только https
// при переадресации.
func NewClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: NetTimeout}).DialContext,
		TLSHandshakeTimeout:   NetTimeout,
		ResponseHeaderTimeout: NetTimeout,
	}
	return &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("слишком много переадресаций")
		}
		if !RedirectAllowed(req.URL) {
			return fmt.Errorf("переадресация на чужой адрес: %s", req.URL.Redacted())
		}
		return nil
	}}
}

// RedirectAllowed — куда можно уйти по переадресации: только https и только
// хосты GitHub (github.com, api.github.com, *.githubusercontent.com — туда
// GitHub отправляет за вложениями выпусков, например
// release-assets.githubusercontent.com).
func RedirectAllowed(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "github.com" || h == "api.github.com" || strings.HasSuffix(h, ".githubusercontent.com")
}

// Status — для страницы.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := Status{State: u.state, Version: u.version, Current: u.c.Current,
		NoWrite: u.noWrite, InstallFailed: u.installFailed, Installing: u.installing}
	if u.ready != nil {
		r := *u.ready
		s.Ready = &r
	}
	return s
}

// Ready — готовое к установке обновление (nil — нет).
func (u *Updater) Ready() *Release {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.ready == nil {
		return nil
	}
	r := *u.ready
	return &r
}

// Installing — установка уже запущена.
func (u *Updater) Installing() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.installing
}

// Start: первая проверка через FirstDelay, дальше раз в Interval. Чаще
// раза в MinInterval не ходим (время помним в файле), даже если программу
// перезапускают подряд; тогда только подхватываем скачанное раньше.
func (u *Updater) Start() {
	go func() {
		t := time.NewTimer(FirstDelay)
		defer t.Stop()
		for {
			select {
			case <-u.stop:
				return
			case <-t.C:
			}
			u.Tick()
			t.Reset(Interval)
		}
	}()
}

// Stop останавливает таймер.
func (u *Updater) Stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	select {
	case <-u.stop:
	default:
		close(u.stop)
	}
}

// Tick — одна проверка по таймеру (отдельно — для тестов).
func (u *Updater) Tick() {
	if !u.c.Auto() || !ValidVersion(u.c.Current) {
		return
	}
	if last, ok := u.lastCheck(); ok && u.c.Now().Sub(last) < MinInterval {
		u.pickup()
		return
	}
	u.check()
}

// Check — кнопка «Проверить обновления»: без часового ограничения и даже
// при выключенном автообновлении. Не ждёт итога.
func (u *Updater) Check() { go u.CheckSync() }

// CheckSync — то же с ожиданием (для тестов).
func (u *Updater) CheckSync() {
	if !ValidVersion(u.c.Current) {
		u.mu.Lock()
		u.state = StateDev
		u.mu.Unlock()
		return
	}
	u.check()
}

func (u *Updater) set(state, version string) {
	u.mu.Lock()
	u.state, u.version = state, version
	u.mu.Unlock()
}

func (u *Updater) path(name string) string { return filepath.Join(u.c.Dir, name) }

func (u *Updater) lastCheck() (time.Time, bool) {
	b, err := os.ReadFile(u.path(checkFile))
	if err != nil {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(n, 0), true
}

func (u *Updater) check() {
	u.mu.Lock()
	// Кнопка и таймер могли совпасть: вторая проверка не нужна, а две
	// загрузки в один файл — тем более.
	if u.busy || u.installing {
		u.mu.Unlock()
		return
	}
	u.busy = true
	u.state, u.version = StateChecking, ""
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.busy = false
		u.mu.Unlock()
	}()

	if err := u.c.Secure(u.c.Dir); err != nil {
		u.c.Logf("папка обновления не готова: %v", err)
		u.set(StateError, "")
		return
	}
	os.WriteFile(u.path(checkFile), []byte(strconv.FormatInt(u.c.Now().Unix(), 10)), 0644)
	rel, err := u.fetch()
	if err != nil {
		u.c.Logf("проверка не удалась: %v", err)
		u.set(StateError, "")
		return
	}
	u.handle(rel)
}

func (u *Updater) fetch() (Release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), NetTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.c.APIURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AlbionJournal-Windows/"+u.c.Current)
	r, err := u.c.Client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return Release{}, err
	}
	return parseWith(b, u.c.AllowURL)
}

func (u *Updater) handle(rel Release) {
	if !Newer(rel.Version, u.c.Current) {
		// Мы и есть последняя (или выпуск — старый Zone Fix 0.x) — старое
		// скачанное больше не нужно.
		u.cleanDownloaded()
		u.mu.Lock()
		u.ready, u.state, u.version = nil, StateLatest, u.c.Current
		u.mu.Unlock()
		return
	}
	u.mu.Lock()
	if u.ready != nil && u.ready.Version == rel.Version {
		u.ready.Notes = rel.Notes // заметки могли поправить на GitHub
		r := *u.ready
		u.state, u.version = StateReady, rel.Version
		u.mu.Unlock()
		u.writeMeta(r)
		return
	}
	u.mu.Unlock()
	if !rel.HasAssets() {
		u.c.Logf("у выпуска %s нет %s и %s с github.com", rel.Version, ZipName, SigName)
		u.set(StateError, "")
		return
	}
	// Уже скачано в прошлый раз — проверяем заново и не качаем.
	if m, ok := u.readMeta(); ok && m.Version == rel.Version && u.verifyDownloaded(rel.Version) == nil {
		u.becomeReady(rel)
		return
	}
	u.set(StateDownloading, rel.Version)
	u.c.Logf("скачиваю %s: %s", rel.Version, rel.ZipURL)
	if err := u.download(rel); err != nil {
		u.c.Logf("загрузка не удалась: %v", err)
		u.cleanDownloaded()
		u.set(StateError, "")
		return
	}
	if err := u.verifyDownloaded(rel.Version); err != nil {
		u.c.Logf("обновление %s отвергнуто: %v — удалил", rel.Version, err)
		u.cleanDownloaded()
		u.set(StateError, "")
		return
	}
	u.c.Logf("обновление %s скачано и проверено", rel.Version)
	u.becomeReady(rel)
}

func (u *Updater) becomeReady(rel Release) {
	u.writeMeta(rel)
	noWrite := !u.c.Writable(u.c.ProgramDir)
	if noWrite {
		u.c.Logf("нет прав на запись в %s — обновление не поставить", u.c.ProgramDir)
	}
	u.mu.Lock()
	u.ready, u.state, u.version, u.noWrite = &rel, StateReady, rel.Version, noWrite
	u.mu.Unlock()
}

// pickup — после перезапуска программы: готовое обновление на диске
// показываем сразу, не дожидаясь сети.
func (u *Updater) pickup() {
	u.mu.Lock()
	has := u.ready != nil || u.busy || u.installing
	u.mu.Unlock()
	if has {
		return
	}
	if err := u.c.Secure(u.c.Dir); err != nil {
		u.c.Logf("папка обновления не готова: %v", err)
		return
	}
	m, ok := u.readMeta()
	if !ok {
		return
	}
	if !Newer(m.Version, u.c.Current) {
		// Это обновление уже стоит (мы — его новая копия): убираем остатки.
		u.cleanDownloaded()
		return
	}
	if err := u.verifyDownloaded(m.Version); err != nil {
		u.c.Logf("скачанное раньше обновление %s отвергнуто: %v — удалил", m.Version, err)
		u.cleanDownloaded()
		return
	}
	u.becomeReady(m)
}

func (u *Updater) readMeta() (Release, bool) {
	b, err := os.ReadFile(u.path(metaFile))
	if err != nil {
		return Release{}, false
	}
	var r Release
	if json.Unmarshal(b, &r) != nil || r.Version == "" {
		return Release{}, false
	}
	return r, true
}

func (u *Updater) writeMeta(r Release) {
	b, _ := json.Marshal(r)
	os.WriteFile(u.path(metaFile), b, 0644)
}

func (u *Updater) cleanDownloaded() {
	for _, n := range []string{zipFile, sigFile, metaFile, checkDir, stageDir} {
		os.RemoveAll(u.path(n)) // «ставлю» может держать работающий установщик — не страшно
	}
}

func (u *Updater) download(rel Release) error {
	if err := u.get(rel.SigURL, u.path(sigFile), 4096); err != nil {
		return fmt.Errorf("%s: %w", SigName, err)
	}
	if err := u.get(rel.ZipURL, u.path(zipFile), maxZip); err != nil {
		return fmt.Errorf("%s: %w", ZipName, err)
	}
	return nil
}

// get качает адрес в файл. 15 с — на тишину в соединении; весь файл может
// идти дольше (до 30 минут).
func (u *Updater) get(url, dst string, limit int64) error {
	if !u.c.AllowURL(url) {
		return fmt.Errorf("адрес не с github.com: %s", url)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AlbionJournal-Windows/"+u.c.Current)
	r, err := u.c.Client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", r.StatusCode)
	}
	idle := time.AfterFunc(NetTimeout, cancel)
	defer idle.Stop()
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(&idleReader{r: r.Body, t: idle}, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > limit {
		err = errors.New("файл слишком большой")
	}
	if err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, dst)
}

type idleReader struct {
	r io.Reader
	t *time.Timer
}

func (i *idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.t.Reset(NetTimeout)
	}
	return n, err
}

// verifyDownloaded: подпись zip (с версией внутри подписи) и состав.
func (u *Updater) verifyDownloaded(version string) error {
	_, err := u.unpack(checkDir, version)
	os.RemoveAll(u.path(checkDir))
	return err
}

// unpack проверяет подпись (версия выпуска входит в подписанное
// сообщение — скачанные exe не запускаем) и распаковывает zip в каталог
// name. Возвращает папку программы.
func (u *Updater) unpack(name, version string) (string, error) {
	sig, err := os.ReadFile(u.path(sigFile))
	if err != nil {
		return "", err
	}
	if err := Verify(u.c.PublicKey, u.path(zipFile), string(sig), version); err != nil {
		return "", err
	}
	dir := u.path(name)
	if err := Extract(u.path(zipFile), dir); err != nil {
		return "", err
	}
	return FindRoot(dir)
}

// Install запускает процесс установки; он сам дождётся нашего выхода.
// restart — «Перезапустить сейчас». Ошибка — ничего не запущено, программу
// можно не останавливать.
func (u *Updater) Install(restart bool) (err error) {
	u.mu.Lock()
	if u.installing {
		u.mu.Unlock()
		return errors.New("установка уже идёт")
	}
	if u.busy {
		u.mu.Unlock()
		return errors.New("идёт проверка обновлений")
	}
	if u.ready == nil {
		u.mu.Unlock()
		return errors.New("ставить нечего")
	}
	rel := *u.ready
	u.busy = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.busy = false
		if err == nil {
			u.installing, u.installFailed = true, false
		} else if restart {
			u.installFailed = true
		}
		u.mu.Unlock()
		if err != nil {
			u.c.Logf("обновление не поставлено: %v", err)
		}
	}()

	if inside(u.c.ProgramDir, u.c.TempDir) {
		return fmt.Errorf("программа запущена из временной папки %s (прямо из zip?) — некуда ставить", u.c.ProgramDir)
	}
	if !u.c.Writable(u.c.ProgramDir) {
		u.mu.Lock()
		u.noWrite = true
		u.mu.Unlock()
		return fmt.Errorf("нет прав на запись в %s", u.c.ProgramDir)
	}
	// Папка обновления должна быть по-прежнему только для администраторов,
	// и подпись — проверена ещё раз прямо перед установкой.
	if err := u.c.Secure(u.c.Dir); err != nil {
		return fmt.Errorf("папка обновления: %w", err)
	}
	root, err := u.unpack(stageDir, rel.Version)
	if err != nil {
		u.cleanDownloaded()
		u.mu.Lock()
		u.ready, u.state, u.version = nil, StateError, ""
		u.mu.Unlock()
		return err
	}
	plan := Plan{Src: root, Dest: u.c.ProgramDir, Prev: u.path(prevDir), PID: u.c.PID,
		Restart: restart, DataDir: u.c.DataDir}
	if err := u.c.Launch(filepath.Join(root, MainExe), plan.Args()...); err != nil {
		return fmt.Errorf("не запустить установку: %w", err)
	}
	how := "при выходе"
	if restart {
		how = "и перезапускаю"
	}
	u.c.Logf("ставлю %s %s", rel.Version, how)
	return nil
}

// inside — путь dir внутри base (без учёта регистра: Windows).
func inside(dir, base string) bool {
	if dir == "" || base == "" {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(base)), strings.ToLower(filepath.Clean(dir)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Writable — можно ли создавать файлы в папке.
func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".aj-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
