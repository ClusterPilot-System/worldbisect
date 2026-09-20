//go:build !linux

package hub

import (
	"errors"
	"os"
)

func acquireLock(string) (*os.File, error) {
	return nil, errors.New("the experimental team hub currently supports Linux only")
}
