# Security Policy

## Supported versions

Only the latest release receives security fixes.

## Reporting a vulnerability

Please **do not** open a public issue. Use GitHub's private reporting:
<https://github.com/skhell/visio-to-drawio/security/advisories/new>

Include the affected version, a description, and if possible a minimal
`.vsdx` file without any confidential data that reproduces the problem.
You can expect an acknowledgement within 7 days and a fix or mitigation
plan within 30 days.

## Scope and threat model

visiodrawio parses untrusted `.vsdx`/`.vstx`files (ZIP archives
containing XML and images).
Relevant issues include crashes or excessive memory/CPU use
on crafted input (e.g. zip bombs), path traversal in archive entries,
and XML handling flaws.

The MCP server (`visiodrawio mcp`) runs locally over stdio with the
privileges of the user, and reads and writes files at paths supplied by
the AI client. Set `VISIODRAWIO_ROOT` to restrict it to a directory
tree. It makes no network connections.