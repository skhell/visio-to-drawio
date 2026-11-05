// geom.go: geometry and 2D transforms.
//
// Pt and Mat are the point and affine-matrix types used to place shapes
// on the page. parseGeometry turns a shape's visible Geometry sections
// into subpaths in the shape's local coordinates (inches, y up),
// keeping each section NoFill/NoLine flags. Curves are flattened to
// polylines: elliptical and circular arcs, Bezier curves, NURBS
// (approximated with a clamped cubic B-spline) and POLYLINE formulas.
// Axis-aligned ellipses stay ellipses so they can become native
// draw.io ellipses.

package converter

import (
	"math"
	"strconv"
	"strings"
)

type Pt struct{ X, Y float64 }

// Mat maps (x,y) -> (A*x+C*y+E, B*x+D*y+F).
type Mat struct{ A, B, C, D, E, F float64 }

func (m Mat) Apply(p Pt) Pt { return Pt{m.A*p.X + m.C*p.Y + m.E, m.B*p.X + m.D*p.Y + m.F} }

// Mul returns m n (apply n first, then m).
func (m Mat) Mul(n Mat) Mat {
	return Mat{
		m.A*n.A + m.C*n.B, m.B*n.A + m.D*n.B,
		m.A*n.C + m.C*n.D, m.B*n.C + m.D*n.D,
		m.A*n.E + m.C*n.F + m.E, m.B*n.E + m.D*n.F + m.F,
	}
}

func scaleMat(sx, sy float64) Mat { return Mat{A: sx, D: sy} }

type SubPath struct {
	Pts     []Pt
	Closed  bool
	Ellipse *[4]float64 // cx, cy, rx, ry (axis aligned only)
}

type GeoSection struct {
	NoFill, NoLine bool
	Paths          []SubPath
}

