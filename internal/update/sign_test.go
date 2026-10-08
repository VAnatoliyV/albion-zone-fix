package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEmbeddedKey(t *testing.T) {
	if len(PublicKey()) != ed25519.PublicKeySize {
		t.Fatal("вшитый открытый ключ битый")
	}
}

func TestVerify(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ZipName)
	os.WriteFile(f, []byte("архив"), 0644)
	k := testKey(t)
	pub := k.Public().(ed25519.PublicKey)
	sig, err := Sign(k, f, "1.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, f, sig, "1.0.1"); err != nil {
		t.Fatalf("верная подпись: %v", err)
	}
	if err := Verify(pub, f, "  "+sig+"\r\n", "1.0.1"); err != nil {
		t.Fatalf("пробелы вокруг: %v", err)
	}
	// Другая версия (тег не тот, что подписан).
	for _, v := range []string{"1.0.2", "1.0.10", "1.0.1 ", "v1.0.1", ""} {
		if Verify(pub, f, sig, v) != ErrBadSignature {
			t.Errorf("подпись 1.0.1 прошла для версии %q", v)
		}
	}
	// Чужой ключ.
	other := testKey(t)
	if Verify(other.Public().(ed25519.PublicKey), f, sig, "1.0.1") != ErrBadSignature {
		t.Fatal("подпись чужим ключом прошла")
	}
	osig, _ := Sign(other, f, "1.0.1")
	if Verify(pub, f, osig, "1.0.1") != ErrBadSignature {
		t.Fatal("файл, подписанный чужим ключом, прошёл")
	}
	// Подменённый файл (другой хеш).
	os.WriteFile(f, []byte("архив!"), 0644)
	if Verify(pub, f, sig, "1.0.1") != ErrBadSignature {
		t.Fatal("подменённый файл прошёл")
	}
	// Битая подпись.
	for _, s := range []string{"", "не base64", base64.StdEncoding.EncodeToString([]byte("коротко"))} {
		if Verify(pub, f, s, "1.0.1") == nil {
			t.Fatalf("битая подпись %q прошла", s)
		}
	}
	if Verify(nil, f, sig, "1.0.1") == nil {
		t.Fatal("без ключа — ошибка")
	}
	if Verify(pub, filepath.Join(dir, "нет"), sig, "1.0.1") == nil {
		t.Fatal("нет файла — ошибка")
	}
	if _, err := Sign(k, f, "dev"); err == nil {
		t.Fatal("подписывать можно только версию 1.2.3")
	}
}

// Сообщение — ровно «версия\nhex(sha256)»: этот формат описан в отчёте и
// в sign.go, менять его — значит сломать все выпуски.
func TestMessageFormat(t *testing.T) {
	d := sha256.Sum256([]byte("abc"))
	got := string(Message("1.0.1", d[:]))
	if got != "1.0.1\nba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal(got)
	}
}

func TestPrivateRoundTrip(t *testing.T) {
	k := testKey(t)
	k2, err := ParsePrivate(EncodePrivate(k))
	if err != nil || !k.Equal(k2) {
		t.Fatal("ключ не читается обратно")
	}
	if _, err := ParsePrivate("abc"); err == nil {
		t.Fatal("битый ключ")
	}
}
