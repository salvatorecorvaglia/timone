package web

import (
	"embed"
	"encoding/json"
	"io/fs"
)

//go:embed all:dist
var content embed.FS

func Assets() fs.FS { assets, _ := fs.Sub(content, "dist"); return assets }

//go:embed package.json
var packageJSON []byte

// Version is shared by the frontend, MCP server, and Kubernetes client.
var Version = func() string {
	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(packageJSON, &metadata); err != nil {
		panic(err)
	}
	if metadata.Version == "" {
		panic("missing application version in web/package.json")
	}
	return metadata.Version
}()
