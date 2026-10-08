package ocr

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// Фикстура — вывод скрипта в том виде, в каком его отдаёт PowerShell 5.1:
// BOM, \r\n, посторонние предупреждения, одна строка не массивом, null.
func TestParseRecognize(t *testing.T) {
	b, err := os.ReadFile("testdata/recognize.out")
	if err != nil {
		t.Fatal(err)
	}
	o := Parse(b)
	if !reflect.DeepEqual(o.Lines["ru-RU"], []string{"Путь Авалона в", "Cebos-Avemlum", "7/7", "Закроется через 23 м 14 с"}) {
		t.Errorf("%q", o.Lines["ru-RU"])
	}
	if !reflect.DeepEqual(o.Lines["en-US"], []string{"Road of Avalon to"}) {
		t.Errorf("одна строка не массивом: %q", o.Lines["en-US"])
	}
	if l, ok := o.Lines["de-DE"]; !ok || len(l) != 0 {
		t.Errorf("null: %q %v", l, ok)
	}
	if !reflect.DeepEqual(o.Skipped, []string{"es-ES"}) || o.Err != "" {
		t.Errorf("%+v", o)
	}
}

func TestParseLangsAndError(t *testing.T) {
	b, _ := os.ReadFile("testdata/langs.out")
	if o := Parse(b); !reflect.DeepEqual(o.Langs, []string{"en-US", "ru-RU"}) {
		t.Errorf("%v", o.Langs)
	}
	if o := Parse([]byte(`{"kind":"error","msg":"Файл не найден"}`)); o.Err != "Файл не найден" {
		t.Error(o.Err)
	}
	if o := Parse(nil); len(o.Lines) != 0 || o.Err != "" {
		t.Error("пустой вывод")
	}
}

func TestPickAndHint(t *testing.T) {
	if p := Pick([]string{"de-DE", "en-GB", "ru-RU", "en-US"}); !reflect.DeepEqual(p, []string{"ru-RU", "en-GB"}) {
		t.Error(p)
	}
	if p := Pick(nil); !reflect.DeepEqual(p, []string{"ru-RU", "en-US"}) {
		t.Error(p)
	}
	cases := map[string][]string{HintNone: {"de-DE"}, HintNoRu: {"en-US"}, HintNoEn: {"ru-RU"}, "": {"ru-RU", "en-US"}}
	for want, in := range cases {
		if h := Hint(in); h != want {
			t.Errorf("%v: %q, ждали %q", in, h, want)
		}
	}
}

func TestStdinAndScript(t *testing.T) {
	// Ввод PowerShell читает в кодировке консоли: в скрипте только ASCII.
	for i, r := range Script {
		if r > 127 {
			t.Fatalf("не ASCII в скрипте на %d: %q", i, string(r))
		}
	}
	for _, s := range []string{"IAsyncOperation`1", "AJ_OCR_PATH", "AJ_OCR_LANGS", "UTF8Encoding $false", "TryCreateFromLanguage"} {
		if !strings.Contains(Script, s) {
			t.Errorf("в скрипте нет %q", s)
		}
	}
}
