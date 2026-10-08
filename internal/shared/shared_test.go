package shared

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const sample = `{"t":1791437421,"cities":["a","b","c"],"c":{"T4_BAG":{},"T5_BAG":{}},"bo":{"T5_BAG":{},"T6_SWORD":{}},"m":{"T4_ORE":{}}}`

func TestParse(t *testing.T) {
	s, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if s.SnapshotTs != 1791437421 || s.Cities != 3 || s.Items != 3 || s.BlackMarket != 2 || s.Resources != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestLoaderPrefersLocalAndCaches(t *testing.T) {
	var pub, loc atomic.Int32
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { pub.Add(1); w.Write([]byte(sample)) }))
	defer public.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { loc.Add(1); w.Write([]byte(sample)) }))
	defer local.Close()

	up := true
	l := New(func() bool { return up })
	l.Public, l.Local = public.URL, local.URL
	s := l.Get(context.Background(), false)
	if !s.FromLocal || s.Failed || loc.Load() != 1 || pub.Load() != 0 {
		t.Fatalf("%+v loc=%d pub=%d", s, loc.Load(), pub.Load())
	}
	l.Get(context.Background(), false)
	if loc.Load() != 1 {
		t.Fatal("свежий снимок перечитан без нужды")
	}
	up = false
	s = l.Get(context.Background(), true)
	if s.FromLocal || pub.Load() != 1 {
		t.Fatalf("без приёмника — с GitHub: %+v", s)
	}
}

func TestLoaderFailureKeepsGoodSnapshot(t *testing.T) {
	ok := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok {
			http.Error(w, "нет", 500)
			return
		}
		w.Write([]byte(sample))
	}))
	defer srv.Close()
	l := New(nil)
	l.Public = srv.URL
	if s := l.Get(context.Background(), false); s.Failed {
		t.Fatal(s.Error)
	}
	ok = false
	if s := l.Get(context.Background(), true); s.Failed || s.Items != 3 {
		t.Fatalf("неудача затёрла хороший снимок: %+v", s)
	}
	l2 := New(nil)
	l2.Public = srv.URL
	if s := l2.Get(context.Background(), false); !s.Failed || s.Error == "" {
		t.Fatalf("первая неудача должна быть видна: %+v", s)
	}
}
