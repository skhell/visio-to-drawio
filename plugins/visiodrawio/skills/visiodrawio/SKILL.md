---
name: visiodrawio
description: Convert Microsoft Visio .vsdx and .vstx diagrams to editable draw.io files, locally via CLI or via an MCP server and a skill.
---

# Visio to draw.io conversion

Use the `visiodrawio` MCP server (tools `list_pages` and `convert`).
If the MCP tools are unavailable, use the CLI: `visiodrawio file.vsdx -o out`.
If the binary is missing, tell the user to install it from
https://github.com/skhell/visio-to-drawio/releases or `go install github.com/skhell/visio-to-drawio/cmd/visiodrawio@latest`.

## Workflow

1. Confirm the input is a `.vsdx` or `.vstx` file. Legacy `.vsd` is unsupported: ask the user to open it in Visio and Save As `.vsdx`.
2. Call `list_pages` to see the pages. Use `convert` with `page` set if the user wants only one.
3. Call `convert` with `path` (and optionally `output_dir`). It refuses to overwrite existing output unless `force` is true; only set `force` when the user agrees to overwrite.
4. Report the output: `<name>_ALL_PAGES.drawio` (all pages), `single-pages/*.drawio`, and `report/*.csv`.
5. Read the CSV report or stderr warnings and tell the user about shapes that were approximated.

## Options

`no_fit`, `split_layers`, `hide_layers`, `background_editable` and `no_background` map to the CLI flags `-nofit`, `-splitlayers`, `-hidelayer`, `-bgeditable` and `-nobg`.
