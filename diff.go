package witness

import "reflect"

// Options mirror django-auditlog's include_fields / exclude_fields / mask_fields.
// An empty Include means all fields.
type Options struct {
	Include []string
	Exclude []string
	Mask    []string
}

const maskValue = "****" // ponytail: full mask; django masks only half the string

// Diff compares two field snapshots. Pass nil for old on create and nil for new on delete.
func Diff(old, new map[string]any, o Options) map[string]Change {
	out := map[string]Change{}
	for k := range union(old, new) {
		if !o.tracks(k) {
			continue
		}
		ov, nv := old[k], new[k]
		if reflect.DeepEqual(ov, nv) {
			continue
		}
		if contains(o.Mask, k) {
			ov, nv = maskIfSet(ov), maskIfSet(nv)
		}
		out[k] = Change{Old: ov, New: nv}
	}
	return out
}

func (o Options) tracks(field string) bool {
	if contains(o.Exclude, field) {
		return false
	}
	return len(o.Include) == 0 || contains(o.Include, field)
}

func maskIfSet(v any) any {
	if v == nil {
		return nil
	}
	return maskValue
}

func union(a, b map[string]any) map[string]struct{} {
	s := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		s[k] = struct{}{}
	}
	for k := range b {
		s[k] = struct{}{}
	}
	return s
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
