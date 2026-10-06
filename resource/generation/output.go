package generation

import (
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/go-playground/errors/v5"
)

// generatedOutput is a run's record of the directories it writes generated files into
// and the files it wrote there, so the sweep at the end of the run removes exactly the
// generated files the run did not write. A phase registers its output directory where
// it used to sweep it (registerOutput), every write goes through writeGeneratedFile,
// and removeStaleOutput runs once after the last phase. No generated file is ever
// missing or partial in between: a file is replaced by an atomic rename, so a package
// compiling against the tree during the run (the drift test under go test ./...) sees
// the previous file or the new one. Safe for the concurrent phases (forEachGo); the
// resource and TypeScript generators share one record through the one client.
type generatedOutput struct {
	mu      sync.Mutex
	targets []outputTarget
	written map[string]struct{}
}

// outputTarget is a directory a run writes generated files into, with the rule naming
// the files the run owns there.
type outputTarget struct {
	dir    string
	method generatedFileDeleteMethod
}

// registerOutput records dir as a directory the run owns generated files in, by
// method. Registering a directory twice is harmless.
func (o *generatedOutput) registerOutput(dir string, method generatedFileDeleteMethod) {
	o.mu.Lock()
	defer o.mu.Unlock()

	target := outputTarget{dir: filepath.Clean(dir), method: method}
	if !slices.Contains(o.targets, target) {
		o.targets = append(o.targets, target)
	}
}

// writeGeneratedFile writes data to path atomically, creating the directory when it is
// missing, and records the path as written for the stale sweep.
func (o *generatedOutput) writeGeneratedFile(path string, data []byte) error {
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if o.written == nil {
		o.written = make(map[string]struct{})
	}
	o.written[filepath.Clean(path)] = struct{}{}

	return nil
}

// removeStaleOutput removes, from every registered directory, the generated files the
// run did not write, and forgets the record so a later run on the same client starts
// clean.
func (o *generatedOutput) removeStaleOutput() error {
	o.mu.Lock()
	targets, written := o.targets, o.written
	o.targets, o.written = nil, nil
	o.mu.Unlock()

	for _, target := range targets {
		if err := removeGeneratedFiles(target.dir, target.method, written); err != nil {
			return errors.Wrap(err, "removeGeneratedFiles()")
		}
	}

	return nil
}

// generatedTempPattern names the temporary file a generated file is written to before
// the rename: a dot file, which the Go tool ignores, carrying nothing of the generated
// name, so a drift test hashing zz_gen files never sees one.
const generatedTempPattern = ".gen-*.tmp"

// generatedFileMode is the mode of a generated file: world-readable, as os.Create
// writes them under the usual umask.
const generatedFileMode = 0o644

// writeFileAtomic writes data to path through a temporary file in the same directory
// and an atomic rename, so a reader never sees a missing or partial file. A failure
// leaves the previous file untouched and no temporary behind.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}

	tmp, err := os.CreateTemp(dir, generatedTempPattern)
	if err != nil {
		return errors.Wrap(err, "os.CreateTemp()")
	}
	tmpName := tmp.Name()
	if err := writeAndClose(tmp, data); err != nil {
		_ = os.Remove(tmpName)

		return errors.Wrapf(err, "writing %s", path)
	}
	if err := os.Chmod(tmpName, generatedFileMode); err != nil {
		_ = os.Remove(tmpName)

		return errors.Wrapf(err, "os.Chmod(): file: %s", path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)

		return errors.Wrapf(err, "os.Rename(): file: %s", path)
	}

	return nil
}

// writeAndClose writes data to f and closes it, closing it on a write failure too.
func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()

		return errors.Wrap(err, "os.File.Write()")
	}
	if err := f.Close(); err != nil {
		return errors.Wrap(err, "os.File.Close()")
	}

	return nil
}
