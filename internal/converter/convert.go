// convert.go: shape-to-cell conversion.
//
// Conv walks a Microsoft Visio page shape tree. For each shape it:
//   - inherits from the shape master and builds its page transform
// 	   from pin, LocPin, rotation and flips (Visio flips about the pin,
//     not the centre);
//   - picks the draw.io layer and visibility from the shape
//     Visio layers;
//   - emits draw.io cells: a native rectangle or ellipse when the
//     geometry is one, a native edge for a single open line, and
//     otherwise an inline draw.io stencil holding all paths, with fills
//     painted before outlines like Visio;
//   - maps line, fill, gradient and fill-pattern cells to draw.io
//     styles, and text (runs, fields, fonts, alignment, HideText) to an
//     HTML label, shrinking it to fit its box unless NoFit is set;
//   - converts images (PNG/JPEG/GIF directly, EMF/WMF through external
//     tools when they are installed) and embedded .vsdx drawings,
//     recursively.
//
// Unsupported or approximated features are counted in Conv.Warn.

package converter

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const pxPerInch = 72.0

type Out struct {
	ID, Parent, Value, Style string
	Edge                     bool
	X, Y, W, H               float64
	Src, Dst                 Pt
	Via                      []Pt
	Hidden                   bool
}

type ReportRow struct {
	Page, Shape, Name, Layer, Text, Kind string
	Hidden                               bool
}

type LayerOut struct {
	ID      string
	Name    string
	Visible bool
	Locked  bool
	Cells   []*Out
}

type Conv struct {
	pkg      *Package
	pg       *Page
	layers   []*LayerOut
	visioMap map[int]*LayerOut
	noLayer  *LayerOut
	cur      *LayerOut // override target (background / embedded)
	next     int
	Report   []ReportRow
	Warn     map[string]int
	depth    int
	prefix   string
	k        float64 // font shrink factor for the label being built
	NoFit    bool
	u        float64 // geometry scale, see Page.U
	Split    bool
	hide     []string
	after    []func()
}

func newConv(pg *Page) *Conv {
	u := pg.U
	if u <= 0 {
		u = 1
	}
	return &Conv{pkg: pg.pkg, pg: pg, u: u, Warn: map[string]int{}, visioMap: map[int]*LayerOut{}}
}

// gpx converts a geometry length (drawing inches) to pixels.
func (c *Conv) gpx() float64 { return pxPerInch * c.u }

func (c *Conv) warn(s string) { c.Warn[s]++ }

func (c *Conv) newID() string {
	c.next++
	return fmt.Sprintf("%sc%d", c.prefix, c.next)
}

func (c *Conv) addLayer(name string, visible bool) *LayerOut {
	l := &LayerOut{ID: fmt.Sprintf("%sL%d", c.prefix, len(c.layers)+1), Name: name, Visible: visible}
	c.layers = append(c.layers, l)
	return l
}

func num2(f float64) string {
	s := strconv.FormatFloat(math.Round(f*100)/100, 'f', -1, 64)
	if s == "-0" {
		return "0"
	}
	return s
}

func escAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "\n", "&#10;", "\r", "", "\t", "&#9;")
	return r.Replace(s)
}

func encodeURIComponent(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func stencilStyle(xmlDef string) string {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	w.Write([]byte(encodeURIComponent(xmlDef)))
	w.Close()
	return "shape=stencil(" + base64.StdEncoding.EncodeToString(buf.Bytes()) + ")"
}

type walkCtx struct {
	layer    *LayerOut
	hidden   bool
	angle    float64 // accumulated visio rotation (rad, CCW)
	flipYs   int
	locked   bool
	hostName string
	master   *masterDef
}

func (c *Conv) layerFor(s *XShape, ctx walkCtx) (*LayerOut, string, bool) {
	if c.cur != nil {
		return c.cur, c.cur.Name, ctx.hidden
	}
	if ctx.layer != nil {
		// a child on layers that are all hidden is hidden even when its group is visible
		hidden := ctx.hidden
		if lm, ok := s.cell("LayerMember"); ok && lm != "" && !hidden {
			known, vis := false, false
			for _, p := range strings.Split(lm, ";") {
				if i, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && c.visioMap[i] != nil {
					known = true
					vis = vis || c.visioMap[i].Visible
				}
			}
			hidden = known && !vis
		}
		return ctx.layer, ctx.layer.Name, hidden
	}
	lm, ok := s.cell("LayerMember")
	if !ok || lm == "" {
		if c.noLayer == nil {
			c.noLayer = c.addLayer("(no layer)", true)
		}
		return c.noLayer, "(no layer)", false
	}
	var first *LayerOut
	visible := false
	var names []string
	for _, p := range strings.Split(lm, ";") {
		i, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			continue
		}
		l := c.visioMap[i]
		if l == nil {
			continue
		}
		names = append(names, l.Name)
		if first == nil {
			first = l
		}
		if l.Visible {
			visible = true
			first = l
		}
	}
	if first == nil {
		if c.noLayer == nil {
			c.noLayer = c.addLayer("(no layer)", true)
		}
		return c.noLayer, "(no layer)", false
	}
	return first, strings.Join(names, ";"), !visible
}

