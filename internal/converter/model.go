// model.go: the Visio file model.
//
// Defines the XML structures the converter reads from a .vsdx/.vstx
// package (shapes, cells, sections, rows, stylesheets and pages) and
// loads the package: document fonts, colours and stylesheets, the page
// list with each page's size, drawing scale and layers, and the
// shapes of a page.
//
// It also resolves cell values. A cell missing on a shape is looked up
// through the shape's line, fill or text stylesheet chain
// (val, num, rowProp) and colours given as palette indexes are turned
// into #RRGGBB (color).

package converter

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

const nsR = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

type XCell struct {
	N string `xml:"N,attr"`
	V string `xml:"V,attr"`
	F string `xml:"F,attr"`
}

type XRow struct {
	T     string  `xml:"T,attr"`
	N     string  `xml:"N,attr"`
	IX    string  `xml:"IX,attr"`
	Del   string  `xml:"Del,attr"`
	Cells []XCell `xml:"Cell"`
}

type XSection struct {
	N     string  `xml:"N,attr"`
	IX    string  `xml:"IX,attr"`
	Cells []XCell `xml:"Cell"`
	Rows  []XRow  `xml:"Row"`
}

type XText struct {
	Inner string `xml:",innerxml"`
}

type XForeign struct {
	Type string `xml:"ForeignType,attr"`
	Rel  struct {
		ID string `xml:"id,attr"`
	} `xml:"Rel"`
}

type XShape struct {
	ID        string     `xml:"ID,attr"`
	Type      string     `xml:"Type,attr"`
	Master    string     `xml:"Master,attr"`
	NameU     string     `xml:"NameU,attr"`
	LineStyle string     `xml:"LineStyle,attr"`
	FillStyle string     `xml:"FillStyle,attr"`
	TextStyle string     `xml:"TextStyle,attr"`
	Cells     []XCell    `xml:"Cell"`
	Sections  []XSection `xml:"Section"`
	Text      *XText     `xml:"Text"`
	Foreign   *XForeign  `xml:"ForeignData"`
	Children  []XShape   `xml:"Shapes>Shape"`

	MasterShape string `xml:"MasterShape,attr"`
	relPart     string
	cm          map[string]string
}

func (s *XShape) cell(n string) (string, bool) {
	if s.cm == nil {
		s.cm = map[string]string{}
		for _, c := range s.Cells {
			s.cm[c.N] = c.V
		}
	}
	v, ok := s.cm[n]
	return v, ok
}

func (s *XShape) section(n string) *XSection {
	for i := range s.Sections {
		if s.Sections[i].N == n {
			return &s.Sections[i]
		}
	}
	return nil
}

type XStyle struct {
	ID        string     `xml:"ID,attr"`
	Name      string     `xml:"Name,attr"`
	LineStyle string     `xml:"LineStyle,attr"`
	FillStyle string     `xml:"FillStyle,attr"`
	TextStyle string     `xml:"TextStyle,attr"`
	Cells     []XCell    `xml:"Cell"`
	Sections  []XSection `xml:"Section"`
	cm        map[string]string
}

type XDocument struct {
	Faces []struct {
		NameU string `xml:"NameU,attr"`
	} `xml:"FaceNames>FaceName"`
	Colors []struct {
		IX  string `xml:"IX,attr"`
		RGB string `xml:"RGB,attr"`
	} `xml:"Colors>ColorEntry"`
	Styles []XStyle `xml:"StyleSheets>StyleSheet"`
}

type XPageSheet struct {
	Cells    []XCell    `xml:"Cell"`
	Sections []XSection `xml:"Section"`
}

type XPageRef struct {
	ID         string     `xml:"ID,attr"`
	Name       string     `xml:"Name,attr"`
	NameU      string     `xml:"NameU,attr"`
	BackPage   string     `xml:"BackPage,attr"`
	Background string     `xml:"Background,attr"`
	Sheet      XPageSheet `xml:"PageSheet"`
	Rel        struct {
		ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	} `xml:"Rel"`
}

type XPages struct {
	Pages []XPageRef `xml:"Page"`
}

type XContents struct {
	Shapes []XShape `xml:"Shapes>Shape"`
}

type Layer struct {
	Name    string
	Visible bool
}

type Page struct {
	pkg    *Package
	Ref    XPageRef
	File   string
	W, H   float64
	U      float64 // paper inches per drawing inch (PageScale/DrawingScale); text and line widths are not scaled
	Layers []Layer
}

type Package struct {
	zr     *zip.Reader
	files  map[string]*zip.File
	styles map[string]*XStyle
	faces  []string
	colors map[int]string
	Pages  []*Page

	masters     map[string]*masterDef
	masterFiles map[string]string
}

