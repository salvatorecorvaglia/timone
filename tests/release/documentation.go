package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Fail before building archives if a required release document is missing.
func validateDocumentation(root string) error {
	for _, name := range []string{"README.md", "LICENSE", "CHANGELOG.md"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || info.IsDir() {
			return fmt.Errorf("required release documentation missing: %s", name)
		}
	}
	return nil
}
