package store

import "testing"

func TestListFilterNormalize(t *testing.T) {
	cases := []struct {
		name                 string
		in                   ListFilter
		wantPage, wantSize   int
	}{
		{"zero -> defaults", ListFilter{}, 1, 50},
		{"negative page", ListFilter{Page: -3}, 1, 50},
		{"oversize clamped", ListFilter{Page: 3, PageSize: 500}, 3, 200},
		{"in range untouched", ListFilter{Page: 2, PageSize: 25}, 2, 25},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := c.in
			f.Normalize()
			if f.Page != c.wantPage || f.PageSize != c.wantSize {
				t.Fatalf("Normalize() = page %d size %d, want page %d size %d",
					f.Page, f.PageSize, c.wantPage, c.wantSize)
			}
		})
	}
}
