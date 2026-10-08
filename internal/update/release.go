// Пакет update — автообновление Albion Journal для Windows из выпусков
// GitHub, подписанных нашим ключом ed25519.
//
// Как это устроено (повторяет мак-версию, app/Обновление.swift):
//  1. первая проверка через 30 с после запуска, дальше раз в 6 часов; чаще
//     раза в час не ходим, даже если программу перезапускают подряд;
//  2. если последний выпуск новее нас — тихо качаем AlbionJournal.zip и
//     AlbionJournal.zip.sig (только https://github.com/...), проверяем
//     подпись вшитым открытым ключом, распаковываем во временную папку и
//     сверяем версию внутри (AlbionJournal.exe -version) с тегом;
//  3. ставит отдельный процесс — новая AlbionJournal.exe, запущенная из
//     распакованной папки с -apply-update: ждёт выхода программы, меняет
//     файлы, при ошибке откатывает, по «Перезапустить сейчас» запускает
//     новую версию.
package update

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Repo — репозиторий выпусков Windows (там же старые выпуски Zone Fix v0.x).
const Repo = "VAnatoliyV/albion-zone-fix"

// APIURL — последний выпуск.
const APIURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// Имена вложений выпуска.
const (
	ZipName = "AlbionJournal.zip"
	SigName = ZipName + ".sig"
)

// Release — выпуск с GitHub: версия без «v», заметки «что нового», ссылки.
// ZipURL и SigURL пустые, если у выпуска нет наших вложений (старый Zone Fix).
type Release struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
	ZipURL  string `json:"-"`
	SigURL  string `json:"-"`
}

// Parse разбирает ответ releases/latest. Черновик и предвыпуск — ошибка.
// Вложения берутся только с https://github.com/ (AllowedURL).
func Parse(data []byte) (Release, error) { return parseWith(data, AllowedURL) }

func parseWith(data []byte, allow func(string) bool) (Release, error) {
	var r struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Release{}, err
	}
	if r.Tag == "" {
		return Release{}, errors.New("в ответе нет tag_name")
	}
	if r.Draft || r.Prerelease {
		return Release{}, errors.New("черновик или предвыпуск")
	}
	v := VersionFromTag(r.Tag)
	if v == "" || v[0] < '0' || v[0] > '9' {
		return Release{}, errors.New("тег не похож на версию: " + r.Tag)
	}
	rel := Release{Version: v, Notes: strings.TrimSpace(strings.ReplaceAll(r.Body, "\r\n", "\n"))}
	for _, a := range r.Assets {
		if !allow(a.URL) {
			continue
		}
		switch a.Name {
		case ZipName:
			if rel.ZipURL == "" {
				rel.ZipURL = a.URL
			}
		case SigName:
			if rel.SigURL == "" {
				rel.SigURL = a.URL
			}
		}
	}
	return rel, nil
}

// HasAssets — у выпуска есть и zip, и подпись.
func (r Release) HasAssets() bool { return r.ZipURL != "" && r.SigURL != "" }

// VersionFromTag: «v1.4» → «1.4». Без «v» — как есть.
func VersionFromTag(tag string) string {
	t := strings.TrimSpace(tag)
	if strings.HasPrefix(t, "v") || strings.HasPrefix(t, "V") {
		return t[1:]
	}
	return t
}

// AllowedURL — только https и хост ровно github.com: там живут
// browser_download_url выпусков. Переадресация на
// objects.githubusercontent.com при скачивании — нормально: подлинность
// проверяет подпись, а не адрес.
func AllowedURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "https" && strings.EqualFold(u.Hostname(), "github.com") && u.Port() == "" && u.User == nil
}

// Newer — a новее b? По числам: 1.0.10 > 1.0.9, 1.4 == 1.4.0. Нечисловой
// хвост («1.4-beta») отбрасывается. Старые выпуски Zone Fix (0.x) поэтому
// старее любой 1.x.
func Newer(a, b string) bool {
	x, y := nums(a), nums(b)
	for len(x) < len(y) {
		x = append(x, 0)
	}
	for len(y) < len(x) {
		y = append(y, 0)
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func nums(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ".") {
		n := 0
		for i := 0; i < len(part) && part[i] >= '0' && part[i] <= '9'; i++ {
			n = n*10 + int(part[i]-'0')
		}
		out = append(out, n)
	}
	return out
}

// ValidVersion — версия сборки похожа на «1.2.3» (не «dev»).
func ValidVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}