func fnum(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// parseGeometry returns every visible Geometry section in local
// coordinates (inches).
func parseGeometry(s *XShape, w, h float64, warn func(string)) []GeoSection {
	var out []GeoSection
	for _, sec := range s.Sections {
		if sec.N != "Geometry" {
			continue
		}
		g := GeoSection{}
		noShow := false
		for _, c := range sec.Cells {
			switch c.N {
			case "NoFill":
				g.NoFill = c.V == "1"
			case "NoLine":
				g.NoLine = c.V == "1"
			case "NoShow":
				noShow = c.V == "1"
			}
		}
		if noShow {
			continue
		}
		var cur *SubPath
		flush := func() {
			if cur != nil && len(cur.Pts) > 1 || cur != nil && cur.Ellipse != nil {
				c := cur
				if len(c.Pts) > 2 && dist(c.Pts[0], c.Pts[len(c.Pts)-1]) < 1e-6 {
					c.Closed = true
				}
				g.Paths = append(g.Paths, *c)
			}
			cur = nil
		}
		var last Pt
		for _, r := range sec.Rows {
			rc := map[string]float64{}
			for _, c := range r.Cells {
				rc[c.N] = fnum(c.V)
			}
			switch r.T {
			case "MoveTo":
				flush()
				last = Pt{rc["X"], rc["Y"]}
				cur = &SubPath{Pts: []Pt{last}}
			case "RelMoveTo":
				flush()
				last = Pt{rc["X"] * w, rc["Y"] * h}
				cur = &SubPath{Pts: []Pt{last}}
			case "LineTo", "RelLineTo":
				p := Pt{rc["X"], rc["Y"]}
				if r.T == "RelLineTo" {
					p = Pt{p.X * w, p.Y * h}
				}
				if cur == nil {
					cur = &SubPath{Pts: []Pt{last}}
				}
				cur.Pts = append(cur.Pts, p)
				last = p
			case "EllipticalArcTo":
				p := Pt{rc["X"], rc["Y"]}
				if cur == nil {
					cur = &SubPath{Pts: []Pt{last}}
				}
				cur.Pts = append(cur.Pts, ellipticalArc(last, p, Pt{rc["A"], rc["B"]}, rc["C"], rc["D"])...)
				last = p
			case "ArcTo":
				p := Pt{rc["X"], rc["Y"]}
				if cur == nil {
					cur = &SubPath{Pts: []Pt{last}}
				}
				mid := Pt{(last.X + p.X) / 2, (last.Y + p.Y) / 2}
				dx, dy := p.X-last.X, p.Y-last.Y
				l := math.Hypot(dx, dy)
				if l > 0 && rc["A"] != 0 {
					ctrl := Pt{mid.X + dy/l*rc["A"], mid.Y - dx/l*rc["A"]}
					cur.Pts = append(cur.Pts, ellipticalArc(last, p, ctrl, 0, 1)...)
				} else {
					cur.Pts = append(cur.Pts, p)
				}
				last = p
			case "NURBSTo":
				p := Pt{rc["X"], rc["Y"]}
				if cur == nil {
					cur = &SubPath{Pts: []Pt{last}}
				}
				e := ""
				for _, c := range r.Cells {
					if c.N == "E" {
						e = c.V
					}
				}
				cur.Pts = append(cur.Pts, nurbs(last, p, e, w, h)...)
				last = p
			case "CubBezTo", "RelCubBezTo", "QuadBezTo", "RelQuadBezTo":
				sx, sy := 1.0, 1.0
				if strings.HasPrefix(r.T, "Rel") {
					sx, sy = w, h
				}
				p := Pt{rc["X"] * sx, rc["Y"] * sy}
				if cur == nil {
					cur = &SubPath{Pts: []Pt{last}}
				}
				c1 := Pt{rc["A"] * sx, rc["B"] * sy}
				if strings.Contains(r.T, "Cub") {
					cur.Pts = append(cur.Pts, bezier(last, c1, Pt{rc["C"] * sx, rc["D"] * sy}, p)...)
				} else {
					e1 := Pt{last.X + 2.0/3*(c1.X-last.X), last.Y + 2.0/3*(c1.Y-last.Y)}
					e2 := Pt{p.X + 2.0/3*(c1.X-p.X), p.Y + 2.0/3*(c1.Y-p.Y)}
					cur.Pts = append(cur.Pts, bezier(last, e1, e2, p)...)
				}
				last = p
			case "RelEllipticalArcTo":
				p := Pt{rc["X"] * w, rc["Y"] * h}
				if cur == nil {
					cur = &SubPath{Pts: []Pt{last}}
				}
				cur.Pts = append(cur.Pts, ellipticalArc(last, p, Pt{rc["A"] * w, rc["B"] * h}, rc["C"], rc["D"])...)
				last = p
			case "PolylineTo":
				p := Pt{rc["X"], rc["Y"]}
				if cur == nil {
					cur = &SubPath{Pts: []Pt{last}}
				}
				for _, c := range r.Cells {
					if c.N == "A" {
						cur.Pts = append(cur.Pts, polylinePoints(c.F, w, h)...)
					}
				}
				cur.Pts = append(cur.Pts, p)
				last = p
			case "Ellipse":
				flush()
				cx, cy := rc["X"], rc["Y"]
				rx, ry := math.Hypot(rc["A"]-cx, rc["B"]-cy), math.Hypot(rc["C"]-cx, rc["D"]-cy)
				e := [4]float64{cx, cy, rx, ry}
				cur = &SubPath{Ellipse: &e, Closed: true}
				flush()
			default:
				warn("unsupported geometry row " + r.T)
				if _, ok := rc["X"]; ok && cur != nil {
					p := Pt{rc["X"], rc["Y"]}
					cur.Pts = append(cur.Pts, p)
					last = p
				}
			}
		}
		flush()
		if len(g.Paths) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func dist(a, b Pt) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }

// ellipticalArc returns points from p0 (exclusive) to p1 (inclusive)
// along the arc through ctrl.
func ellipticalArc(p0, p1, ctrl Pt, angle, ratio float64) []Pt {
	if ratio <= 0 {
		ratio = 1
	}
	cs, sn := math.Cos(-angle), math.Sin(-angle)
	fwd := func(p Pt) Pt { // rotate into ellipse frame, stretch minor axis to make a circle
		x, y := p.X*cs-p.Y*sn, p.X*sn+p.Y*cs
		return Pt{x, y * ratio}
	}
	cb, sb := math.Cos(angle), math.Sin(angle)
	inv := func(p Pt) Pt {
		x, y := p.X, p.Y/ratio
		return Pt{x*cb - y*sb, x*sb + y*cb}
	}
	a, b, c := fwd(p0), fwd(ctrl), fwd(p1)
	d := 2 * (a.X*(b.Y-c.Y) + b.X*(c.Y-a.Y) + c.X*(a.Y-b.Y))
	if math.Abs(d) < 1e-12 {
		return []Pt{p1}
	}
	ux := ((a.X*a.X+a.Y*a.Y)*(b.Y-c.Y) + (b.X*b.X+b.Y*b.Y)*(c.Y-a.Y) + (c.X*c.X+c.Y*c.Y)*(a.Y-b.Y)) / d
	uy := ((a.X*a.X+a.Y*a.Y)*(c.X-b.X) + (b.X*b.X+b.Y*b.Y)*(a.X-c.X) + (c.X*c.X+c.Y*c.Y)*(b.X-a.X)) / d
	t0 := math.Atan2(a.Y-uy, a.X-ux)
	tc := math.Atan2(b.Y-uy, b.X-ux)
	t1 := math.Atan2(c.Y-uy, c.X-ux)
	r := math.Hypot(a.X-ux, a.Y-uy)
	norm := func(x float64) float64 {
		for x < 0 {
			x += 2 * math.Pi
		}
		for x >= 2*math.Pi {
			x -= 2 * math.Pi
		}
		return x
	}
	ccw1, ccwc := norm(t1-t0), norm(tc-t0)
	sweep := ccw1
	if ccwc > ccw1 { // control point not on the CCW sweep: go clockwise
		sweep = ccw1 - 2*math.Pi
	}
	n := int(math.Max(4, math.Ceil(math.Abs(sweep)/(math.Pi/12))))
	out := make([]Pt, 0, n)
	for i := 1; i <= n; i++ {
		t := t0 + sweep*float64(i)/float64(n)
		out = append(out, inv(Pt{ux + r*math.Cos(t), uy + r*math.Sin(t)}))
	}
	out[len(out)-1] = p1
	return out
}

// nurbs samples a Visio NURBSTo as a clamped cubic B-spline over start
// + listed points + end.
// The exact Visio knot vector is not reproduced, so the curve
// is an approximation.
func nurbs(start, end Pt, e string, w, h float64) []Pt {
	open, cl := strings.Index(e, "("), strings.LastIndex(e, ")")
	if open < 0 || cl < open {
		return []Pt{end}
	}
	var v []float64
	for _, f := range strings.Split(e[open+1:cl], ",") {
		v = append(v, fnum(strings.TrimSpace(f)))
	}
	if len(v) < 4+4 {
		return []Pt{end}
	}
	xRel, yRel := v[2] == 0, v[3] == 0
	ctrl := []Pt{start}
	for i := 4; i+3 < len(v)+0 && i+1 < len(v); i += 4 {
		x, y := v[i], v[i+1]
		if xRel {
			x *= w
		}
		if yRel {
			y *= h
		}
		ctrl = append(ctrl, Pt{x, y})
	}
	ctrl = append(ctrl, end)
	deg := 3
	if len(ctrl) <= deg {
		deg = len(ctrl) - 1
	}
	n := len(ctrl)
	m := n + deg + 1
	knots := make([]float64, m)
	inner := n - deg
	for i := range knots {
		switch {
		case i <= deg:
			knots[i] = 0
		case i >= n:
			knots[i] = float64(inner)
		default:
			knots[i] = float64(i - deg)
		}
	}
	const steps = 24
	out := make([]Pt, 0, steps)
	for k := 1; k <= steps; k++ {
		t := float64(inner) * float64(k) / steps
		if k == steps {
			t = float64(inner) - 1e-9
		}
		out = append(out, deBoor(ctrl, knots, deg, t))
	}
	out[len(out)-1] = end
	return out
}

func deBoor(ctrl []Pt, knots []float64, deg int, t float64) Pt {
	n := len(ctrl)
	span := deg
	for span < n-1 && t >= knots[span+1] {
		span++
	}
	d := make([]Pt, deg+1)
	for j := 0; j <= deg; j++ {
		d[j] = ctrl[span-deg+j]
	}
	for r := 1; r <= deg; r++ {
		for j := deg; j >= r; j-- {
			i := span - deg + j
			den := knots[i+deg-r+1] - knots[i]
			a := 0.0
			if den != 0 {
				a = (t - knots[i]) / den
			}
			d[j] = Pt{(1-a)*d[j-1].X + a*d[j].X, (1-a)*d[j-1].Y + a*d[j].Y}
		}
	}
	return d[deg]
}

// polylinePoints parses the intermediate points of
// POLYLINE(xType, yType, x1, y1, ...).
// Type 0 means the coordinates are fractions of the shape width/height,
// otherwise inches.
func polylinePoints(f string, w, h float64) []Pt {
	f = strings.TrimSpace(f)
	if !strings.HasPrefix(strings.ToUpper(f), "POLYLINE(") || !strings.HasSuffix(f, ")") {
		return nil
	}
	parts := strings.Split(f[len("POLYLINE("):len(f)-1], ",")
	if len(parts) < 4 {
		return nil
	}
	xt, yt := fnum(strings.TrimSpace(parts[0])), fnum(strings.TrimSpace(parts[1]))
	var pts []Pt
	for i := 2; i+1 < len(parts); i += 2 {
		x, y := fnum(strings.TrimSpace(parts[i])), fnum(strings.TrimSpace(parts[i+1]))
		if xt == 0 {
			x *= w
		}
		if yt == 0 {
			y *= h
		}
		pts = append(pts, Pt{x, y})
	}
	return pts
}

// bezier flattens a cubic Bezier curve (quadratics are degree-elevated by the caller).
func bezier(p0, c1, c2, p1 Pt) []Pt {
	const n = 16
	pts := make([]Pt, 0, n)
	for i := 1; i <= n; i++ {
		t := float64(i) / n
		u := 1 - t
		pts = append(pts, Pt{
			u*u*u*p0.X + 3*u*u*t*c1.X + 3*u*t*t*c2.X + t*t*t*p1.X,
			u*u*u*p0.Y + 3*u*u*t*c1.Y + 3*u*t*t*c2.Y + t*t*t*p1.Y,
		})
	}
	return pts
}
