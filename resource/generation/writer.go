package generation

import (
	"bytes"
	"go/format"
	"os"
	"sync"

	"github.com/go-playground/errors/v5"
	"github.com/momaek/formattag/align"
	"golang.org/x/tools/imports"
)

// FileWriter provides convenience methods to safely goformat & write bytes to file.
type FileWriter struct {
	muAlign sync.Mutex
}

// WriteBytesToFile truncates a file and writes given bytes to it.
func (f *FileWriter) WriteBytesToFile(file *os.File, data []byte) error {
	if err := file.Truncate(0); err != nil {
		return errors.Wrapf(err, "file.Truncate(): file: %s", file.Name())
	}
	if _, err := file.Seek(0, 0); err != nil {
		return errors.Wrapf(err, "file.Seek(): file: %s", file.Name())
	}
	if _, err := file.Write(data); err != nil {
		return errors.Wrapf(err, "file.Write(): file: %s", file.Name())
	}

	return nil
}

// GoFormatBytes runs Go Format on bytes for a go source file. If the Go source data is not syntactically
// correct, GoFormatBytes will return an error. Safe to use concurrently.
func (f *FileWriter) GoFormatBytes(fileName string, data []byte) ([]byte, error) {
	formattedData, err := format.Source(data)
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): file: %s, file content: %q", fileName, data)
	}

	formattedData, err = imports.Process(fileName, formattedData, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "imports.Process(): file: %s", fileName)
	}

	// we use a mutex because align package is not concurrent safe
	f.muAlign.Lock()
	defer f.muAlign.Unlock()

	align.Init(bytes.NewReader(formattedData))
	formattedData, err = align.Do()
	if err != nil {
		return nil, errors.Wrapf(err, "align.Do(): file: %s", fileName)
	}

	return formattedData, nil
}
