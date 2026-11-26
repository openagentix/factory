//go:build !unix

package harness

import (
	"errors"
	"io"
)

var errNoUnix = errors.New("stub script: this behaviour needs a unix system")

func makeFifo(string) error { return errNoUnix }

func ignoreSignals() func() { return func() {} }

func startChild(StubChild, io.Writer) (int, error) { return 0, errNoUnix }
