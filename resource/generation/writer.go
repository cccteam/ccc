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

type FileWriter struct {
	muAlign sync.Mutex
}

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

func (f *FileWriter) GoFormatBytes(fileName string, data []byte) ([]byte, error) {
	formattedData, err := format.Source(data)
	if err != nil {
		return nil, errors.Wrapf(err, "format.Source(): file: %s, file content: %q", fileName, data)
	}

	formattedData, err = imports.Process(fileName, formattedData, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "imports.Process(): file: %s", fileName)
	}

	// align package is not concurrent safe
	f.muAlign.Lock()
	defer f.muAlign.Unlock()

	align.Init(bytes.NewReader(formattedData))
	formattedData, err = align.Do()
	if err != nil {
		return nil, errors.Wrapf(err, "align.Do(): file: %s", fileName)
	}

	return formattedData, nil
}
