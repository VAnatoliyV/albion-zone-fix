package autostart

import (
	"encoding/xml"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTaskXML(t *testing.T) {
	task := Task{Exe: `C:\Игры\Albion & Co\AlbionJournal.exe`, Dir: `C:\Игры\Albion & Co`, UserID: `PC\Анатолий`}
	x := task.XML()
	// XML разбирается, спецсимволы экранированы.
	var v struct {
		Principals struct {
			Principal struct {
				RunLevel  string
				LogonType string
				UserId    string
			}
		}
		Settings struct {
			ExecutionTimeLimit         string
			DisallowStartIfOnBatteries bool
			StopIfGoingOnBatteries     bool
			MultipleInstancesPolicy    string
		}
		Triggers struct {
			LogonTrigger struct{ UserId string }
		}
		Actions struct {
			Exec struct{ Command, Arguments, WorkingDirectory string }
		}
	}
	dec := xml.NewDecoder(strings.NewReader(strings.Replace(x, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)))
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("XML не разбирается: %v\n%s", err, x)
	}
	p := v.Principals.Principal
	if p.RunLevel != "HighestAvailable" || p.LogonType != "InteractiveToken" || p.UserId != task.UserID {
		t.Fatalf("права: %+v", p)
	}
	s := v.Settings
	if s.ExecutionTimeLimit != "PT0S" || s.DisallowStartIfOnBatteries || s.StopIfGoingOnBatteries || s.MultipleInstancesPolicy != "IgnoreNew" {
		t.Fatalf("настройки: %+v", s)
	}
	if v.Triggers.LogonTrigger.UserId != task.UserID {
		t.Fatal("вход не того пользователя")
	}
	e := v.Actions.Exec
	if e.Command != task.Exe || e.Arguments != Flag || e.WorkingDirectory != task.Dir {
		t.Fatalf("действие: %+v", e)
	}
	if CommandOf(x) != task.Exe {
		t.Fatalf("CommandOf: %q", CommandOf(x))
	}
}

func TestUTF16(t *testing.T) {
	b := UTF16("я\nb")
	if b[0] != 0xFF || b[1] != 0xFE {
		t.Fatal("нет BOM")
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
	}
	if string(utf16.Decode(u)) != "я\r\nb" {
		t.Fatalf("%q", string(utf16.Decode(u)))
	}
}

func TestArgs(t *testing.T) {
	if strings.Join(CreateArgs(`C:\t.xml`), " ") != `/Create /TN Albion Journal /XML C:\t.xml /F` {
		t.Fatal(CreateArgs(`C:\t.xml`))
	}
	if strings.Join(DeleteArgs(), " ") != "/Delete /TN Albion Journal /F" {
		t.Fatal(DeleteArgs())
	}
	if CommandOf("<Task><Exec><Command>\"C:\\a b\\x.exe\"</Command></Exec></Task>") != `C:\a b\x.exe` {
		t.Fatal("кавычки вокруг пути")
	}
	if CommandOf("мусор") != "" {
		t.Fatal("мусор")
	}
}

func TestSyncOffElsewhere(t *testing.T) {
	if err := Sync(false); err != nil {
		t.Fatal(err)
	}
}
