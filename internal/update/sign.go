package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// PublicKeyB64 — открытый ключ выпусков (ed25519, base64). Пара создана
// tools/updatekey; закрытая половина лежит только у автора на маке
// (~/.config/albion-journal/windows-update.key) и в репозиторий не попадает.
const PublicKeyB64 = "r1NRl67z+Ej6cS0kcOKiGRTJ5yObk76Tpgk4MITZPNU="

// PublicKey — вшитый ключ, которым проверяется каждое обновление.
func PublicKey() ed25519.PublicKey {
	k, err := base64.StdEncoding.DecodeString(PublicKeyB64)
	if err != nil || len(k) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(k)
}

// Формат подписи (AlbionJournal.zip.sig):
//
//	одна строка base64 (стандартный алфавит, с «=») — 64 байта подписи
//	ed25519 над сообщением
//
//	    <версия> "\n" <SHA-256 всего zip-файла, 64 строчные hex-цифры>
//
//	например "1.0.1\n3f2a…9c". Версия — без «v», ровно как в теге выпуска.
//
// Версия входит в подпись, поэтому старый подписанный zip нельзя выложить
// под новым тегом, а программе не нужно запускать скачанный exe, чтобы
// узнать его версию. Zip хешируется потоком, в память целиком не грузится.
// Пробелы и перевод строки вокруг base64 допустимы.

// Digest — SHA-256 файла.
func Digest(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// Message — что подписывается: версия, перевод строки, hex SHA-256 zip.
func Message(version string, digest []byte) []byte {
	return []byte(version + "\n" + hex.EncodeToString(digest))
}

// Sign — текст .sig для файла этой версии (с переводом строки в конце).
func Sign(priv ed25519.PrivateKey, path, version string) (string, error) {
	if !ValidVersion(version) {
		return "", fmt.Errorf("версия %q не похожа на 1.2.3", version)
	}
	d, err := Digest(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, Message(version, d))) + "\n", nil
}

// ErrBadSignature — подпись не сошлась (файл подменён, версия не та или
// подписано не нашим ключом).
var ErrBadSignature = errors.New("подпись не сошлась")

// Verify проверяет файл по тексту .sig, ожидаемой версии (из тега) и
// открытому ключу.
func Verify(pub ed25519.PublicKey, path, sigText, version string) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("нет открытого ключа")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigText))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("файл подписи битый")
	}
	d, err := Digest(path)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, Message(version, d), sig) {
		return ErrBadSignature
	}
	return nil
}

// ParsePrivate читает закрытый ключ из файла ключа: base64 32-байтового
// зерна ed25519 (одна строка).
func ParsePrivate(text string) (ed25519.PrivateKey, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("файл ключа битый")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// EncodePrivate — текст файла ключа.
func EncodePrivate(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"
}
