package ownprices

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMissing(t *testing.T) {
	s := Read(filepath.Join(t.TempDir(), FileName))
	if s.Exists || s.Error != "" || s.Positions != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestReadBroken(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(p, []byte("{"), 0644)
	if s := Read(p); s.Exists || s.Error == "" {
		t.Fatalf("%+v", s)
	}
}

func TestReadSummary(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(p, []byte(`{"prices":{
		"T4_BAG|Lymhurst|1":{"sell":1200,"sellTs":100},
		"T5_ORE|Martlock|1":{"buy":300,"buyTs":250,"sell":350,"sellTs":200},
		"T4_BAG|Martlock|2":{"sell":1500,"sellTs":250},
		"BROKEN":{"sell":1,"sellTs":5}
	},"built":260,"seenOrders":4321}`), 0644)
	s := Read(p)
	if !s.Exists || s.Positions != 4 || s.Orders != 4321 || s.Cities != 3 || s.LastTs != 250 {
		t.Fatalf("%+v", s)
	}
	// по времени, при равенстве — по ключу
	if s.Recent[0].ID != "T4_BAG|Martlock|2" || s.Recent[1].ID != "T5_ORE|Martlock|1" || s.Recent[3].Name != "BROKEN" || s.Recent[3].City != "—" {
		t.Fatalf("порядок: %+v", s.Recent)
	}
	ore := s.Recent[1]
	if ore.Name != "T5_ORE" || ore.City != "Martlock" || ore.Quality != "1" || *ore.Buy != 300 || *ore.Sell != 350 {
		t.Fatalf("%+v", ore)
	}
	if s.Recent[0].Buy != nil {
		t.Fatal("нет бай-ордера — не должно быть и цены сдачи")
	}
}
