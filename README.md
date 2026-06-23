# worklog

<p align="center">
  <img src="docs/icon.svg" width="96" alt="worklog logo">
</p>

![worklog flow](docs/worklog-flow.svg)

Local work diary without Git hooks and without changing existing repositories.

`worklog` scans Git repositories, writes commits and manual notes to daily Markdown files, groups entries by task key like `ABC-123`, and can prepare a standup summary with Grok/xAI.

## Install

```bash
git clone git@github.com:PiomClone/workglog.git
cd workglog
make install
```

The binary is linked to:

```text
~/.local/bin/worklog
```

## Usage

```bash
worklog                          # interactive wizard
worklog scan
worklog add "ABC-123 what I did"
worklog report
worklog report 2026-06-22
worklog standup                  # previous workday, scan + Grok summary
worklog standup --prompt         # previous workday prompt only
worklog summarize --prompt
worklog summarize --ai --model grok-4
worklog version
```

## Storage

```text
~/.worklog/
  config.json
  state.json
  days/YYYY-MM-DD.md
  summaries/YYYY-MM-DD.md
```

Example config:

```json
{
  "scan_root": "/Users/avkorkin/prj",
  "ai_model": "grok-4",
  "xai_base_url": "https://api.x.ai/v1",
  "jira_url": "https://jira.example.com",
  "jira_user": "user@example.com"
}
```

## Commands

### Interactive wizard

```bash
worklog
```

The wizard lets you choose scan, add note, report, standup summary, standup prompt, or setup. Explicit commands bypass the wizard.

### Scan commits

```bash
worklog scan
```

Defaults:

- root: `/Users/avkorkin/prj`
- range: `--since "14 days ago"`
- author: global `git config user.email`, fallback to `user.name`

Options:

```bash
worklog scan --root /path/to/projects
worklog scan --since "30 days ago"
worklog scan --all-authors
worklog scan --author user@example.com
```

### Add manual entry

```bash
worklog add "ABC-123 call about integration"
worklog add --date 2026-06-22 "ABC-123 retro note"
```

### Report

```bash
worklog report
worklog report 2026-06-22
```

Entries are grouped by:

```text
\b[A-Z][A-Z0-9]+-\d+\b
```

Entries without a task key go to `untracked`.

### Standup summary

Print prompt only:

```bash
worklog summarize --prompt
```

Call Grok/xAI via environment variable:

```bash
export XAI_API_KEY="..."
export WORKLOG_AI_MODEL="grok-4"
export WORKLOG_XAI_BASE_URL="https://api.x.ai/v1"
worklog summarize --ai
```

Or store the key in macOS Keychain:

```bash
security add-generic-password -a "$USER" -s xai-api-token -w "YOUR_XAI_API_KEY" -U
worklog summarize --ai
```

`XAI_API_KEY` has priority. If it is empty, `worklog` checks Keychain services `xai-api-token`, `grok.x.ai-api-token`, then legacy `worklog-xai-api-key`.

`WORKLOG_XAI_BASE_URL` has priority over config. Default: `https://api.x.ai/v1`.

Jira token is read from Keychain service `jira-api-token`, with legacy fallback `worklog-jira-api-token`.

The summary is saved to:

```text
~/.worklog/summaries/YYYY-MM-DD.md
```

## Development

```bash
make fmt
make test
make build
make install
```

## Versioning

Version is stored in `VERSION`.

Create a release:

```bash
git tag v$(cat VERSION)
git push origin main --tags
```

Pushing a `v*.*.*` tag runs the release workflow and publishes binaries.

## License

MIT
