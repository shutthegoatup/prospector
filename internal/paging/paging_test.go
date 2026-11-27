package paging

import "testing"

func TestACursorIsOnlyWhatAListGaveOut(t *testing.T) {
	t.Parallel()
	if at, err := (Page{}).At(2); err != nil || at[0] != 0 || at[1] != 0 {
		t.Fatalf("no cursor is the start: %v %v", at, err)
	}
	if at, err := (Page{Cursor: Cursor(7, 9)}).At(2); err != nil || at[0] != 7 || at[1] != 9 {
		t.Fatalf("a cursor read back: %v %v", at, err)
	}
	for _, bad := range []string{"nonsense", Cursor(7), Cursor(1, 2, 3), Cursor(-1, 2), "W10", "!!"} {
		if _, err := (Page{Cursor: bad}).At(2); err != ErrCursor {
			t.Errorf("cursor %q was taken: %v", bad, err)
		}
	}
	if (Page{}).Size() != Usual || (Page{Limit: 3}).Size() != 3 || (Page{Limit: 100000}).Size() != Most {
		t.Fatal("the page size isn't the usual one, the one asked for, or the most allowed")
	}
}
