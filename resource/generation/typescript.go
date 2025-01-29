package generation

import (
	"fmt"

	"github.com/go-playground/errors/v5"
)

func (c *GenerationClient) RunTypescriptGeneration() error {
	structs, err := c.structsFromSource()
	if err != nil {
		return errors.Wrap(err, "c.structsFromSource()")
	}

	if err := removeGeneratedFiles(c.typescriptDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	fmt.Printf("structs: %v\n", structs)

	return nil
}
