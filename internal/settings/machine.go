package settings

import "os"

// Machine holds the settings and the two locks. Close releases both.
type Machine struct {
	Settings Settings
	cfg, dat *os.File
}

// Close releases the locks. It is safe to call twice.
func (m *Machine) Close() error {
	var first error
	for _, f := range []**os.File{&m.dat, &m.cfg} {
		if *f != nil {
			if err := (*f).Close(); err != nil && first == nil {
				first = err
			}
			*f = nil
		}
	}
	return first
}
