package web

import (
	"embed"
	"io/fs"
)

//go:embed public
var publicFiles embed.FS

func publicFS() fs.FS { sub, _ := fs.Sub(publicFiles, "public"); return sub }
