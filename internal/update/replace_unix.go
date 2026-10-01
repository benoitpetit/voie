//go:build !windows

package update

import "os"

func replaceExecutable(stagedPath, executablePath string) (bool, error) {
	return false, os.Rename(stagedPath, executablePath)
}
