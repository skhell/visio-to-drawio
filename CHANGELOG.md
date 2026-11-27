# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.3] - 2026-11-27

### Added
- Text on images: a picture shape's own text is now converted to a
  label above the image.
- Uncompressed 24/32-bit BMP images are decoded natively, so their
  transparency is kept (`sips` turned transparent pixels black).
- Line patterns above 10 (Visio's custom dash styles) become a dashed
  line instead of a solid one.

### Changed
- Default label font is Calibri, the theme font of Visio's default
  styles, instead of Arial.

### Fixed
- Rectangles and ellipses whose text box is moved or resized stay
  native draw.io shapes; the text goes to a separate text cell placed
  where Visio draws it, instead of the whole shape becoming a stencil.
- Text boxes with zero height hang from (top-aligned) or rise to
  (bottom-aligned) their pin like in Visio, instead of being centred
  on the shape.
- `TxtAngle` is applied, so text kept upright on a rotated shape is no
  longer rotated with it.
- Paths that return to their start point are filled even when they are
  not flagged as closed, as Visio does.
- EMF images: the full-canvas glow-mask bitmaps that `emf2svg` emits are
  removed (draw.io showed them as dark smudges), and EMF files holding
  several PNGs are sent to the EMF renderer instead of keeping only
  the first PNG.

## [0.1.2] - 2026-11-06

### Added
- README setup instructions per assistant (Claude Code, Claude Desktop,
  GitHub Copilot in VS Code and the Copilot CLI, OpenAI Codex,
  other MCP clients), and which clients can't use a local server.
- Privacy policy (`PRIVACY.md`), linked from the README and the
  plugin listing.

### Fixed
- Running `visiodrawio` with no arguments prints the usage and exits
  with code 0 instead of 2, so winget's install validation, which runs
  the binary without arguments, no longer reports a failure. Unknown
  flags still exit with code 2.

## [0.1.1] - 2026-11-05

### Changed
- Release workflow: GitHub Actions updated (`actions/checkout`,
  `actions/setup-go` and `goreleaser/goreleaser-action` v7); re-running
  a release replaces assets that were already uploaded.

### Added
- Claude Code plugin icon.

## [0.1.0] - 2026-11-05

First public release.

### Added
- `visiodrawio` CLI that converts Microsoft Visio drawings (`.vsdx`) and
  templates (`.vstx`) into draw.io (`.drawio`) files.
- Multi-page output: one `<NAME>_ALL_PAGES.drawio` with one tab per
  Visio page, one `single-pages/<PAGE>.drawio` per page, and a CSV
  report per page in `report/`.
- Conversion of shapes and their masters (stencil shapes), groups,
  geometry (lines, arcs, Bezier, NURBS and polyline curves), text
  formatting, fills, gradients, fill patterns (as a draw.io hatch fill),
  line styles, arrowheads, rotation and flips, images and
  embedded `.vsdx` drawings.
- Visio layers become draw.io layers; layers hidden in Visio stay hidden
  and can be switched on. Background pages are added as locked layers.
- Fidelity details: shapes flip about their pin like in Visio, fills are
  painted before outlines (so symbols on filled areas stay visible), and
  text hidden with `HideText` stays hidden.
- Flags `-o`, `-force`, `-page`, `-list`, `-nofit`, `-splitlayers`,
  `-hidelayer`, `-bgeditable`, `-nobg` and `-version`.
- `visiodrawio mcp`: local MCP server over stdio with the tools
  `list_pages` and `convert`, for `.vsdx` and `.vstx` files.
  `VISIODRAWIO_ROOT` restricts it to a directory tree.
- Claude Code plugin (MCP server and skill) and a self-hosted plugin
  marketplace.
- Prebuilt binaries for Linux, macOS and Windows (amd64, arm64)
  via GoReleaser.

[Unreleased]: https://github.com/skhell/visio-to-drawio/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/skhell/visio-to-drawio/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/skhell/visio-to-drawio/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/skhell/visio-to-drawio/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/skhell/visio-to-drawio/releases/tag/v0.1.0
