package modules

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

const stdlibModulePrefix = "__stdlib__/"

//go:embed stdlib
var stdlibFS embed.FS

func isStdlibImport(parts []string) bool {
	return len(parts) > 0 && parts[0] == "polyloft"
}

func resolveStdlibModule(parts []string) (string, bool) {
	relPath := path.Join(parts...)
	baseName := parts[len(parts)-1]
	for _, candidate := range []string{
		path.Join("stdlib", relPath+".pf"),
		path.Join("stdlib", relPath, "index.pf"),
		path.Join("stdlib", relPath, baseName+".pf"),
	} {
		if _, err := fs.Stat(stdlibFS, candidate); err == nil {
			return stdlibModulePrefix + candidate, true
		}
	}
	return "", false
}

func isStdlibModulePath(modulePath string) bool {
	return strings.HasPrefix(modulePath, stdlibModulePrefix)
}

func trimStdlibModulePath(modulePath string) string {
	return strings.TrimPrefix(modulePath, stdlibModulePrefix)
}
