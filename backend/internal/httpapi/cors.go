package httpapi

import "net/http"

// CORS answers browser cross-origin requests against a fixed allowlist. It
// echoes the request's Origin header — never "*" — only when that origin
// exactly matches an allowlist entry, and always sets Vary: Origin so a
// shared cache never serves one origin's response to another.
//
// A disallowed origin gets no Access-Control-Allow-Origin header but is
// NOT blocked server-side: the browser enforces the block on its own
// response, and a server-side 403 here would break every non-browser
// client, including cmd/callit-cli.
func CORS(origins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		allowed[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")

			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			next.ServeHTTP(w, r)
		})
	}
}
