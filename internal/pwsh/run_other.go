//go:build !windows

package pwsh

import "context"

const supported = false

// RunScript — на маке PowerShell нет.
func RunScript(ctx context.Context, script string, env []string) ([]byte, error) {
	return nil, ErrUnsupported
}

func startProc() (Proc, error) { return nil, ErrUnsupported }
