# Privacy Policy

Effective date: 2026-11-05

This policy covers `visiodrawio`: the command-line tool, its MCP server
(`visiodrawio mcp`) and the Claude Code plugin published in this
repository.

## Summary

visiodrawio runs entirely on your own computer.
It does not collect, send or keep any data, and the developer receives
nothing.

## What the software processes

- It reads the Visio files (`.vsdx`, `.vstx`) that you, or the AI
  assistant you use, point it to.
- It writes the converted `.drawio` files and the per-page CSV reports
  to the folder you choose on your own disk. Diagrams can contain
  personal data, such as names on a floor plan. That text is copied into
  the output files, because that is what a conversion does.
- With the MCP server, the tool results (page names, the list of written
  files) are returned to your AI client.

## What the software does not do

- It makes no network connections: no telemetry, analytics, crash
  reports or update checks.
- It does not send your files or their contents to the developer or to
  any other service.
- It keeps no data of its own, such as logs, caches or accounts.
  The only files it creates are the outputs you asked for.

## Data handled by others

- **Your AI client** (for example Claude, Copilot or Codex) receives the
  tool results and whatever you share in the conversation. That data is
  governed by the client provider's privacy policy, not by this one.
- **Download channels** (GitHub Releases, Homebrew, Scoop, winget, the
  Claude plugin directory) may log downloads under their own privacy
  policies. The developer does not receive personal data from them.

## Children

visiodrawio is a general-purpose tool and is not directed at
children under 18.

## Changes

Changes to this policy are published in this file, and the effective
date above is updated. The full history is in the repository's
commit log.

## Contact

Questions: open an issue at <https://github.com/skhell/visio-to-drawio/issues>.
Security problems: follow [SECURITY.md](SECURITY.md).