func (p *Package) read(name string) ([]byte, error) {
	f, ok := p.files[name]
	if !ok {
		return nil, fmt.Errorf("missing part %s", name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func readRels(p *Package, part string) map[string]string {
	dir, base := path.Split(part)
	b, err := p.read(dir + "_rels/" + base + ".rels")
	m := map[string]string{}
	if err != nil {
		return m
	}
	var rels struct {
		R []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	xml.Unmarshal(b, &rels)
	for _, r := range rels.R {
		t := r.Target
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Clean(path.Join(dir, t))
		}
		m[r.ID] = t
	}
	return m
}

func openPackage(zr *zip.Reader) (*Package, error) {
	p := &Package{zr: zr, files: map[string]*zip.File{}, styles: map[string]*XStyle{}, colors: map[int]string{}}
	for _, f := range zr.File {
		p.files[f.Name] = f
	}
	db, err := p.read("visio/document.xml")
	if err != nil {
		return nil, err
	}
	var d XDocument
	if err := xml.Unmarshal(db, &d); err != nil {
		return nil, err
	}
	for _, f := range d.Faces {
		p.faces = append(p.faces, f.NameU)
	}
	for _, c := range d.Colors {
		i, _ := strconv.Atoi(c.IX)
		p.colors[i] = c.RGB
	}
	for i := range d.Styles {
		s := &d.Styles[i]
		s.cm = map[string]string{}
		for _, c := range s.Cells {
			s.cm[c.N] = c.V
		}
		p.styles[s.ID] = s
	}
	pb, err := p.read("visio/pages/pages.xml")
	if err != nil {
		return nil, err
	}
	var ps XPages
	if err := xml.Unmarshal(pb, &ps); err != nil {
		return nil, err
	}
	rels := readRels(p, "visio/pages/pages.xml")
	for _, r := range ps.Pages {
		pg := &Page{pkg: p, Ref: r, File: rels[r.Rel.ID]}
		var ps, ds float64
		for _, c := range r.Sheet.Cells {
			switch c.N {
			case "PageScale":
				ps, _ = strconv.ParseFloat(c.V, 64)
			case "DrawingScale":
				ds, _ = strconv.ParseFloat(c.V, 64)
			case "PageWidth":
				pg.W, _ = strconv.ParseFloat(c.V, 64)
			case "PageHeight":
				pg.H, _ = strconv.ParseFloat(c.V, 64)
			}
		}
		pg.U = 1
		if ps > 0 && ds > 0 {
			pg.U = ps / ds
		}
		for _, s := range r.Sheet.Sections {
			if s.N != "Layer" {
				continue
			}
			for _, row := range s.Rows {
				l := Layer{Visible: true}
				for _, c := range row.Cells {
					switch c.N {
					case "Name":
						l.Name = c.V
					case "Visible":
						l.Visible = c.V != "0"
					}
				}
				pg.Layers = append(pg.Layers, l)
			}
		}
		p.Pages = append(p.Pages, pg)
	}
	return p, nil
}

func (pg *Page) shapes() ([]XShape, error) {
	b, err := pg.pkg.read(pg.File)
	if err != nil {
		return nil, err
	}
	var c XContents
	if err := xml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return c.Shapes, nil
}

func (p *Package) pageByID(id string) *Page {
	for _, pg := range p.Pages {
		if pg.Ref.ID == id {
			return pg
		}
	}
	return nil
}

var lineCells = map[string]bool{"LineColor": true, "LinePattern": true, "LineWeight": true, "Rounding": true,
	"BeginArrow": true, "EndArrow": true, "BeginArrowSize": true, "EndArrowSize": true, "LineCap": true}
var fillCells = map[string]bool{"FillForegnd": true, "FillBkgnd": true, "FillPattern": true, "ShdwPattern": true,
	"FillGradientEnabled": true, "FillForegndTrans": true}

func (p *Package) chain(s *XShape, cat string, fn func(st *XStyle) bool) {
	id := s.TextStyle
	if cat == "line" {
		id = s.LineStyle
	} else if cat == "fill" {
		id = s.FillStyle
	}
	for n := 0; id != "" && n < 20; n++ {
		st := p.styles[id]
		if st == nil || fn(st) {
			return
		}
		switch cat {
		case "line":
			id = st.LineStyle
		case "fill":
			id = st.FillStyle
		default:
			id = st.TextStyle
		}
	}
}

// val resolves a cell from the shape, then its stylesheet chain.
func (p *Package) val(s *XShape, n string) (string, bool) {
	if v, ok := s.cell(n); ok {
		return v, true
	}
	cat := "text"
	if lineCells[n] {
		cat = "line"
	} else if fillCells[n] {
		cat = "fill"
	}
	var out string
	var found bool
	p.chain(s, cat, func(st *XStyle) bool {
		if v, ok := st.cm[n]; ok {
			out, found = v, true
			return true
		}
		return false
	})
	return out, found
}

func (p *Package) num(s *XShape, n string, def float64) float64 {
	v, ok := p.val(s, n)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// rowProp reads a property from section rows (Character / Paragraph)
// with style fallback.
func (p *Package) rowProp(s *XShape, sec, ix, n string) (string, bool) {
	look := func(secs []XSection, ix string) (string, bool) {
		for _, x := range secs {
			if x.N != sec {
				continue
			}
			for _, r := range x.Rows {
				if r.IX == ix {
					for _, c := range r.Cells {
						if c.N == n {
							return c.V, true
						}
					}
				}
			}
		}
		return "", false
	}
	if v, ok := look(s.Sections, ix); ok {
		return v, true
	}
	var out string
	var found bool
	p.chain(s, "text", func(st *XStyle) bool {
		if v, ok := look(st.Sections, ix); ok {
			out, found = v, true
			return true
		}
		if ix != "0" {
			if v, ok := look(st.Sections, "0"); ok {
				out, found = v, true
				return true
			}
		}
		return false
	})
	return out, found
}

var basePalette = []string{"#000000", "#FFFFFF", "#FF0000", "#00FF00", "#0000FF", "#FFFF00", "#FF00FF", "#00FFFF",
	"#800000", "#008000", "#000080", "#808000", "#800080", "#008080", "#C0C0C0", "#E6E6E6",
	"#CCCCCC", "#B3B3B3", "#999999", "#808080", "#666666", "#4D4D4D", "#333333", "#1A1A1A"}

func (p *Package) color(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "#") && len(v) == 7 {
		return strings.ToUpper(v)
	}
	if i, err := strconv.Atoi(v); err == nil {
		if c, ok := p.colors[i]; ok {
			return strings.ToUpper(c)
		}
		if i >= 0 && i < len(basePalette) {
			return basePalette[i]
		}
	}
	return ""
}
