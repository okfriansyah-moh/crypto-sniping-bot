package web

import "net/http"

// SecurityHeaders attaches defensive HTTP response headers for the sniper JSON API.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setAPIResponseHeaders(w)
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		next.ServeHTTP(w, r)
	})
}

// APIResponseHeaders attaches cache and hardening headers for the dashboard JSON API.
// CSP is omitted — dashboard serves JSON only (operator-dashboard plan §7.6 / Task 9).
func APIResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setAPIResponseHeaders(w)
		next.ServeHTTP(w, r)
	})
}

func setAPIResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
