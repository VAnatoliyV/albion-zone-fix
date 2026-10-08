//go:build !windows

package screen

// CaptureAroundCursor — на маке снимков нет (карточка зоны там своя).
func CaptureAroundCursor(path string) (Info, error) { return Info{}, ErrUnsupported }