func (c *Conv) shapeName(s *XShape) string {
	if s.NameU != "" {
		return s.NameU
	}
	return ""
}

type textRun struct {
	Text   string
	CharIX string
	ParaIX string
	Field  bool
}

func (c *Conv) parseText(s *XShape) []textRun {
	if s.Text == nil {
		return nil
	}
	fields := map[string]string{}
	if f := s.section("Field"); f != nil {
		for _, r := range f.Rows {
			for _, cell := range r.Cells {
				if cell.N == "Value" {
					fields[r.IX] = cell.V
				}
			}
		}
	}
	d := xml.NewDecoder(strings.NewReader("<t>" + s.Text.Inner + "</t>"))
	var runs []textRun
	ci, pi := "0", "0"
	inFld := false
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			get := func(n string) string {
				for _, a := range t.Attr {
					if a.Name.Local == n {
						return a.Value
					}
				}
				return ""
			}
			switch t.Name.Local {
			case "cp":
				ci = get("IX")
			case "pp":
				pi = get("IX")
			case "fld":
				inFld = true
				runs = append(runs, textRun{Text: fields[get("IX")], CharIX: ci, ParaIX: pi, Field: true})
			}
		case xml.EndElement:
			if t.Name.Local == "fld" {
				inFld = false
			}
		case xml.CharData:
			if !inFld {
				runs = append(runs, textRun{Text: string(t), CharIX: ci, ParaIX: pi})
			}
		}
	}
	// drop trailing newline(s) added by Visio
	for len(runs) > 0 {
		l := &runs[len(runs)-1]
		l.Text = strings.TrimRight(l.Text, "\n")
		if l.Text == "" && !l.Field {
			runs = runs[:len(runs)-1]
		} else {
			break
		}
	}
	return runs
}

type charProps struct {
	size  float64
	color string
	style int
	font  string
}

func (c *Conv) charProps(s *XShape, ix string) charProps {
	p := c.pkg
	cp := charProps{size: 8, color: "#000000", font: "Arial"}
	if v, ok := p.rowProp(s, "Character", ix, "Size"); ok {
		cp.size = fnum(v) * pxPerInch
	}
	if c.k > 0 {
		cp.size *= c.k
	}
	cp.size = math.Round(cp.size*10) / 10
	if v, ok := p.rowProp(s, "Character", ix, "Color"); ok {
		if col := p.color(v); col != "" {
			cp.color = col
		}
	}
	if v, ok := p.rowProp(s, "Character", ix, "Style"); ok {
		cp.style = int(fnum(v))
	}
	if v, ok := p.rowProp(s, "Character", ix, "Font"); ok {
		if i, err := strconv.Atoi(v); err == nil {
			// numeric font IDs here point at Wingdings, which would turn plain text into symbols
			if i >= 0 && i < len(p.faces) && !strings.Contains(p.faces[i], "Wingdings") && !strings.Contains(p.faces[i], "Symbol") {
				cp.font = p.faces[i]
			}
		} else if v != "" && v != "Themed" {
			cp.font = v
		}
	}
	return cp
}

type labelInfo struct {
	html  string
	style string
}

