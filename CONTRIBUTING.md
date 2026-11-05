# Contributing

Thanks for helping! Bug reports, sample files that convert badly, and pull requests are all welcome.

## Reporting bugs

Open an issue using the bug template. A minimal `.vsdx` that reproduces the problem (with no confidential content) helps most. Include the output of `visiodrawio -version`.

## Pull requests

1. Fork and create a branch.
2. Keep changes focused; add or update tests when behavior changes.
3. Run `gofmt -l .`, `go vet ./...` and `go test ./...` before pushing.
4. Use [Conventional Commits](https://www.conventionalcommits.org) style messages (`fix:`, `feat:`, `docs:` ...); they drive the changelog.

## Layout

- `cmd/visiodrawio`: CLI and MCP server
- `internal/converter`: the Visio → draw.io conversion
- `plugins/visiodrawio`: Claude Code plugin (skill + MCP config)

Do not commit confidential diagrams. Only add test files you have the right to share.

By contributing you agree that your contributions are licensed under the [MIT License](LICENSE). Be respectful; the [Contributor Covenant](https://www.contributor-covenant.org/version/2/1/code_of_conduct/) applies to all project spaces.
