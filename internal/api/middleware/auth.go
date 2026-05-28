package middleware

import (
	"log"
	"net/http"
)

func Auth(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != apiKey {
			log.Printf("unauthorized request: method=%s path=%s ip=%s", r.Method, r.URL.Path, r.RemoteAddr)
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