func (c *Conv) buildLabel(s *XShape, w, h, rotation float64) (labelInfo, bool) {
	if v, ok := c.pkg.val(s, "HideText"); ok && fnum(v) == 1 {
		return labelInfo{}, false
	}
	runs := c.parseText(s)
	if len(runs) == 0 {
		return labelInfo{}, false
	}
	any := false
	for _, r := range runs {
		if strings.TrimSpace(r.Text) != "" {
			any = true
		}
	}
	if !any {
		return labelInfo{}, false
	}
	c.k = 1
	if !c.NoFit {
		c.k = c.fitScale(s, runs, w, h, rotation)
	}
	defer func() { c.k = 1 }()
	base := c.charProps(s, runs[0].CharIX)
	var b strings.Builder
	for _, r := range runs {
		cp := c.charProps(s, r.CharIX)
		txt := strings.ReplaceAll(preserveSpaces(html.EscapeString(r.Text)), "\n", "<br>")
		var st []string
		if cp.size != base.size {
			st = append(st, fmt.Sprintf("font-size: %spx", num2(cp.size)))
		}
		if cp.color != base.color {
			st = append(st, "color: "+cp.color)
		}
		if cp.font != base.font {
			st = append(st, "font-family: "+fontStack(cp.font))
		}
		if cp.style&1 != base.style&1 {
			w := "normal"
			if cp.style&1 != 0 {
				w = "bold"
			}
			st = append(st, "font-weight: "+w)
		}
		if cp.style&2 != base.style&2 {
			w := "normal"
			if cp.style&2 != 0 {
				w = "italic"
			}
			st = append(st, "font-style: "+w)
		}
		if cp.style&4 != base.style&4 && cp.style&4 != 0 {
			st = append(st, "text-decoration: underline")
		}
		if len(st) > 0 {
			fmt.Fprintf(&b, `<span style="%s">%s</span>`, strings.Join(st, "; "), txt)
		} else {
			b.WriteString(txt)
		}
	}
	p := c.pkg
	align := "center"
	if v, ok := p.rowProp(s, "Paragraph", runs[0].ParaIX, "HorzAlign"); ok {
		switch int(fnum(v)) {
		case 0:
			align = "left"
		case 2:
			align = "right"
		}
	}
	valign := "middle"
	if v, ok := p.val(s, "VerticalAlign"); ok {
		switch int(fnum(v)) {
		case 0:
			valign = "top"
		case 2:
			valign = "bottom"
		}
	}
	st := []string{"html=1", "whiteSpace=wrap",
		"fontSize=" + num2(base.size), "fontColor=" + base.color, "fontFamily=" + fontStack(base.font),
		"fontStyle=" + strconv.Itoa(base.style&7), "align=" + align, "verticalAlign=" + valign,
		"spacingLeft=" + num2(p.num(s, "LeftMargin", 0.05)*pxPerInch),
		"spacingRight=" + num2(p.num(s, "RightMargin", 0.05)*pxPerInch),
		"spacingTop=" + num2(math.Min(p.num(s, "TopMargin", 0.05)*pxPerInch, 1)),
		"spacingBottom=" + num2(math.Min(p.num(s, "BottomMargin", 0.05)*pxPerInch, 1))}
	if v, ok := s.cell("TextBkgnd"); ok {
		if col := p.color(v); col != "" && col != "#000000" {
			if tr, _ := p.val(s, "TextBkgndTrans"); fnum(tr) < 1 {
				st = append(st, "labelBackgroundColor="+col)
			}
		}
	}
	return labelInfo{b.String(), strings.Join(st, ";")}, true
}

func (c *Conv) lineStyle(s *XShape, noLine bool) []string {
	p := c.pkg
	if noLine {
		return []string{"strokeColor=none"}
	}
	col := "#000000"
	if v, ok := p.val(s, "LineColor"); ok {
		if x := p.color(v); x != "" {
			col = x
		}
	}
	lw := math.Max(p.num(s, "LineWeight", 0.0139)*pxPerInch, 0.75)
	st := []string{"strokeColor=" + col, "strokeWidth=" + num2(lw)}
	switch int(p.num(s, "LinePattern", 1)) {
	case 0:
		return []string{"strokeColor=none"}
	case 2, 7, 8, 9, 10:
		st = append(st, "dashed=1", "fixDash=1", "dashPattern="+num2(math.Max(lw*4, 4))+" "+num2(math.Max(lw*3, 3)))
	case 3:
		st = append(st, "dashed=1", "fixDash=1", "dashPattern="+num2(math.Max(lw, 1.5))+" "+num2(math.Max(lw*2, 2.5)))
	case 4, 5, 6:
		st = append(st, "dashed=1", "fixDash=1", "dashPattern="+num2(math.Max(lw*4, 4))+" "+num2(math.Max(lw*2, 2.5))+" "+num2(math.Max(lw, 1.5))+" "+num2(math.Max(lw*2, 2.5)))
	}
	return st
}

func (c *Conv) fillStyle(s *XShape, noFill bool) []string {
	p := c.pkg
	pat := int(p.num(s, "FillPattern", 1))
	if noFill || pat == 0 {
		return []string{"fillColor=none"}
	}
	col := "#FFFFFF"
	if v, ok := p.val(s, "FillForegnd"); ok {
		if x := p.color(v); x != "" {
			col = x
		}
	}
	if p.num(s, "FillGradientEnabled", 0) == 1 {
		if first, last := c.gradientStops(s); first != "" {
			dir := "south"
			if a := math.Mod(p.num(s, "FillGradientAngle", 0)*180/math.Pi+360, 360); a >= 45 && a < 135 {
				dir = "west"
			} else if a >= 135 && a < 225 {
				dir = "north"
			} else if a >= 225 && a < 315 {
				dir = "east"
			}
			st := []string{"fillColor=" + first}
			if last != first {
				st = append(st, "gradientColor="+last, "gradientDirection="+dir)
			}
			return st
		}
	}
	st := []string{"fillColor=" + col}
	if tr := p.num(s, "FillForegndTrans", 0); tr > 0 && tr < 1 {
		st = append(st, "fillOpacity="+num2((1-tr)*100))
	}
	if pat > 1 && pat < 25 {
		// Visio patterns draw FillForegnd lines over FillBkgnd;
		// draw.io hatch fill draws fillColor lines over a transparent
		// background, which matches the common case of a white or
		// (mostly) transparent background.
		st = append(st, patternStyle(pat)...)
		c.warn("fill pattern approximated with a draw.io hatch fill")
	}
	return st
}

