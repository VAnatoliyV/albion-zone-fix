//go:build windows

package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func programData() (string, error) {
	// Настоящая папка из реестра, а не %ProgramData%: переменную окружения
	// мог подменить кто угодно.
	return windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
}

// StageDir — %ProgramData%\Albion Journal\update.
func StageDir() (string, error) {
	pd, err := programData()
	if err != nil {
		return "", err
	}
	return filepath.Join(pd, "Albion Journal", "update"), nil
}

// SecureStage проверяет перед каждым использованием, что папка обновления
// и её родитель (%ProgramData%\Albion Journal) — наши: не точки повторной
// обработки (junction, symlink), владелец и DACL как stageSDDL. Чужую
// папку удаляет и создаёт заново с нашими правами; не вышло — ошибка, и
// обновление не идёт.
func SecureStage(dir string) error {
	pd, err := programData()
	if err != nil {
		return err
	}
	root := filepath.Dir(filepath.Clean(dir))
	if !strings.EqualFold(filepath.Dir(root), filepath.Clean(pd)) {
		return fmt.Errorf("папка обновления %s не в %s", dir, pd)
	}
	if err := secureOne(root); err != nil {
		return err
	}
	return secureOne(dir)
}

func secureOne(p string) error {
	ok, exists, err := checkOne(p)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	if exists {
		if IsReparse(p) {
			err = os.Remove(p) // только саму ссылку, не то, куда она ведёт
		} else {
			err = os.RemoveAll(p)
		}
		if err != nil {
			return fmt.Errorf("папка %s чужая и не удаляется: %w", p, err)
		}
	}
	sd, err := windows.SecurityDescriptorFromString(stageSDDL)
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	p16, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(p16, sa); err != nil {
		return fmt.Errorf("не создать %s: %w", p, err)
	}
	if ok, _, err := checkOne(p); err != nil || !ok {
		return fmt.Errorf("папка %s создана, но права не те", p)
	}
	return nil
}

// checkOne: ok — папка наша; exists — по пути что-то есть.
func checkOne(p string) (ok, exists bool, err error) {
	p16, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return false, false, err
	}
	attrs, err := windows.GetFileAttributes(p16)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return false, false, nil
		}
		return false, false, err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return false, true, nil
	}
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, true, nil
	}
	return SDDLTrusted(sd.String()), true, nil
}

// IsReparse — путь существует и это точка повторной обработки (symlink,
// junction): по таким путям установка не ходит.
func IsReparse(p string) bool {
	p16, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p16)
	return err == nil && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
