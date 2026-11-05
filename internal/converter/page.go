// page.go: the public API and page-level conversion.
//
// Open loads a file. ForegroundPages and FindPage select pages, and
// Convert turns one page into a complete .drawio document.
// Convert first converts the page background pages (the BackPage chain)
// as locked layers, then the page itself.
// Visible Visio layers are merged into one draw.io layer to keep
// Visio's z-order, while hidden layers, and every layer with
// Options.Split, stay separate and toggleable.
// This file also writes the mxGraphModel XML and per-shape CSV report.

package converter

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Host is written to the host attribute of every generated mxfile.
const Host = "github.com/skhell/visio-to-drawio"

// Options controls a page conversion.
type Options struct {
	NoFit      bool     // do not shrink text that would overflow its box
	Split      bool     // keep each visible Visio layer as its own draw.io layer
	NoBG       bool     // skip BackPage background layers
	BGEditable bool     // leave background layers unlocked
	HideLayers []string // Visio layer names to keep as separate, hidden draw.io layers
}

// Result is the outcome of converting one page.
type Result struct {
	XML      string
	Report   []ReportRow
	Warnings map[string]int
	Layers   []LayerInfo
}

// LayerInfo summarises one generated draw.io layer.
type LayerInfo struct {
	Name    string
	Visible bool
	Cells   int
}

// Open loads a .vsdx file.
func Open(path string) (*Package, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return openPackageBytes(b)
}

// IsBackground reports whether the page is a Visio background page.
func (pg *Page) IsBackground() bool { return pg.Ref.Background == "1" }

// Name returns the page title.
func (pg *Page) Name() string { return pg.Ref.Name }

// ForegroundPages returns the pages that are not backgrounds, in document order.
func (p *Package) ForegroundPages() []*Page {
	var out []*Page
	for _, pg := range p.Pages {
		if !pg.IsBackground() {
			out = append(out, pg)
		}
	}
	return out
}

// FindPage looks a page up by name, unique name or ID.
func (p *Package) FindPage(key string) *Page {
	for _, pg := range p.Pages {
		if pg.Ref.Name == key || pg.Ref.NameU == key || pg.Ref.ID == key {
			return pg
		}
	}
	return nil
}

func openPackageBytes(b []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	return openPackage(zr)
}

// Convert renders one page (with its backgrounds) as a .drawio document.
func Convert(pkg *Package, pg *Page, opt Options) (*Result, error) {
	var layers []*LayerOut
	warns := map[string]int{}
	if !opt.NoBG {
		for i, bp := range backChain(pkg, pg) {
			bc := newConv(bp)
			bc.NoFit, bc.Split, bc.hide = opt.NoFit, opt.Split, opt.HideLayers
			if err := bc.convertPage(fmt.Sprintf("b%d", i), "BG "+bp.Ref.Name+" / ", !opt.BGEditable); err != nil {
				return nil, err
			}
			layers = append(layers, bc.layers...)
			for k, v := range bc.Warn {
				warns["[bg] "+k] += v
			}
		}
	}
	fc := newConv(pg)
	fc.NoFit, fc.Split, fc.hide = opt.NoFit, opt.Split, opt.HideLayers
	if err := fc.convertPage("p", "", false); err != nil {
		return nil, err
	}
	layers = append(layers, fc.layers...)
	for k, v := range fc.Warn {
		warns[k] += v
	}

	var sb strings.Builder
	sb.WriteString(`<mxfile host="` + Host + `"><diagram name="` + escAttr(pg.Ref.Name) + `" id="d1">`)
	fmt.Fprintf(&sb, `<mxGraphModel dx="0" dy="0" grid="1" gridSize="10" guides="1" tooltips="1" connect="1" arrows="1" fold="1" page="1" pageScale="1" pageWidth="%s" pageHeight="%s" math="0" shadow="0"><root>`+"\n",
		num2(pg.W*pxPerInch*pg.U), num2(pg.H*pxPerInch*pg.U))
	sb.WriteString(`<mxCell id="0"/>` + "\n")
	res := &Result{Report: fc.Report, Warnings: warns}
	for _, l := range layers {
		writeLayer(&sb, l)
		res.Layers = append(res.Layers, LayerInfo{l.Name, l.Visible, len(l.Cells)})
	}
	sb.WriteString("</root></mxGraphModel></diagram></mxfile>\n")
	res.XML = sb.String()
	return res, nil
}