// patternStyle maps a Visio FillPattern (2..24) to a draw.io fill style.
// Pattern 2 is Visio's diagonal-line pattern ("///"); the other
// patterns have no documented geometry, so they fall back to the same
// diagonal hatch.
func patternStyle(pat int) []string {
	return []string{"fillStyle=hatch", "hachureGap=6", "fillWeight=0.75", "hachureAngle=-45"}
}

// gradientStops returns the first and last colours of the shape
// FillGradient section.
func (c *Conv) gradientStops(s *XShape) (first, last string) {
	for _, sec := range s.Sections {
		if sec.N != "FillGradient" {
			continue
		}
		for _, r := range sec.Rows {
			for _, cell := range r.Cells {
				if cell.N != "GradientStopColor" {
					continue
				}
				if x := c.pkg.color(cell.V); x != "" {
					if first == "" {
						first = x
					}
					last = x
				}
			}
		}
	}
	return
}

func (c *Conv) add(l *LayerOut, o *Out, hidden bool) {
	o.ID = c.newID()
	o.Parent = l.ID
	o.Hidden = hidden && l.Visible
	l.Cells = append(l.Cells, o)
}

func (c *Conv) walk(shapes []XShape, parent Mat, ctx walkCtx) {
	for i := range shapes {
		c.shape(&shapes[i], parent, ctx)
	}
}

func (c *Conv) shape(s *XShape, parent Mat, ctx walkCtx) {
	p := c.pkg
	md := ctx.master
	if s.Master != "" {
		if md = p.master(s.Master); md != nil && len(md.shapes) > 0 {
			inherit(s, &md.shapes[0], md)
		}
	} else if s.MasterShape != "" && md != nil {
		if ms := md.byID[s.MasterShape]; ms != nil {
			inherit(s, ms, md)
		}
	}
	ctx.master = md
	w, h := p.num(s, "Width", 0), p.num(s, "Height", 0)
	pinX, pinY := p.num(s, "PinX", 0), p.num(s, "PinY", 0)
	lpx, lpy := p.num(s, "LocPinX", w/2), p.num(s, "LocPinY", h/2)
	ang := p.num(s, "Angle", 0)
	fx, fy := p.num(s, "FlipX", 0) == 1, p.num(s, "FlipY", 0) == 1
	// local (visio, y-up) -> parent. Visio flips a shape about its pin
	// (LocPin), not about its centre, so the flip is applied after
	// moving LocPin to the origin.
	sx, sy := 1.0, 1.0
	if fx {
		sx = -1
	}
	if fy {
		sy = -1
	}
	flip := Mat{A: sx, D: sy}
	shift := Mat{A: 1, D: 1, E: -lpx, F: -lpy}
	cs, sn := math.Cos(ang), math.Sin(ang)
	rot := Mat{A: cs, B: sn, C: -sn, D: cs}
	place := Mat{A: 1, D: 1, E: pinX, F: pinY}
	M := parent.Mul(place).Mul(rot).Mul(flip).Mul(shift)

	layer, layerName, hidden := c.layerFor(s, ctx)
	nctx := ctx
	nctx.angle += ang
	if fy {
		nctx.flipYs++
	}
	if c.cur == nil && ctx.layer == nil {
		nctx.layer = layer
		nctx.hidden = hidden
	} else if c.cur == nil {
		nctx.hidden = hidden
	}

	name := c.shapeName(s)
	kind := ""
	txt := ""
	if rs := c.parseText(s); len(rs) > 0 {
		var sb strings.Builder
		for _, r := range rs {
			sb.WriteString(r.Text)
		}
		txt = strings.TrimSpace(strings.ReplaceAll(sb.String(), "\n", " | "))
	}

	if s.Master != "" && md == nil && len(s.Sections) == 0 && len(s.Children) == 0 && s.Foreign == nil {
		c.warn("shape references a master that could not be loaded")
	}

	nAfter := len(c.after)
	if s.Foreign != nil {
		kind = c.foreign(s, M, layer, hidden, w, h, nctx)
	} else {
		kind = c.vector(s, M, layer, hidden, w, h, nctx)
	}
	if c.depth == 0 && (kind != "" || txt != "") {
		c.Report = append(c.Report, ReportRow{Page: c.pg.Ref.Name, Shape: s.ID, Name: name, Layer: layerName, Hidden: hidden, Text: txt, Kind: kind})
	}
	if len(s.Children) > 0 {
		c.walk(s.Children, M, nctx)
	}
	if len(c.after) > nAfter {
		for _, f := range c.after[nAfter:] {
			f()
		}
		c.after = c.after[:nAfter]
	}
}

