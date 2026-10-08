package notify

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"

	"albionzonefix/internal/desktop"
)

func TestXML(t *testing.T) {
	x := XML("Qiient-Al-Vynsis", "🌀 дорога убежищ · T6", "🟦 2 × средний <ветеранский> & \"x\"\nволокно\x01")
	var v struct {
		Visual struct {
			Binding struct {
				Template string   `xml:"template,attr"`
				Text     []string `xml:"text"`
			} `xml:"binding"`
		} `xml:"visual"`
	}
	if err := xml.Unmarshal([]byte(x), &v); err != nil {
		t.Fatal(err, x)
	}
	tx := v.Visual.Binding.Text
	if v.Visual.Binding.Template != "ToastGeneric" || len(tx) != 3 || tx[0] != "Qiient-Al-Vynsis" ||
		tx[2] != "🟦 2 × средний <ветеранский> & \"x\"\nволокно" {
		t.Fatalf("%q", tx)
	}
	if !strings.Contains(x, `silent="true"`) {
		t.Error("без звука, как у мака")
	}
	if n := strings.Count(XML("a", "", "c"), "<text>"); n != 2 {
		t.Error("пустой подзаголовок пропускается", n)
	}
}

func TestPNGFromICO(t *testing.T) {
	p := PNGFromICO(desktop.Icon)
	if p == nil || !bytes.HasPrefix(p, []byte("\x89PNG")) {
		t.Fatal("в кролике нет PNG")
	}
	if PNGFromICO([]byte("мусор")) != nil || PNGFromICO(nil) != nil {
		t.Fatal("мусор")
	}
}
