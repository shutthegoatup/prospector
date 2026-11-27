package request

import "encoding/json"

// Recast carries what the store answers into the spec's type of the same JSON, so the
// API says exactly what these routes always have; a field the spec lacks is caught by
// the test that compares the two.
func Recast[T any](v any) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}
