//go:build !(darwin || linux)

package settings

import "errors"

// Open is not supported here: the lock and the file checks need a Unix system.
func Open() (*Machine, error) {
	return nil, errors.New("settings: this operating system is not supported; use macOS or Linux")
}
