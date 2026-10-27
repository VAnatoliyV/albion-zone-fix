package chestlog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// NamesURL — таблица названий на сайте (обновляется вместе с сайтом).
var NamesURL = "https://vanatoliyv.github.io/albion-craft-profit/item_loc.json"

// NamesFile — имя таблицы в каталоге данных и рядом с программой.
const NamesFile = "item_loc.json"

// Names — названия предметов на всех языках → коды.
type Names struct {
	mu    sync.Mutex
	index map[string][]string
}

// Lookup — коды предметов с таким названием (без учёта регистра).
func (n *Names) Lookup(name string) []string {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.index[norm(name)]
}

func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// ParseNames читает item_loc.json: {"langs": [...], "items": {"CODE": ["name", ...]}}.
func ParseNames(data []byte) (*Names, error) {
	var raw struct {
		Items map[string][]string `json:"items"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if len(raw.Items) < 1000 {
		return nil, fmt.Errorf("в таблице названий всего %d предметов", len(raw.Items))
	}
	idx := make(map[string][]string, len(raw.Items)*4)
	for code, list := range raw.Items {
		seen := map[string]bool{}
		for _, name := range list {
			k := norm(name)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			idx[k] = append(idx[k], code)
		}
	}
	return &Names{index: idx}, nil
}

var (
	loadMu   sync.Mutex
	loaded   *Names
	loadedAt time.Time
)

// Load отдаёт таблицу названий: свежую из каталога данных, иначе скачивает
// с сайта, иначе берёт старую копию или ту, что лежит рядом с программой.
func Load(dataDir, resDir string) (*Names, error) {
	loadMu.Lock()
	defer loadMu.Unlock()
	if loaded != nil && time.Since(loadedAt) < 12*time.Hour {
		return loaded, nil
	}
	cached := filepath.Join(dataDir, NamesFile)
	if st, err := os.Stat(cached); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		if n, err := readNames(cached); err == nil {
			loaded, loadedAt = n, time.Now()
			return n, nil
		}
	}
	n, dlErr := download(cached)
	if dlErr == nil {
		loaded, loadedAt = n, time.Now()
		return n, nil
	}
	for _, p := range []string{cached, filepath.Join(resDir, NamesFile)} {
		if n, err := readNames(p); err == nil {
			loaded, loadedAt = n, time.Now()
			return n, nil
		}
	}
	return nil, fmt.Errorf("таблица названий предметов не загрузилась: %v", dlErr)
}

func readNames(path string) (*Names, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseNames(b)
}

func download(path string) (*Names, error) {
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Get(NamesURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	n, err := ParseNames(b)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			os.Rename(tmp, path)
		}
	}
	return n, nil
}
