//go:build !windows

package cfg

import "os"

func replaceFileAtomically(source, destination string) error {
	return os.Rename(source, destination)
}
