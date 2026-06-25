# workglog

[English](README.md) | [Русский](README.ru.md)

<p align="center">
  <img src="docs/icon.svg" width="96" alt="workglog logo">
</p>

![workglog flow](docs/workglog-flow.svg)

Local work diary without Git hooks and without changing existing repositories.

`workglog` scans Git repositories, writes commits and manual notes to daily Markdown files, groups entries by task key like `ABC-123`, and can prepare a standup summary with Groq.

## Install

```bash
git clone git@github.com:PiomClone/workglog.git
cd workglog
make install
```

The binary is linked to:

```text
~/.local/bin/workglog
```

## Usage

```bash
workglog                          # interactive wizard
workglog scan
workglog add "ABC-123 what I did"
workglog add --task ABC-123 "manual note without task key"
workglog add --last "manual note for the only task of the day"
workglog add --plan "ABC-123 next step"
workglog add --blocker "ABC-123 waiting for access"
workglog report
workglog report 2026-06-22
workglog standup                  # previous workday, scan + Groq summary
workglog standup --prompt         # previous workday prompt only
workglog summarize --prompt
workglog summarize --ai --model llama-3.3-70b-versatile
workglog stats                    # task/Groq statistics
workglog version
```

## Common flows

### Daily Telegram report without AI

```bash
workglog scan
workglog report
```

`report` does not call Groq. It reads the daily Markdown file, removes time/repo/sha noise, deduplicates identical commit messages, and formats text for Telegram.

### Add manual entries

```bash
workglog add "ABC-123 what I did"
workglog add --task ABC-123 "manual note without task key"
workglog add --last "manual note for the only task of the day"
workglog add --plan "ABC-123 next step"
workglog add --blocker "ABC-123 waiting for access"
```

In Web report, choose a task from the dropdown: the task key is prepended automatically. If a manual entry is wrong, edit the day file directly:

```bash
$EDITOR ~/.workglog/days/2026-06-23.md
workglog report 2026-06-23
```

### Rescan commits for a day without touching manual entries

```bash
workglog scan --since "2026-06-23 00:00" --force
workglog report 2026-06-23
```

`--force` ignores `state.json`, but SHA deduplication still prevents duplicate commits. Manual sections `Manual`, `Plan`, and `Blockers` are preserved.

### Regenerate Groq summary after manual edits

```bash
workglog standup --date 2026-06-23 --no-scan
cat ~/.workglog/summaries/2026-06-23.md
```

`summary` is the saved Groq result. `report` is a local Telegram-ready report without AI.

### Web UI in background

```bash
workglog web start
workglog web status
workglog web stop
workglog web restart
```

Foreground mode is still available:

```bash
workglog web
```

## Storage

Default storage is `~/.workglog`. If it does not exist and legacy `~/.worklog` exists, `workglog` uses the legacy directory to avoid data loss.

```text
~/.workglog/
  config.json
  state.json
  days/YYYY-MM-DD.md
  summaries/YYYY-MM-DD.md
```

Example config:

```json
{
  "scan_root": "/Users/avkorkin/prj",
  "groq_model": "llama-3.3-70b-versatile",
  "groq_base_url": "https://api.groq.com/openai/v1",
  "jira_url": "https://jira.example.com",
  "jira_user": "user@example.com"
}
```

## Commands

### Interactive wizard

```bash
workglog
```

The wizard lets you choose scan, add note, report, standup summary, standup prompt, or setup. Explicit commands bypass the wizard.

### Scan commits

```bash
workglog scan
```

Defaults:

- root: `/Users/avkorkin/prj`
- range: start of today (`YYYY-MM-DD 00:00`)
- refs: all local refs/branches by default; use `--current-branch` for current HEAD only
- author: global `git config user.email`, fallback to `user.name`

Options:

```bash
workglog scan --root /path/to/projects
workglog scan --since "14 days ago"   # bootstrap
workglog scan --since "30 days ago"
workglog scan --all-authors
workglog scan --author user@example.com
workglog scan --force             # ignore state.json, deduplicate by SHA in day files
```

### Add manual entry

```bash
workglog add "ABC-123 call about integration"
workglog add --date 2026-06-22 "ABC-123 retro note"
workglog add --type plan "ABC-123 next step"
workglog add --type blocker "ABC-123 waiting for access"
workglog add --plan "ABC-123 next step"
workglog add --blocker "ABC-123 waiting for access"
```

### Report

```bash
workglog report
workglog report 2026-06-22
```

Entries are grouped by:

```text
\b[A-Z][A-Z0-9]+-\d+\b
```

Entries without a task key go to `untracked`.

### Standup summary

Prompt only:

```bash
workglog summarize --prompt
workglog standup --prompt --no-scan
```

Generate with Groq:

```bash
security add-generic-password -a "$USER" -s groq-api-token -w "YOUR_GROQ_API_KEY" -U
workglog summarize --ai
```

If Groq key is missing, `workglog` falls back to a simple local summary.

Prompt template override:

```bash
workglog prompt init
workglog prompt path
workglog prompt print
```

Template file:

```text
~/.workglog/prompts/standup.md
```

Placeholders: `{{date}}`, `{{done}}`, `{{planned}}`, `{{blockers}}`.

The summary is saved to:

```text
~/.workglog/summaries/YYYY-MM-DD.md
```

## Web UI

Run local web interface:

```bash
workglog web                         # foreground
workglog web --addr 127.0.0.1:8088
workglog web start                   # background via launchctl
workglog web stop
workglog web status
workglog web restart
```

By default it listens on localhost only. If you bind to a public address, pass a token:

```bash
workglog web --addr 0.0.0.0:8088 --token "secret"
workglog web start --addr 0.0.0.0:8088 --token "secret"
```

Current Web UI supports the same storage as CLI:

- dashboard with task and Groq limit statistics;
- report by date with copy button for Telegram text;
- prompt preview;
- saved Groq summary view with copy button;
- scan action, including force rescan;
- add manual note: done / plan / blocker;
- generate standup;
- buttons disabled while requests are running;
- clean setup/config page.

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
