# Agent instructions

Go CLI converting Visio `.vsdx` to draw.io `.drawio`; also an MCP server (`visiodrawio mcp`).

- Build/test: `go build ./cmd/visiodrawio && go vet ./... && go test ./...`
- Code: `cmd/visiodrawio` (CLI, MCP), `internal/converter` (conversion).
- In MCP mode stdout carries protocol messages only; log to stderr, never `fmt.Println`.
- To convert a diagram: `visiodrawio file.vsdx -o out` (list pages with `-list`).
