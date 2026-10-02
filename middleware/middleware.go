// Package middleware puts the request's actor and remote address into the context
// so every audited write made while handling it is attributed.
package middleware

import (
	"net"
	"net/http"

	"github.com/shubhodeep9/witness"
)

// New returns net/http middleware. actor identifies the caller (return "" if anonymous) and
// may be nil. The remote address is r.RemoteAddr without the port; X-Forwarded-For is not
// read because clients can spoof it, so behind a proxy run a real-IP middleware first
// (one that rewrites r.RemoteAddr, such as chi's RealIP).
func New(actor func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m := witness.Meta{RemoteAddr: host(r.RemoteAddr)}
			if actor != nil {
				m.Actor = actor(r)
			}
			next.ServeHTTP(w, r.WithContext(witness.WithMeta(r.Context(), m)))
		})
	}
}

func host(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}
