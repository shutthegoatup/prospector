// Package slicesx has what the standard slices package doesn't.
package slicesx

// Map is f of each of xs, in order. It is never nil, so an empty list is [] in JSON.
func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

// NonNil is xs, or an empty slice for nil, so an empty list is [] in JSON.
func NonNil[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}
