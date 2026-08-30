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
				// Safe to set unconditionally alongside a specific echoed
				// origin (never paired with "*") only because this app has
				// no cookie-based credential for a browser to auto-attach —
				// frontend/lib/api.ts always sends the JWT as an explicit
				// Authorization header. A future cookie-based auth flow
				// would need to re-review this line, not inherit it.
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// A preflight is OPTIONS carrying Access-Control-Request-Method.
			// It must be answered here, ahead of the mux: http.ServeMux
			// would otherwise reply 405 to an OPTIONS on a POST-only route
			// before any per-route middleware ever runs.
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
