// Package cloud is what the cloud drivers share. A driver declares the variables it reads
// on a settings struct, which an application embeds in its configuration instead of
// declaring the variables itself. The driver exports that struct's declaration in the
// shape this package defines, for the tools that read an application without loading its
// packages (impulse's checks, bedrock's stack) and have to know which variables the
// embedding declares.
package cloud

// Declaration is a settings struct's declaration: where the struct is, and the env-tagged
// fields it declares in declaration order, each as the source writes it.
type Declaration struct {
	// Path is the import path of the package declaring the struct, and Name the struct's
	// type name: what an embedding in an application's configuration resolves to.
	Path string
	Name string
	// Fields are the env-tagged fields in declaration order.
	Fields []Field
}

// Field is one env-tagged field of a settings struct, as the source writes it.
type Field struct {
	// Name is the field's name.
	Name string
	// Type is the field's type as written.
	Type string
	// Tag is the env tag's value as written: the variable and its options.
	Tag string
	// Doc is the field's doc comment without the comment markers, one line per source
	// line.
	Doc string
}
