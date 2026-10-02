package witness

import (
	"reflect"
	"sync"
)

// Registry maps model types to their audit Options. Unregistered models are not audited.
// The zero value is ready to use.
type Registry struct {
	mu     sync.RWMutex
	models map[reflect.Type]Options
}

// Register opts a model in. model may be a value or pointer, e.g. User{} or &User{}.
// Registering the same type again replaces its Options.
func (r *Registry) Register(model any, o Options) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.models == nil {
		r.models = map[reflect.Type]Options{}
	}
	r.models[typeOf(model)] = o
}

// Lookup returns the Options for model and whether it is registered.
func (r *Registry) Lookup(model any) (Options, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.models[typeOf(model)]
	return o, ok
}

// LookupName finds a model by its Go type name (e.g. "User"), for adapters that only see
// names, like Ent mutations. If several registered types share a name, one is returned.
func (r *Registry) LookupName(name string) (Options, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for t, o := range r.models {
		if t.Name() == name {
			return o, true
		}
	}
	return Options{}, false
}

func typeOf(model any) reflect.Type {
	t := reflect.TypeOf(model)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
