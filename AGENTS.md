# Thunder Compute CLI

`tnr` is the CLI for [Thunder Compute](https://www.thundercompute.com), a cloud GPU platform for AI/ML workloads.

## MCP Server

Thunder Compute provides a remote MCP server that lets AI coding agents manage GPU instances directly — no local install required.

**Connect it:**

| Agent | Setup |
|-------|-------|
| Claude Code | `/mcp add --transport http https://www.thundercompute.com/mcp` |
| Cursor | Add to `.cursor/mcp.json` (see config below) |
| Windsurf | Add to MCP settings (see config below) |
| Other agents | POST to endpoint with OAuth 2.0 |

```json
{
  "mcpServers": {
    "thunder-compute": {
      "type": "http",
      "url": "https://www.thundercompute.com/mcp"
    }
  }
}
```

No API tokens needed — authenticates via OAuth in the browser.

**37 tools:** instance management, command execution and background jobs, file reads, GPU specs/pricing/availability, snapshots, SSH keys, port forwarding, billing, and API tokens.

[Full MCP documentation](https://www.thundercompute.com/docs/guides/mcp-server)

## CLI Quick Reference

```
tnr login            # Authenticate with Thunder Compute
tnr create           # Create a GPU instance
tnr status           # List instances with status
tnr connect <id>     # SSH into an instance
tnr scp <src> <dst>  # Transfer files
tnr delete <id>      # Delete an instance
tnr port <id> <port> # Forward a port
```

Instance IDs are integers. Add `--json` for scripted/non-interactive usage.

## Release Notes

Every pull request that changes `cli/**` must add a Changie fragment under
`cli/changes/unreleased/`. The generated release-preparation pull request is the
only exception because it consumes all pending fragments. The same rule applies
to the CLI release workflows and Changie configuration.

Use the fragment kind that matches the shipped user impact. Changie derives the
SemVer bump automatically:

- `Fixed`, `Changed`, or `Security`: patch
- `Added` or `Deprecated`: minor
- `Breaking` or `Removed`: major
- `Internal`: no release bump

Example:

```sh
cd cli
changie new --kind Fixed \
  --body "Preserve the login link when the terminal is resized."
```

Tests, refactors, and build-only changes still require an explicit `Internal`
fragment:

```sh
cd cli
changie new --kind Internal \
  --body "Adds test coverage without changing shipped behavior."
```

Write summaries for users and describe observable impact rather than
implementation details. Do not update `VERSION` in ordinary feature or fix pull
requests. `make prepare-cli-release` asks Changie to select the highest pending
bump and generate the versioned notes and changelog. See
[RELEASING.md](RELEASING.md) for the complete process.

## Interactive cancellation

Cancellation before submission is silent: clear loading indicators and menus without printing cancellation messages, errors, or usage. Status and snapshot monitoring preserve their final tables. Once a mutation is submitted, wait for and report its actual result; explain when work is still in progress instead of claiming it was cancelled. Retain actionable notices when an interruption leaves partial work or an uncertain outcome.

Q/Esc menu cancellation exits with code 0; Ctrl+C in menus and SIGINT/SIGTERM delivered to the local process exit with code 130 before submission. Use `tui.NewProgram` and `tui.RunProgram` so signals follow the model's normal cleanup path. Submitted mutations use `RunProgress`, retain signal handling until completion, and return the operation's actual result.

## Development

- **Language:** Go 1.25
- **Build:** from the monorepo root, run `make build-cli`
- **Test:** from the monorepo root, run `make test-cli`

## Links

- [Documentation](https://www.thundercompute.com/docs)
- [CLI Reference](https://www.thundercompute.com/docs/cli-reference)
- [API Reference](https://www.thundercompute.com/docs/api-reference)
- [Quick Start](https://www.thundercompute.com/docs/quickstart)
