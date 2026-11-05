// master.go: Visio masters (stencil shapes) and inheritance.
//
// Most shapes on a page are instances of a master and store only the
// cells they override. This file loads masters on demand
// (cached per package) and copies everything an instance leaves unset
// from its master shape: cells, whole sections, rows and cells inside
// sections text and embedded images.
// Rows are matched by index or name, rows the instance deleted stay
// deleted and row order is restored afterwards so geometry paths
// are drawn in order.

package converter

import (
	"encoding/xml"
	"path"
	"sort"
	"strconv"
)

// masterDef is a Visio master (stencil shape) that page shapes
// inherit from.
type masterDef struct {
	file   string
	shapes []XShape
	byID   map[string]*XShape
}

func indexShapes(shapes []XShape, m map[string]*XShape) {
	for i := range shapes {
		m[shapes[i].ID] = &shapes[i]
		indexShapes(shapes[i].Children, m)
	}
}

// master loads (and caches) the master with the given ID, or nil when
// it is unavailable.
func (p *Package) master(id string) *masterDef {
	if m, ok := p.masters[id]; ok {
		return m
	}
	if p.masters == nil {
		p.masters = map[string]*masterDef{}
		p.masterFiles = map[string]string{}
		if b, err := p.read("visio/masters/masters.xml"); err == nil {
			var ms struct {
				M []struct {
					ID  string `xml:"ID,attr"`
					Rel struct {
						ID string `xml:"id,attr"`
					} `xml:"Rel"`
				} `xml:"Master"`
			}
			if xml.Unmarshal(b, &ms) == nil {
				rels := readRels(p, "visio/masters/masters.xml")
				for _, m := range ms.M {
					p.masterFiles[m.ID] = rels[m.Rel.ID]
				}
			}
		}
	}
	var def *masterDef
	if file := p.masterFiles[id]; file != "" {
		if b, err := p.read(file); err == nil {
			var c XContents
			if xml.Unmarshal(b, &c) == nil {
				def = &masterDef{file: path.Clean(file), shapes: c.Shapes, byID: map[string]*XShape{}}
				indexShapes(def.shapes, def.byID)
			}
		}
	}
	p.masters[id] = def
	return def
}

// inherit fills everything the instance leaves unset from its
// master shape.
func inherit(s, m *XShape, md *masterDef) {
	if s.LineStyle == "" {
		s.LineStyle = m.LineStyle
	}
	if s.FillStyle == "" {
		s.FillStyle = m.FillStyle
	}
	if s.TextStyle == "" {
		s.TextStyle = m.TextStyle
	}
	have := map[string]bool{}
	for _, c := range s.Cells {
		have[c.N] = true
	}
	for _, c := range m.Cells {
		if !have[c.N] {
			s.Cells = append(s.Cells, c)
		}
	}
	haveSec := map[string]bool{}
	for _, sec := range s.Sections {
		haveSec[sec.N+"/"+sec.IX] = true
	}
	for _, sec := range m.Sections {
		if !haveSec[sec.N+"/"+sec.IX] {
			s.Sections = append(s.Sections, sec)
			continue
		}
		for i := range s.Sections {
			if s.Sections[i].N == sec.N && s.Sections[i].IX == sec.IX {
				mergeRows(&s.Sections[i], &sec)
			}
		}
	}
	if s.Text == nil {
		s.Text = m.Text
	}
	if s.Foreign == nil && m.Foreign != nil {
		f := *m.Foreign
		s.Foreign = &f
		s.relPart = md.file
	}
	s.cm = nil
}

// mergeRows fills rows and cells that an instance section leaves out
// from the master section.
func mergeRows(dst, src *XSection) {
	// rows are identified by IX (or name); the row type may differ
	// between instance and master
	key := func(r XRow) string {
		if r.N == "" && r.IX != "" {
			return "ix/" + r.IX
		}
		return r.T + "/" + r.N + "/" + r.IX
	}
	idx := map[string]int{}
	for i, r := range dst.Rows {
		idx[key(r)] = i
	}
	haveCell := map[string]bool{}
	for _, c := range dst.Cells {
		haveCell[c.N] = true
	}
	for _, c := range src.Cells {
		if !haveCell[c.N] {
			dst.Cells = append(dst.Cells, c)
		}
	}
	for _, r := range src.Rows {
		i, ok := idx[key(r)]
		if !ok {
			dst.Rows = append(dst.Rows, r)
			continue
		}
		if dst.Rows[i].Del == "1" {
			continue
		}
		have := map[string]bool{}
		for _, c := range dst.Rows[i].Cells {
			have[c.N] = true
		}
		for _, c := range r.Cells {
			if !have[c.N] {
				dst.Rows[i].Cells = append(dst.Rows[i].Cells, c)
			}
		}
	}
	rows := dst.Rows[:0]
	for _, r := range dst.Rows {
		if r.Del != "1" {
			rows = append(rows, r)
		}
	}
	dst.Rows = rows
	sortRowsByIX(dst.Rows)
}

// sortRowsByIX restores row order after appending master rows; named
// rows keep their place.
func sortRowsByIX(rows []XRow) {
	for _, r := range rows {
		if _, err := strconv.Atoi(r.IX); err != nil {
			return
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, _ := strconv.Atoi(rows[i].IX)
		b, _ := strconv.Atoi(rows[j].IX)
		return a < b
	})
}
