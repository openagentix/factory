package floor

import "sort"

// Detail returns one piece in detail, or false when no piece has that id. The
// heading title is cut at 250 characters, the drawer's limit.
func Detail(in Inputs, id string) (PieceDetail, bool) {
	f := Project(in)
	var d PieceDetail
	found := false
	for _, p := range f.Pieces {
		if p.ID == id {
			d.Piece, found = p, true
		}
	}
	if !found {
		return PieceDetail{}, false
	}
	d.Route, d.Rulings, d.Unconfirmed = []RouteStep{}, []Untrusted{}, []Untrusted{}
	c := newCtx(in)
	title := ""
	switch d.Piece.Kind {
	case KindTicket:
		for _, tk := range c.tickets {
			if d.Piece.Ticket != nil && tk.Number == *d.Piece.Ticket {
				title = tk.Title
				at := map[StationNo]*string{}
				for _, r := range tk.Route {
					if r.Station.Valid() {
						if cur, ok := at[r.Station]; !ok || cur == nil {
							at[r.Station] = c.fmtPast(r.At)
						}
					}
				}
				for no := StationNo(1); no <= 7; no++ {
					if v, ok := at[no]; ok {
						d.Route = append(d.Route, RouteStep{Station: no, At: v})
					}
				}
				d.Rulings = cutAll(tk.Rulings)
				d.Unconfirmed = cutAll(tk.Unconfirmed)
			}
		}
	default:
		if g, ok := c.goals[d.Piece.Goal]; ok {
			title = g.Title
		}
	}
	d.Piece.Title = NewUntrusted(title, 250)
	return d, true
}

const detailItems = 20

func cutAll(ss []string) []Untrusted {
	s := append([]string(nil), ss...)
	sort.Strings(s)
	out := []Untrusted{}
	for _, x := range s {
		if len(out) == detailItems {
			break
		}
		out = append(out, NewUntrusted(x, 250))
	}
	return out
}
