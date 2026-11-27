package paging

import (
	"github.com/shutthegoatup/prospector/internal/say"
)

// PageIn checks what a list was asked for as the other lists do. Zero is the limit
// left out, which the generated code can't tell from 0 asked for.
func PageIn(limit int, cursor string) (Page, error) {
	if limit < 0 {
		return Page{}, say.New("limit is a whole number, 1 or more")
	}
	return Page{Limit: limit, Cursor: cursor}, nil
}

// ListPage is one page of a list already read whole and in its order. Its cursor is
// how far down the list the page ended, so a row added or gone between two pages
// moves the rest by one.
func ListPage[T any](all []T, limit int, cursor string) (page []T, next string, err error) {
	in, err := PageIn(limit, cursor)
	if err != nil {
		return nil, "", err
	}
	at, err := in.At(1)
	if err != nil {
		return nil, "", err
	}
	from := min(int(at[0]), len(all))
	to := min(from+in.Size(), len(all))
	if to < len(all) {
		next = Cursor(int64(to))
	}
	return all[from:to], next, nil
}
