package githost

import (
	"fmt"
	"strings"
)

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
func contains(s, sub string) bool       { return strings.Contains(s, sub) }
