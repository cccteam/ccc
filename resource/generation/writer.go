package generation

import (
	"bytes"
	"go/format"
	"sync"

	"github.com/go-playground/errors/v5"
	"github.com/momaek/formattag/align"
	"golang.org/x/tools/imports"
)

// alignMu serializes the struct-tag alignment step. The align package keeps its
// manager in a package-level variable (align.Init writes it, align.Do reads it), so the
// lock guarding it is package-level too: a lock per FileWriter guards nothing once two
// writers exist, as they do in tests that build one client per parallel subtest.
var alignMu sync.Mutex

// FileWriter provides convenience methods to goformat the bytes of a Go source file.
type FileWriter struct{}

// GoFormatBytes runs Go Format on bytes for a go source file, resolving missing or unused
// imports via goimports. If the Go source data is not syntactically correct, GoFormatBytes
// will return an error. Safe to use concurrently.
func (f *FileWriter) GoFormatBytes(fileName string, data []byte) ([]byte, error) {
	formattedData, err := format.Source(data)
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): file: %s, file content: %q", fileName, data)
	}

	formattedData, err = imports.Process(fileName, formattedData, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "imports.Process(): file: %s", fileName)
	}

	return f.alignBytes(fileName, formattedData)
}

// formatBytes runs Go Format and struct-tag alignment without import resolution: the
// file's import block must already be exactly correct. Safe to use concurrently.
func (f *FileWriter) formatBytes(fileName string, data []byte) ([]byte, error) {
	formattedData, err := format.Source(data)
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): file: %s, file content: %q", fileName, data)
	}

	return f.alignBytes(fileName, formattedData)
}

// alignBytes aligns the struct tags of a formatted Go file through the process-global
// align manager, under the package-level lock that guards it.
func (f *FileWriter) alignBytes(fileName string, data []byte) ([]byte, error) {
	alignMu.Lock()
	defer alignMu.Unlock()

	align.Init(bytes.NewReader(data))
	alignedData, err := align.Do()
	if err != nil {
		return nil, errors.Wrapf(err, "align.Do(): file: %s", fileName)
	}

	return alignedData, nil
}
