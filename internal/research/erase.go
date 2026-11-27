package research

import (
	"strings"
	"time"

	"github.com/shutthegoatup/prospector/internal/store"
)

// KeptValue is what an event may still say once its person is gone: a stage, or the
// day of a mark. Everything else held their details or free text about them.
func KeptValue(e store.Event) *string {
	v := store.Deref(e.Value)
	switch {
	case e.Action == "set:stage" && store.ValidStage(v):
		return &v
	case strings.HasPrefix(e.Action, "mark:") && len(v) >= 10:
		if _, err := time.Parse("2006-01-02", v[:10]); err == nil {
			return store.Str(v[:10])
		}
	}
	return nil
}
