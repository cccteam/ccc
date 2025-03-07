package nestedtypes

type A struct {
	d *D
}

type B struct {
	a A
}

type C string

type D struct {
	b  B
	c  C
	c2 C
	c3 []C
	c4 []*C
}
