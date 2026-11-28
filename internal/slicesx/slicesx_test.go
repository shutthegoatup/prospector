package slicesx

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestMap(t *testing.T) {
	t.Parallel()
	if got := Map([]int{1, 2, 3}, strconv.Itoa); len(got) != 3 || got[0] != "1" || got[2] != "3" {
		t.Fatalf("Map: %v", got)
	}
	if b, _ := json.Marshal(Map([]int(nil), strconv.Itoa)); string(b) != "[]" {
		t.Fatalf("Map of nothing is %s, not []", b)
	}
}
