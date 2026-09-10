package router

// New mounts the console's routes. The portal outlet's routes are never mounted, which
// the outlet-wired check reports.
func New(h any) {
	generatedRoutes(nil, h)
}
