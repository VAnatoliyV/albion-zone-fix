//go:build !windows

package ocr

import "context"

// Languages — на маке OCR Windows нет.
func Languages(ctx context.Context) ([]string, error) { return nil, ErrUnsupported }

// Recognize — на маке OCR Windows нет.
func Recognize(ctx context.Context, png string, langs []string) (map[string][]string, error) {
	return nil, ErrUnsupported
}
