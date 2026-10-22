// Command release builds portable archives without publishing them.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func main() {
	version := flag.String("version", "dev", "archive version (letters, digits, dot, dash, plus)")
	output := flag.String("output", "dist", "output directory")
	targets := flag.String("targets", "linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64", "comma-separated OS/architecture pairs")
	flag.Parse()
	if err := run(*version, *output, *targets); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(version, output, targets string) error {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]{0,127}$`).MatchString(version) {
		return fmt.Errorf("invalid version")
	}
	pairs := strings.Split(targets, ",")
	for _, pair := range pairs {
		if !regexp.MustCompile(`^(linux|darwin|windows)/(amd64|arm64)$`).MatchString(pair) {
			return fmt.Errorf("unsupported target %q", pair)
		}
	}
	if err := validateDocumentation("."); err != nil {
		return err
	}
	if _, err := os.Stat("web/dist/index.html"); err != nil {
		return fmt.Errorf("build the frontend first: %w", err)
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "timone-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	checksums := []string{}
	for _, pair := range pairs {
		target := strings.Split(pair, "/")
		name := "timone"
		if target[0] == "windows" {
			name += ".exe"
		}
		binary := filepath.Join(tmp, name)
		command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", binary, "./cmd/timone")
		env := []string{}
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "GOOS=") && !strings.HasPrefix(v, "GOARCH=") && !strings.HasPrefix(v, "CGO_ENABLED=") {
				env = append(env, v)
			}
		}
		command.Env = append(env, "GOOS="+target[0], "GOARCH="+target[1], "CGO_ENABLED=0")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("build %s: %w", pair, err)
		}
		files := map[string]string{name: binary, "README.md": "README.md", "LICENSE": "LICENSE", "CHANGELOG.md": "CHANGELOG.md"}
		entries := []string{name, "README.md", "LICENSE", "CHANGELOG.md"}
		var buf bytes.Buffer
		ext := ".tar.gz"
		if target[0] == "windows" {
			ext = ".zip"
			writer := zip.NewWriter(&buf)
			for _, file := range entries {
				data, err := os.ReadFile(files[file])
				if err != nil {
					return err
				}
				entry, err := writer.Create(file)
				if err != nil {
					return err
				}
				if _, err = entry.Write(data); err != nil {
					return err
				}
			}
			if err := writer.Close(); err != nil {
				return err
			}
		} else {
			gz := gzip.NewWriter(&buf)
			writer := tar.NewWriter(gz)
			for _, file := range entries {
				data, err := os.ReadFile(files[file])
				if err != nil {
					return err
				}
				mode := int64(0644)
				if file == name {
					mode = 0755
				}
				if err = writer.WriteHeader(&tar.Header{Name: file, Mode: mode, Size: int64(len(data)), ModTime: time.Unix(0, 0)}); err != nil {
					return err
				}
				if _, err = writer.Write(data); err != nil {
					return err
				}
			}
			if err := writer.Close(); err != nil {
				return err
			}
			if err := gz.Close(); err != nil {
				return err
			}
		}
		archive := "timone_" + version + "_" + target[0] + "_" + target[1] + ext
		if err := os.WriteFile(filepath.Join(output, archive), buf.Bytes(), 0644); err != nil {
			return err
		}
		checksums = append(checksums, fmt.Sprintf("%x  %s\n", sha256.Sum256(buf.Bytes()), archive))
		fmt.Println(archive)
	}
	file, err := os.Create(filepath.Join(output, "checksums.txt"))
	if err != nil {
		return err
	}
	_, err = io.WriteString(file, strings.Join(checksums, ""))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
