package web

import (
	"embed"
	"errors"
	"mime"
	"path/filepath"
)

//go:embed index.html style.css app.js
var assets embed.FS

func Asset(path string) ([]byte, string, error) {
	content, err := assets.ReadFile(path)
	if err != nil {
		return nil, "", errors.New("asset not found")
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return content, contentType, nil
}
