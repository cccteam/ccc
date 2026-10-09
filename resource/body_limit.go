package resource

import "net/http"

// BodyLimit returns middleware that bounds every request body it serves to limit bytes.
// The generated router applies it once per route, on the group that holds the resource
// routes and the other JSON routes; the upload, live and RPC routes sit beside that
// group and bound their own bodies, since a nested limit can only tighten the one
// outside it. A body that runs past the limit fails the handler's read with
// *http.MaxBytesError, which the decoders answer with 413 naming the limit, and the
// server closes the connection behind the response rather than read the rest.
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
