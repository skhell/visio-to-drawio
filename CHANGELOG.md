# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/skhell/visio-to-drawio/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/skhell/visio-to-drawio/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/skhell/visio-to-drawio/releases/tag/v0.1.0
