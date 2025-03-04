package nestedtypes

type A struct {
	d *D
}

type B struct {
	a A
}

type C string

type D struct {
	b B
	c C
}
