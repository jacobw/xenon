package main

import (
	"sort"

	"xenon/internal/content"
)

// profileRow summarizes a content profile for the Admin view.
type profileRow struct {
	ID     string
	Subs   int
	Series int
}

func profileRows(store *content.Store) []profileRow {
	rs := make([]profileRow, 0, len(store.Profiles))
	for id, p := range store.Profiles {
		series := 0
		for _, s := range p.Subscriptions {
			series += s.EstSeries
		}
		rs = append(rs, profileRow{ID: id, Subs: len(p.Subscriptions), Series: series})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
	return rs
}
