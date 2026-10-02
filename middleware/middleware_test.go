package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shubhodeep9/witness"
)

func serve(t *testing.T, actor func(*http.Request) string, remote string) witness.Meta {
	t.Helper()
	var got witness.Meta
	h := New(actor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = witness.MetaFrom(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	r.Header.Set("X-Forwarded-For", "6.6.6.6") // must be ignored
	h.ServeHTTP(httptest.NewRecorder(), r)
	return got
}

func TestMeta(t *testing.T) {
	user := func(r *http.Request) string { return r.Header.Get("X-User") }
	cases := []struct {
		name   string
		actor  func(*http.Request) string
		remote string
		want   witness.Meta
	}{
		{"ipv4", func(*http.Request) string { return "alice" }, "203.0.113.7:5000", witness.Meta{Actor: "alice", RemoteAddr: "203.0.113.7"}},
		{"ipv6", nil, "[2001:db8::1]:5000", witness.Meta{RemoteAddr: "2001:db8::1"}},
		{"no port", user, "203.0.113.7", witness.Meta{RemoteAddr: "203.0.113.7"}},
	}
	for _, c := range cases {
		if got := serve(t, c.actor, c.remote); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
