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
	d.Undo = c.undoFor(d.Piece)
	return d, true
}

// Undoable lists every change the Floor offers to take back, as the piece
// details carry them. A change that is not in the list is not offered.
func Undoable(in Inputs) []UndoRef {
	f := Project(in)
	c := newCtx(in)
	out := []UndoRef{}
	for _, p := range f.Pieces {
		if ref := c.undoFor(p); ref != nil {
			out = append(out, *ref)
		}
	}
	return out
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

// undoFor says which change a person may take back for a piece. It is set only
// for a ticket piece at 06 when the holding area is read, published, not
// frozen, not pending and held by this machine. The change is the ticket's
// first applied entry: the earliest by time, then by id. A ticket whose entries
// have no id gets none. Everything unknown means no undo.
func (c *ctx) undoFor(p Piece) *UndoRef {
	h := c.holding
	if p.Kind != KindTicket || p.Ticket == nil || p.Station != 6 || !c.ok(SourceStaging) ||
		!h.Exists || !h.Published || h.Frozen || h.PendingAccept || !h.Mine || h.StagingSeq < 1 || h.Commit == "" {
		return nil
	}
	var best *EntryIn
	for _, e := range c.appliedEntries[*p.Ticket] {
		if e.EntryID == "" {
			return nil // one entry without an id: the first one cannot be told
		}
		e := e
		if best == nil || earlierEntry(e, *best) {
			best = &e
		}
	}
	if best == nil {
		return nil
	}
	return &UndoRef{Change: best.EntryID, StagingSeq: h.StagingSeq, Commit: h.Commit}
}

func earlierEntry(a, b EntryIn) bool {
	at, aok := known(a.At)
	bt, bok := known(b.At)
	switch {
	case aok && bok && !at.Equal(bt):
		return at.Before(bt)
	case aok != bok:
		return aok
	}
	return a.EntryID < b.EntryID
}