func rotDeg(angle float64, _ int) float64 {
	d := -angle * 180 / math.Pi
	d = math.Mod(d, 360)
	if d < 0 {
		d += 360
	}
	return d
}

func (c *Conv) textBox(s *XShape, w, h float64) (x0, y0, tw, th float64, full bool) {
	p := c.pkg
	tw = p.num(s, "TxtWidth", w)
	th = p.num(s, "TxtHeight", h)
	tpx, tpy := p.num(s, "TxtPinX", w/2), p.num(s, "TxtPinY", h/2)
	tlx, tly := p.num(s, "TxtLocPinX", tw/2), p.num(s, "TxtLocPinY", th/2)
	x0, y0 = tpx-tlx, tpy-tly
	full = math.Abs(x0) < 1e-3 && math.Abs(y0) < 1e-3 && math.Abs(tw-w) < 1e-3 && math.Abs(th-h) < 1e-3
	return
}

func (c *Conv) vector(s *XShape, M Mat, layer *LayerOut, hidden bool, w, h float64, ctx walkCtx) string {
	secs := parseGeometry(s, w, h, c.warn)
	rotation := rotDeg(ctx.angle, ctx.flipYs)
	label, hasText := c.buildLabel(s, w, h, rotation)

	if len(secs) == 0 {
		if hasText {
			if len(s.Children) > 0 {
				// Visio paints a group's text above its sub-shapes
				c.after = append(c.after, func() { c.textCell(s, M, layer, hidden, w, h, label, rotation) })
				return "text"
			}
			c.textCell(s, M, layer, hidden, w, h, label, rotation)
			return "text"
		}
		return ""
	}

	// native rectangle / ellipse
	if len(secs) == 1 && len(secs[0].Paths) == 1 {
		g, sp := secs[0], secs[0].Paths[0]
		_, _, _, _, full := c.textBox(s, w, h)
		if sp.Ellipse != nil && w > 0 && h > 0 && (full || !hasText) {
			e := sp.Ellipse
			ctr := M.Apply(Pt{e[0], e[1]})
			st := append([]string{"ellipse"}, c.fillStyle(s, g.NoFill)...)
			st = append(st, c.lineStyle(s, g.NoLine)...)
			if hasText {
				st = append(st, label.style)
			}
			st = append(st, "rotation="+num2(matRot(M)))
			o := &Out{Style: strings.Join(st, ";"), X: ctr.X - e[2]*c.gpx(), Y: ctr.Y - e[3]*c.gpx(), W: 2 * e[2] * c.gpx(), H: 2 * e[3] * c.gpx()}
			if hasText {
				o.Value = label.html
			}
			c.add(layer, o, hidden)
			return "ellipse"
		}
		if isRect(sp, w, h) && (full || !hasText) {
			ctr := M.Apply(Pt{w / 2, h / 2})
			st := []string{}
			if r := c.pkg.num(s, "Rounding", 0); r > 0 {
				st = append(st, "rounded=1", "absoluteArcSize=1", "arcSize="+num2(r*c.gpx()*2))
			} else {
				st = append(st, "rounded=0")
			}
			st = append(st, c.fillStyle(s, g.NoFill)...)
			st = append(st, c.lineStyle(s, g.NoLine)...)
			if hasText {
				st = append(st, label.style)
			}
			st = append(st, "rotation="+num2(matRot(M)))
			o := &Out{Style: strings.Join(st, ";"), X: ctr.X - w*c.gpx()/2, Y: ctr.Y - h*c.gpx()/2, W: w * c.gpx(), H: h * c.gpx()}
			if hasText {
				o.Value = label.html
			}
			c.add(layer, o, hidden)
			return "rect"
		}
	}

	// open single polyline -> native edge
	if len(secs) == 1 && len(secs[0].Paths) == 1 && !secs[0].Paths[0].Closed && secs[0].Paths[0].Ellipse == nil && secs[0].NoFill {
		sp := secs[0].Paths[0]
		var pts []Pt
		for _, q := range sp.Pts {
			pts = append(pts, M.Apply(q))
		}
		st := []string{"endArrow=none", "startArrow=none", "html=1", "edgeStyle=none"}
		st = append(st, c.lineStyle(s, secs[0].NoLine)...)
		if r := c.pkg.num(s, "Rounding", 0); r > 0 {
			st = append(st, "rounded=1", "arcSize="+num2(r*c.gpx()*2))
		} else {
			st = append(st, "rounded=0")
		}
		if a := int(c.pkg.num(s, "EndArrow", 0)); a > 0 {
			st = append(st, "endArrow=classic", "endFill=1", "endSize="+num2(arrowSize(c.pkg.num(s, "EndArrowSize", 2))))
		}
		if a := int(c.pkg.num(s, "BeginArrow", 0)); a > 0 {
			st = append(st, "startArrow=classic", "startFill=1", "startSize="+num2(arrowSize(c.pkg.num(s, "BeginArrowSize", 2))))
		}
		o := &Out{Edge: true, Style: strings.Join(st, ";"), Src: pts[0], Dst: pts[len(pts)-1]}
		if len(pts) > 2 {
			o.Via = pts[1 : len(pts)-1]
		}
		c.add(layer, o, hidden)
		if hasText {
			c.textCell(s, M, layer, hidden, w, h, label, rotation)
		}
		return "line"
	}

	// generic: stencil in bbox of page-space polygon points
	type tp struct {
		subs   [][]Pt
		closed bool
		fill   bool
		stroke bool
	}
	// Visio paints every geometry's fill before any outline, so
	// outlines of a no-fill section (e.g. the "I" of a column) stay
	// visible over a later filled section.
	// Fill is emitted first, strokes follow in geometry order.
	var paths []tp
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var fillS, lineS []string
	var closedSubs [][]Pt
	for _, g := range secs {
		for _, sp := range g.Paths {
			pts := sp.Pts
			if sp.Ellipse != nil {
				e := sp.Ellipse
				pts = nil
				for k := 0; k <= 36; k++ {
					t := 2 * math.Pi * float64(k) / 36
					pts = append(pts, Pt{e[0] + e[2]*math.Cos(t), e[1] + e[3]*math.Sin(t)})
				}
			}
			var q []Pt
			for _, pt := range pts {
				m := M.Apply(pt)
				q = append(q, m)
				minX, maxX = math.Min(minX, m.X), math.Max(maxX, m.X)
				minY, maxY = math.Min(minY, m.Y), math.Max(maxY, m.Y)
			}
			if sp.Closed && !g.NoFill {
				closedSubs = append(closedSubs, q)
			}
			if !g.NoLine {
				paths = append(paths, tp{[][]Pt{q}, sp.Closed, false, true})
			}
		}
	}
	if len(closedSubs) > 0 {
		evenOddWinding(closedSubs)
		paths = append([]tp{{closedSubs, true, true, false}}, paths...)
	}
	if len(paths) == 0 {
		return ""
	}
	if fillS == nil {
		fillS = c.fillStyle(s, false)
	}
	lineS = c.lineStyle(s, false)
	bw, bh := math.Max(maxX-minX, 1), math.Max(maxY-minY, 1)
	var sb strings.Builder
	fmt.Fprintf(&sb, `<shape name="s" w="%s" h="%s" aspect="variable" strokewidth="inherit"><connections/><foreground>`, num2(bw), num2(bh))
	for _, t := range paths {
		sb.WriteString("<path>")
		for _, sub := range t.subs {
			for k, q := range sub {
				tag := "line"
				if k == 0 {
					tag = "move"
				}
				fmt.Fprintf(&sb, `<%s x="%s" y="%s"/>`, tag, num2(q.X-minX), num2(q.Y-minY))
			}
			if t.closed {
				sb.WriteString("<close/>")
			}
		}
		sb.WriteString("</path>")
		switch {
		case t.fill && t.stroke:
			sb.WriteString("<fillstroke/>")
		case t.fill:
			sb.WriteString("<fill/>")
		case t.stroke:
			sb.WriteString("<stroke/>")
		}
	}
	sb.WriteString("</foreground></shape>")
	st := []string{stencilStyle(sb.String())}
	st = append(st, fillS...)
	st = append(st, lineS...)
	o := &Out{Style: strings.Join(st, ";"), X: minX, Y: minY, W: bw, H: bh}
	c.add(layer, o, hidden)
	if hasText {
		c.textCell(s, M, layer, hidden, w, h, label, rotation)
	}
	return "path"
}

