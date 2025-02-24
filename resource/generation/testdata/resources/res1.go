package resources

type AddressType struct {
	ID          string `spanner:"Id"`
	Description string `spanner:"description"`
}
