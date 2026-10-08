package update

import (
	"encoding/json"
	"testing"
)

const gh = "https://github.com/VAnatoliyV/albion-zone-fix/releases/download/v1.0.1/"

func release(t *testing.T, tag string, assets map[string]string, extra map[string]any) []byte {
	t.Helper()
	var as []map[string]string
	for n, u := range assets {
		as = append(as, map[string]string{"name": n, "browser_download_url": u})
	}
	o := map[string]any{"tag_name": tag, "body": "что нового\r\nстрока", "assets": as}
	for k, v := range extra {
		o[k] = v
	}
	b, _ := json.Marshal(o)
	return b
}

func TestParse(t *testing.T) {
	r, err := Parse(release(t, "v1.0.10", map[string]string{ZipName: gh + ZipName, SigName: gh + SigName, "README.txt": gh + "README.txt"}, nil))
	if err != nil || r.Version != "1.0.10" || r.ZipURL != gh+ZipName || r.SigURL != gh+SigName || !r.HasAssets() {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Notes != "что нового\nстрока" {
		t.Fatalf("заметки %q", r.Notes)
	}
	if _, err := Parse([]byte(`{"message":"Not Found"}`)); err == nil {
		t.Fatal("ответ без тега — ошибка")
	}
	if _, err := Parse([]byte(`мусор`)); err == nil {
		t.Fatal("мусор — ошибка")
	}
	if _, err := Parse(release(t, "v1.1.0", nil, map[string]any{"draft": true})); err == nil {
		t.Fatal("черновик не берём")
	}
	if _, err := Parse(release(t, "v1.1.0", nil, map[string]any{"prerelease": true})); err == nil {
		t.Fatal("предвыпуск не берём")
	}
	if _, err := Parse(release(t, "latest", nil, nil)); err == nil {
		t.Fatal("тег не версия — ошибка")
	}
	// Старый выпуск Zone Fix: без наших вложений.
	r, err = Parse(release(t, "v0.3.0", map[string]string{"AlbionZoneFix.zip": gh + "AlbionZoneFix.zip"}, nil))
	if err != nil || r.HasAssets() || r.Version != "0.3.0" {
		t.Fatalf("%+v %v", r, err)
	}
	// Подпись без zip или zip без подписи — не годится.
	if r, _ := Parse(release(t, "v1.2.0", map[string]string{ZipName: gh + ZipName}, nil)); r.HasAssets() {
		t.Fatal("без .sig не ставим")
	}
}

func TestAssetHost(t *testing.T) {
	cases := map[string]bool{
		gh + ZipName:                                            true,
		"https://GitHub.com/a/b/" + ZipName:                     true,
		"http://github.com/a/" + ZipName:                        false,
		"https://evil.example/" + ZipName:                       false,
		"https://github.com.evil.example/" + ZipName:            false,
		"https://objects.githubusercontent.com/" + ZipName:      false,
		"https://github.com:8443/" + ZipName:                    false,
		"https://user@github.com/" + ZipName:                    false,
		"https://evil.example/?u=https://github.com/" + ZipName: false,
		"github.com/" + ZipName:                                 false,
	}
	for u, want := range cases {
		if AllowedURL(u) != want {
			t.Errorf("AllowedURL(%q) = %v", u, !want)
		}
	}
	// Чужое вложение не вытесняет правильное.
	r, _ := Parse(release(t, "v1.0.1", map[string]string{ZipName: "https://evil.example/" + ZipName, SigName: gh + SigName}, nil))
	if r.ZipURL != "" || r.HasAssets() {
		t.Fatalf("zip с чужого хоста взят: %+v", r)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.10", "1.0.9", true},
		{"1.0.9", "1.0.10", false},
		{"1.1", "1.0.9", true},
		{"1.4", "1.4.0", false},
		{"1.4.0", "1.4", false},
		{"1.4.1", "1.4", true},
		{"2.0.0", "1.99.99", true},
		{"0.3.0", "1.0.0", false}, // Zone Fix старее любой 1.x
		{"0.99.99", "1.0.0", false},
		{"1.0.0", "0.3.0", true},
		{"1.4-beta", "1.3.9", true},
		{"1.0.0", "1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestVersionHelpers(t *testing.T) {
	if VersionFromTag("v1.2.3") != "1.2.3" || VersionFromTag(" V2.0 ") != "2.0" || VersionFromTag("1.0") != "1.0" {
		t.Fatal("VersionFromTag")
	}
	for v, want := range map[string]bool{"1.0.0": true, "1.0": true, "dev": false, "": false, "1": false, "1.x": false, "1.0.0-rc": false} {
		if ValidVersion(v) != want {
			t.Errorf("ValidVersion(%q)", v)
		}
	}
}