// WarningLines returns the sorted, human readable warnings.
func (r *Result) WarningLines() []string {
	keys := make([]string, 0, len(r.Warnings))
	for k := range r.Warnings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fmt.Sprintf("%s (x%d)", k, r.Warnings[k])
	}
	return out
}

// WriteReport writes the per-shape CSV report.
func WriteReport(w io.Writer, rows []ReportRow) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"page", "shape_id", "name", "layer", "hidden_in_visio", "kind", "text"})
	for _, r := range rows {
		cw.Write([]string{r.Page, r.Shape, r.Name, r.Layer, fmt.Sprint(r.Hidden), r.Kind, r.Text})
	}
	cw.Flush()
	return cw.Error()
}

func (c *Conv) convertPage(prefix string, namePrefix string, locked bool) error {
	c.prefix = prefix
	// Visio z-order is document order across layers, which draw.io
	// layers cannot express.
	// Visible layers therefore share one draw.io layer; hidden layers
	// stay separate and toggleable.
	if !c.Split {
		c.noLayer = c.addLayer(namePrefix+"Visible content", true)
		c.noLayer.Locked = locked
	}
	for i, l := range c.pg.Layers {
		forced := false
		for _, h := range c.hide {
			forced = forced || strings.EqualFold(strings.TrimSpace(h), l.Name)
		}
		if !c.Split && l.Visible && !forced {
			c.visioMap[i] = c.noLayer
			continue
		}
		lo := c.addLayer(namePrefix+l.Name, l.Visible && !forced)
		lo.Locked = locked
		c.visioMap[i] = lo
	}
	shapes, err := c.pg.shapes()
	if err != nil {
		return err
	}
	root := Mat{A: c.gpx(), D: -c.gpx(), F: c.pg.H * c.gpx()}
	c.walk(shapes, root, walkCtx{})
	if c.Split && c.noLayer != nil {
		c.noLayer.Locked = locked
		c.noLayer.Name = namePrefix + c.noLayer.Name
	}
	return nil
}

func backChain(pkg *Package, pg *Page) []*Page {
	var chain []*Page
	seen := map[string]bool{pg.Ref.ID: true}
	for id := pg.Ref.BackPage; id != "" && !seen[id]; {
		bp := pkg.pageByID(id)
		if bp == nil {
			break
		}
		seen[id] = true
		chain = append([]*Page{bp}, chain...)
		id = bp.Ref.BackPage
	}
	return chain
}

func writeLayer(sb *strings.Builder, l *LayerOut) {
	vis := ""
	if !l.Visible {
		vis = ` visible="0"`
	}
	st := ""
	if l.Locked {
		st = ` style="locked=1;"`
	}
	fmt.Fprintf(sb, `<mxCell id="%s" value="%s" parent="0"%s%s/>`+"\n", l.ID, escAttr(l.Name), vis, st)
	for _, o := range l.Cells {
		vis := ""
		if o.Hidden {
			vis = ` visible="0"`
		}
		if o.Edge {
			fmt.Fprintf(sb, `<mxCell id="%s" value="%s" style="%s" edge="1" parent="%s"%s><mxGeometry relative="1" as="geometry"><mxPoint x="%s" y="%s" as="sourcePoint"/><mxPoint x="%s" y="%s" as="targetPoint"/>`,
				o.ID, escAttr(o.Value), escAttr(o.Style), o.Parent, vis, num2(o.Src.X), num2(o.Src.Y), num2(o.Dst.X), num2(o.Dst.Y))
			if len(o.Via) > 0 {
				sb.WriteString(`<Array as="points">`)
				for _, p := range o.Via {
					fmt.Fprintf(sb, `<mxPoint x="%s" y="%s"/>`, num2(p.X), num2(p.Y))
				}
				sb.WriteString(`</Array>`)
			}
			sb.WriteString("</mxGeometry></mxCell>\n")
			continue
		}
		fmt.Fprintf(sb, `<mxCell id="%s" value="%s" style="%s" vertex="1" parent="%s"%s><mxGeometry x="%s" y="%s" width="%s" height="%s" as="geometry"/></mxCell>`+"\n",
			o.ID, escAttr(o.Value), escAttr(o.Style), o.Parent, vis, num2(o.X), num2(o.Y), num2(o.W), num2(o.H))
	}
}
