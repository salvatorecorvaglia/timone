package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseRejectsMissingDocumentation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Timone"), 0600); err != nil {
		t.Fatal(err)
	}
	if validateDocumentation(root) == nil {
		t.Fatal("release accepted missing license")
	}
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), []byte("License"), 0600); err != nil {
		t.Fatal(err)
	}
	if validateDocumentation(root) == nil {
		t.Fatal("release accepted missing changelog")
	}
	if err := os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte("Changelog"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateDocumentation(root); err != nil {
		t.Fatal(err)
	}
}
func TestProjectReleaseDocumentationIsComplete(t *testing.T) {
	if err := validateDocumentation("../.."); err != nil {
		t.Fatal(err)
	}
}