func arrowSize(v float64) float64 { return 3 + v*1.5 }

func isRect(sp SubPath, w, h float64) bool {
	if sp.Ellipse != nil || !sp.Closed || len(sp.Pts) < 4 || len(sp.Pts) > 6 {
		return false
	}
	eps := 1e-3
	for _, q := range sp.Pts {
		okx := math.Abs(q.X) < eps || math.Abs(q.X-w) < eps
		oky := math.Abs(q.Y) < eps || math.Abs(q.Y-h) < eps
		if !okx || !oky {
			return false
		}
	}
	return w > 0 && h > 0
}

func (c *Conv) textCell(s *XShape, M Mat, layer *LayerOut, hidden bool, w, h float64, l labelInfo, rotation float64) {
	x0, y0, tw, th, _ := c.textBox(s, w, h)
	if tw <= 0 {
		tw = w
	}
	if th <= 0 {
		th = h
	}
	if tw <= 0 || th <= 0 {
		c.warn("text on zero-size shape skipped")
		return
	}
	ctr := M.Apply(Pt{x0 + tw/2, y0 + th/2})
	st := "text;strokeColor=none;fillColor=none;" + l.style + ";rotation=" + num2(rotation)
	c.add(layer, &Out{Value: l.html, Style: st, X: ctr.X - tw*c.gpx()/2, Y: ctr.Y - th*c.gpx()/2, W: tw * c.gpx(), H: th * c.gpx()}, hidden)
}

