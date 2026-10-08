// updatekey — ключ подписи обновлений Albion Journal для Windows.
//
//	go run ./tools/updatekey gen           создать пару (один раз; файл не перезаписывается)
//	go run ./tools/updatekey pub           открытый ключ из файла ключа (для update.PublicKeyB64)
//	go run ./tools/updatekey sign FILE VERSION     записать FILE.sig (подпись над «VERSION\nsha256»)
//	go run ./tools/updatekey verify FILE VERSION   проверить FILE.sig вшитым в программу ключом
//
// Закрытый ключ живёт в ~/.config/albion-journal/windows-update.key (права
// 600) и никогда не печатается. Путь можно сменить переменной AJ_UPDATE_KEY.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"albionzonefix/internal/update"
)

func keyPath() string {
	if p := os.Getenv("AJ_UPDATE_KEY"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		die("нет домашнего каталога: %v", err)
	}
	return filepath.Join(home, ".config", "albion-journal", "windows-update.key")
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "updatekey: "+format+"\n", a...)
	os.Exit(1)
}

func load() ed25519.PrivateKey {
	p := keyPath()
	st, err := os.Stat(p)
	if err != nil {
		die("нет файла ключа %s — подписать нечем (создать: go run ./tools/updatekey gen)", p)
	}
	if st.Mode().Perm()&0077 != 0 {
		die("у файла ключа %s слишком открытые права %v — нужно chmod 600", p, st.Mode().Perm())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		die("%v", err)
	}
	k, err := update.ParsePrivate(string(b))
	if err != nil {
		die("%s: %v", p, err)
	}
	return k
}

func pubOf(k ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}

func main() {
	if len(os.Args) < 2 {
		die("команда: gen | pub | sign FILE VERSION | verify FILE VERSION")
	}
	switch os.Args[1] {
	case "gen":
		p := keyPath()
		if _, err := os.Stat(p); err == nil {
			die("ключ уже есть: %s — не перезаписываю", p)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			die("%v", err)
		}
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			die("%v", err)
		}
		// O_EXCL: даже если файл появился между проверкой и записью — не трогаем.
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			die("%v", err)
		}
		if _, err := f.WriteString(update.EncodePrivate(priv)); err != nil {
			f.Close()
			os.Remove(p)
			die("%v", err)
		}
		f.Close()
		os.Chmod(p, 0600)
		fmt.Println("закрытый ключ записан:", p)
		fmt.Println("открытый ключ:", pubOf(priv))
	case "pub":
		fmt.Println(pubOf(load()))
	case "sign":
		if len(os.Args) < 4 {
			die("sign FILE VERSION")
		}
		k := load()
		if pubOf(k) != update.PublicKeyB64 {
			die("ключ в %s не совпадает с вшитым в программу (update.PublicKeyB64)", keyPath())
		}
		sig, err := update.Sign(k, os.Args[2], os.Args[3])
		if err != nil {
			die("%v", err)
		}
		if err := os.WriteFile(os.Args[2]+".sig", []byte(sig), 0644); err != nil {
			die("%v", err)
		}
		fmt.Println("подпись:", os.Args[2]+".sig", "версия", os.Args[3])
	case "verify":
		if len(os.Args) < 4 {
			die("verify FILE VERSION")
		}
		b, err := os.ReadFile(os.Args[2] + ".sig")
		if err != nil {
			die("%v", err)
		}
		if err := update.Verify(update.PublicKey(), os.Args[2], string(b), os.Args[3]); err != nil {
			die("%s: %v", os.Args[2], err)
		}
		fmt.Println("подпись верна:", os.Args[2], "версия", os.Args[3])
	default:
		die("неизвестная команда %q", os.Args[1])
	}
}
