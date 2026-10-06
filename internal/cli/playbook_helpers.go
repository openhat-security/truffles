package cli

import "runtime"

func isMac() bool {
	if runtime.GOOS == "darwin" {
		return true
	}
	return false
}
