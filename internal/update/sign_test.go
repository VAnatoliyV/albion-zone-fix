package update

import (
	"crypto/ed25519"
	"crypto/rand"
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
	sig, err := Sign(k, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, f, sig); err != nil {
		t.Fatalf("верная подпись: %v", err)
	}
	if err := Verify(pub, f, "  "+sig+"\r\n"); err != nil {
		t.Fatalf("пробелы вокруг: %v", err)
	}
	// Чужой ключ.
	other := testKey(t)
	if Verify(other.Public().(ed25519.PublicKey), f, sig) != ErrBadSignature {
		t.Fatal("подпись чужим ключом прошла")
	}
	osig, _ := Sign(other, f)
	if Verify(pub, f, osig) != ErrBadSignature {
		t.Fatal("файл, подписанный чужим ключом, прошёл")
	}
	// Подменённый файл.
	os.WriteFile(f, []byte("архив!"), 0644)
	if Verify(pub, f, sig) != ErrBadSignature {
		t.Fatal("подменённый файл прошёл")
	}
	// Битая подпись.
	for _, s := range []string{"", "не base64", base64.StdEncoding.EncodeToString([]byte("коротко"))} {
		if Verify(pub, f, s) == nil {
			t.Fatalf("битая подпись %q прошла", s)
		}
	}
	if Verify(nil, f, sig) == nil {
		t.Fatal("без ключа — ошибка")
	}
	if Verify(pub, filepath.Join(dir, "нет"), sig) == nil {
		t.Fatal("нет файла — ошибка")
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
