package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAskShowWaitsForUIFile(t *testing.T) {
	shown := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" || r.Header.Get("X-AJ-Token") != "k" {
			http.Error(w, "нет", 403)
			return
		}
		shown++
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), uiFile)
	// Первая копия записывает ui.json чуть позже.
	go func() {
		time.Sleep(500 * time.Millisecond)
		b, _ := json.Marshal(map[string]string{"url": srv.URL + "/", "token": "k"})
		os.WriteFile(path, b, 0600)
	}()
	if err := askShow(path, 5*time.Second); err != nil || shown != 1 {
		t.Fatalf("err=%v shown=%d", err, shown)
	}
}

func TestAskShowReportsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "нет", 403) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), uiFile)
	if err := askShow(path, 400*time.Millisecond); err == nil {
		t.Fatal("нет ui.json — должна быть ошибка")
	}
	b, _ := json.Marshal(map[string]string{"url": srv.URL + "/", "token": "чужой"})
	os.WriteFile(path, b, 0600)
	if err := askShow(path, 400*time.Millisecond); err == nil {
		t.Fatal("отказ первой копии (403) должен быть ошибкой")
	}
}
