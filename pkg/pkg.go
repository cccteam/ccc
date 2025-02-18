// package pkg
package pkg

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
)

type PkgInfo struct {
	AbsolutePath string
	PackageName  string
}

func Info() (*PkgInfo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, errors.Wrap(err, "os.Getwd()")
	}

	var f *os.File
	for {
		f, err = os.Open(filepath.Join(cwd, "go.mod"))
		if err != nil && !os.IsNotExist(err) {
			return nil, errors.Wrap(err, "os.Open()")
		} else if os.IsNotExist(err) {
			if cwd == "/" {
				return nil, errors.New("pkg.Info(): reached root and did not find go.mod")
			}

			cwd = filepath.Dir(cwd)

			continue
		}

		break
	}
	defer f.Close()

	buff := bufio.NewScanner(f)
	for buff.Scan() {
		line := buff.Text()
		if strings.HasPrefix(line, "module") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return nil, errors.New("pkg.Info(): failed to find module path in go.mod")
			}

			return &PkgInfo{
				AbsolutePath: cwd,
				PackageName:  parts[1],
			}, nil
		}
	}

	return nil, errors.New("pkg.Info(): failed to find module directive in go.mod")
}
