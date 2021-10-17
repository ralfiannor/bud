package mod

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"

	"gitlab.com/mnm/bud/go/is"
)

// Virtual module file
func Virtual(modulePath, dir string) *VirtualFile {
	return &VirtualFile{modulePath, dir}
}

type VirtualFile struct {
	modulePath string
	dir        string
}

func (v *VirtualFile) Directory() string {
	return v.dir
}

func (v *VirtualFile) ModulePath() string {
	return v.modulePath
}

func (v *VirtualFile) ResolveImport(dir string) (importPath string, err error) {
	return resolveImport(v, dir)
}

func (v *VirtualFile) ResolveDirectory(importPath string) (dir string, err error) {
	if is.StdLib(importPath) {
		return filepath.Join(stdDir, importPath), nil
	}
	dir = filepath.Join(build.Default.GOPATH, "src", importPath)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%q doesn't exist. Unable to resolve import path %q", dir, importPath)
		}
		return "", err
	}
	return dir, nil
}

func (v *VirtualFile) Plugins() ([]*Plugin, error) {
	return []*Plugin{}, nil
}