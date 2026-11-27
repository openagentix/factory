package floor

import "os"

func readFileForTest(name string) (string, error) {
	b, err := os.ReadFile(name)
	return string(b), err
}
