//go:build windows

package ocr

import (
	"context"
	"errors"
	"strings"

	"albionzonefix/internal/pwsh"
)

// Languages — установленные языки OCR Windows.
func Languages(ctx context.Context) ([]string, error) {
	b, _, err := pwsh.Call(ctx, pwsh.Request{Cmd: "langs"}, Script, []string{"AJ_OCR_PATH=", "AJ_OCR_LANGS="})
	o := Parse(b)
	if o.Err != "" {
		return o.Langs, errors.New(o.Err)
	}
	if err != nil && len(o.Langs) == 0 {
		return nil, err
	}
	return o.Langs, nil
}

// Recognize распознаёт PNG на языках langs; ответ — язык → строки.
func Recognize(ctx context.Context, png string, langs []string) (map[string][]string, error) {
	b, _, err := pwsh.Call(ctx, pwsh.Request{Cmd: "ocr", Path: png, Langs: langs},
		Script, []string{"AJ_OCR_PATH=" + png, "AJ_OCR_LANGS=" + strings.Join(langs, ",")})
	o := Parse(b)
	if o.Err != "" {
		return o.Lines, errors.New(o.Err)
	}
	if err != nil && len(o.Lines) == 0 {
		return nil, err
	}
	return o.Lines, nil
}
