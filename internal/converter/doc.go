// Package converter turns Microsoft Visio drawings (.vsdx) and
// templates (.vstx) into draw.io (.drawio) diagrams.
//
// A Microsoft Visio file is an Open Packaging (ZIP) archive of XML
// parts: a document part with stylesheets, fonts and colours, a list of
// pages, one XML part per page with its shapes, and the masters
// (stencil shapes) those shapes inherit from.
// The converter reads these parts directly. It does not evaluate Visio
// ShapeSheet formulas it uses the values Visio last saved in each cell.
//
// Conversion pipeline:
//
//  1. Open loads the package and its pages (model.go, page.go).
//  2. Convert walks a page's shape tree. Each shape inherits unset
//     cells and sections from its master (master.go) and resolves the
//     rest through its stylesheet chain (model.go).
//  3. Each shape's geometry sections become paths in page coordinates
//     (geom.go). Its position comes from a transform built from its pin
//     rotation and flips, composed with its parent group's transform.
//  4. Every shape becomes one or more draw.io cells (convert.go): a
//     native rectangle, ellipse or edge when the geometry allows it,
//     otherwise an inline draw.io stencil. Text, images and embedded
//     .vsdx drawings become their own cells.
//  5. The cells are written as an mxGraphModel, one draw.io layer per
//     output layer, together with a per-shape report and the conversion
//     warnings.
//
// Coordinates: Visio uses inches with the y axis pointing up; draw.io
// uses pixels (72 per inch here) with the y axis pointing down.
// The root transform in convertPage flips and scales between the two.
package converter