func (c *Conv) foreign(s *XShape, M Mat, layer *LayerOut, hidden bool, w, h float64, ctx walkCtx) string {
	part := c.pg.File
	if s.relPart != "" {
		part = s.relPart
	}
	rels := readRels(c.pkg, part)
	target := rels[s.Foreign.Rel.ID]
	ctr := M.Apply(Pt{w / 2, h / 2})
	x, y, pw, ph := ctr.X-w*c.gpx()/2, ctr.Y-h*c.gpx()/2, w*c.gpx(), h*c.gpx()
	rotation := rotDeg(ctx.angle, ctx.flipYs)
	if strings.HasSuffix(strings.ToLower(target), ".vsdx") && c.depth < 3 {
		data, err := c.pkg.read(target)
		if err == nil {
			if sub, err := openPackageBytes(data); err == nil && len(sub.Pages) > 0 {
				ep := sub.Pages[0]
				ow, oh := ep.W, ep.H
				if ow > 0 && oh > 0 {
					shapes, err := ep.shapes()
					if err == nil {
						sc := newConv(ep)
						sc.u = 1
						sc.depth = c.depth + 1
						sc.cur = layer
						sc.next = c.next + 100000*(c.depth+1)
						host := M.Mul(scaleMat(w/ow, h/oh))
						sc.walk(shapes, host, walkCtx{hidden: hidden})
						c.next = sc.next
						for k, v := range sc.Warn {
							c.Warn["[embedded] "+k] += v
						}
						return "embedded-vsdx"
					}
				}
			}
		}
		c.warn("embedded vsdx could not be converted")
	}
	// raster / metafile
	if target != "" {
		if data, err := c.pkg.read(target); err == nil {
			if mime, b := toPNG(target, data); b != nil {
				st := fmt.Sprintf("shape=image;verticalLabelPosition=bottom;verticalAlign=top;imageAspect=0;image=data:%s,%s;rotation=%s", mime, base64.StdEncoding.EncodeToString(b), num2(rotation))
				c.add(layer, &Out{Style: st, X: x, Y: y, W: pw, H: ph}, hidden)
				return "image"
			}
		}
	}
	c.warn("image could not be converted: " + filepath.Base(target))
	c.add(layer, &Out{Value: "MISSING IMAGE " + filepath.Base(target), Style: "rounded=0;fillColor=none;strokeColor=#FF0000;dashed=1;opacity=40;fontSize=8;html=1;whiteSpace=wrap", X: x, Y: y, W: pw, H: ph}, hidden)
	return "image-missing"
}

func toPNG(name string, data []byte) (string, []byte) {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png":
		return "image/png", data
	case ".jpg", ".jpeg":
		return "image/jpeg", data
	case ".gif":
		return "image/gif", data
	}
	if ext == ".emf" {
		if b := pngInsideEMF(data); b != nil {
			return "image/png", b
		}
	}
	dir, err := os.MkdirTemp("", "vsdximg")
	if err != nil {
		return "", nil
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in"+ext)
	out := filepath.Join(dir, "out.png")
	os.WriteFile(in, data, 0o600)
	if ext == ".emf" {
		svg := filepath.Join(dir, "out.svg")
		if err := exec.Command("emf2svg-conv", "-i", in, "-o", svg).Run(); err == nil {
			if b, err := os.ReadFile(svg); err == nil && len(b) > 0 {
				return "image/svg+xml", b
			}
		}
	}
	if len(data) > 4 && (data[0] == 0x01 && data[1] == 0x00 && data[2] == 0x09 || data[0] == 0xd7 && data[1] == 0xcd) {
		// WMF (often saved with an .emf extension): libwmf writes its embedded bitmaps next to the SVG
		wmf := filepath.Join(dir, "in.wmf")
		os.WriteFile(wmf, data, 0o600)
		if exec.Command("wmf2svg", "-o", filepath.Join(dir, "w.svg"), wmf).Run() == nil {
			var best []byte
			files, _ := filepath.Glob(filepath.Join(dir, "in-*.png"))
			for _, f := range files {
				if b, err := os.ReadFile(f); err == nil && len(b) > len(best) {
					best = b
				}
			}
			if best != nil {
				return "image/png", best
			}
		}
	}
	if err := exec.Command("sips", "-s", "format", "png", in, "--out", out).Run(); err == nil {
		if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
			return "image/png", b
		}
	}
	return "", nil
}

// preserveSpaces keeps Visio's literal spacing, which HTML would
// otherwise collapse.
func preserveSpaces(s string) string {
	var b strings.Builder
	prev := byte('\n')
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' && (prev == ' ' || prev == '\n' || i+1 < len(s) && s[i+1] == ' ') {
			b.WriteString("&nbsp;")
			prev = 'x'
			continue
		}
		b.WriteByte(s[i])
		prev = s[i]
	}
	return b.String()
}

