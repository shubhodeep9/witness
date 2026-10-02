package witness

import "context"

// Meta is the request-scoped data stamped onto every Entry.
type Meta struct {
	Actor      string
	RemoteAddr string
}

type metaKey struct{}

func WithMeta(ctx context.Context, m Meta) context.Context {
	return context.WithValue(ctx, metaKey{}, m)
}

// MetaFrom returns the zero Meta if none was set.
func MetaFrom(ctx context.Context) Meta {
	m, _ := ctx.Value(metaKey{}).(Meta)
	return m
}
