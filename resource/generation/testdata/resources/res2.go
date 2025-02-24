package resources

type Car struct {
	ID        string `spanner:"Id"`
	NumWheels int    `spanner:"wheels"`
}

type Truck struct {
	ID           string `spanner:"Id"`
	NumWheels    int    `spanner:"wheels"`
	IsFourByFour bool   `spanner:"isFourByFour"`
}