// fontStack adds metric-compatible fallbacks so labels don't grow when
// Calibri is missing.
func fontStack(f string) string {
	if f == "Calibri" {
		return "Calibri, Carlito, Arial"
	}
	return f
}

// fitScale shrinks the font until the wrapped text is estimated to
// fit its text box.
func (c *Conv) fitScale(s *XShape, runs []textRun, w, h, rotation float64) float64 {
	_, _, tw, th, _ := c.textBox(s, w, h)
	if tw <= 0 {
		tw = w
	}
	if th <= 0 {
		th = h
	}
	if r := math.Mod(rotation, 180); math.Abs(r-90) < 1 {
		tw, th = th, tw
	}
	p := c.pkg
	availW := tw*c.gpx() - (p.num(s, "LeftMargin", 0.05)+p.num(s, "RightMargin", 0.05))*pxPerInch
	availH := th*c.gpx() - 2
	if availW < 4 || availH < 4 {
		return 1
	}
	c.k = 1
	maxSize := 0.0
	var text strings.Builder
	bold := false
	for _, r := range runs {
		cp := c.charProps(s, r.CharIX)
		maxSize = math.Max(maxSize, cp.size)
		bold = bold || cp.style&1 != 0
		text.WriteString(r.Text)
	}
	if maxSize <= 0 {
		return 1
	}
	cw := 0.62
	if bold {
		cw = 0.66
	}
	lines := func(size float64) int {
		n := 0
		for _, para := range strings.Split(text.String(), "\n") {
			n++
			cur := 0.0
			for _, wd := range strings.Fields(para) {
				l := float64(len([]rune(wd))) * size * cw
				sp := size * cw * 0.45
				if cur > 0 && cur+sp+l > availW {
					n++
					cur = 0
				}
				if cur > 0 {
					cur += sp
				}
				cur += l
				for cur > availW && availW > size { // single word wider than the box breaks
					cur -= availW
					n++
				}
			}
		}
		return n
	}
	minK := 5.0 / maxSize
	if minK > 1 {
		return 1
	}
	paras := len(strings.Split(text.String(), "\n"))
	if paras > 1 && lines(maxSize) == paras {
		return 1 // explicit line breaks that fit the width: overflow vertically like Visio
	}
	single := !strings.Contains(text.String(), "\n") && len(strings.Fields(text.String())) > 1
	for k := 1.0; k > minK; k -= 0.05 {
		if single && k >= 0.75 && lines(maxSize*k) > 1 {
			continue
		}
		if float64(lines(maxSize*k))*maxSize*k*1.2 <= availH {
			return k
		}
	}
	return minK
}

// pngInsideEMF extracts a PNG stored inside an EMF+ record
// (how Visio embeds pasted PNGs).
func pngInsideEMF(data []byte) []byte {
	sig := []byte("\x89PNG\r\n\x1a\n")
	i := bytes.Index(data, sig)
	if i < 0 {
		return nil
	}
	end := bytes.Index(data[i:], []byte("IEND"))
	if end < 0 || i+end+8 > len(data) {
		return nil
	}
	return data[i : i+end+8]
}

func signedArea(p []Pt) float64 {
	a := 0.0
	for i := range p {
		j := (i + 1) % len(p)
		a += p[i].X*p[j].Y - p[j].X*p[i].Y
	}
	return a / 2
}

func inPoly(pt Pt, poly []Pt) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > pt.Y) != (b.Y > pt.Y) && pt.X < (b.X-a.X)*(pt.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

// evenOddWinding reverses subpaths so that a nonzero fill reproduces
// Visio even-odd fill: nesting depth parity sets the direction.
func evenOddWinding(subs [][]Pt) {
	if len(subs) < 2 {
		return
	}
	for i, s := range subs {
		if len(s) < 3 {
			continue
		}
		depth := 0
		for j, o := range subs {
			if i != j && len(o) >= 3 && inPoly(s[0], o) {
				depth++
			}
		}
		if (signedArea(s) > 0) != (depth%2 == 0) {
			for a, b := 0, len(s)-1; a < b; a, b = a+1, b-1 {
				s[a], s[b] = s[b], s[a]
			}
		}
	}
}

// matRot returns the draw.io rotation (degrees, clockwise) of a shape's
// local x axis under M. Unlike the accumulated Visio angle it accounts
// for flipped parents; it is only used for shapes that are symmetric
// under mirroring (rectangles, ellipses).
func matRot(M Mat) float64 {
	d := math.Atan2(M.B, M.A) * 180 / math.Pi
	if d < 0 {
		d += 360
	}
	if math.Abs(d-360) < 1e-9 {
		d = 0
	}
	return d
}
