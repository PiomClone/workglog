# worklog

[English](README.md) | [Русский](README.ru.md)

<p align="center">
  <img src="docs/icon.svg" width="96" alt="worklog logo">
</p>

![worklog flow](docs/worklog-flow.svg)

Локальный дневник работы без Git hooks и без изменений существующих репозиториев.

`worklog` сканирует Git-репозитории, складывает коммиты и ручные заметки в дневные Markdown-файлы, группирует записи по задачам вида `ABC-123` и умеет готовить summary для стендапа через Groq. Jira используется только для обогащения задач названием и статусом.

## Установка

```bash
git clone git@github.com:PiomClone/workglog.git
cd workglog
make install
```

Бинарь будет доступен как:

```text
~/.local/bin/worklog
```

Если `~/.local/bin` есть в `PATH`, можно запускать просто:

```bash
worklog
```

## Быстрый старт

```bash
worklog                          # интерактивный wizard
worklog scan                     # собрать коммиты
worklog add "ABC-123 что делал"  # ручная запись в «Что сделал»
worklog add --plan "ABC-123 завтра доделаю"
worklog add --blocker "ABC-123 жду доступ"
worklog report                   # Telegram-ready отчёт за сегодня
worklog report 2026-06-22        # отчёт за день
worklog standup                  # scan + вызов Groq за прошлый рабочий день
worklog standup --prompt         # только prompt, без вызова Groq
worklog stats                    # статистика задач/Groq
worklog version
```

## Основные флоу

### Ежедневный Telegram-отчёт без AI

```bash
worklog scan
worklog report
```

`report` не вызывает Groq. Он читает дневной Markdown, чистит время/repo/sha, дедупит одинаковые commit messages и форматирует текст для Telegram.

### Добавить ручные записи

```bash
worklog add "ABC-123 что сделал"
worklog add --task ABC-123 "ручная заметка без номера"
worklog add --last "ручная заметка к единственной задаче дня"
worklog add --plan "ABC-123 что буду делать"
worklog add --blocker "ABC-123 что блокирует"
```

В Web на странице отчёта можно выбрать задачу из dropdown: номер задачи добавится к ручной записи автоматически. Если ошибся, поправь файл дня вручную:

```bash
$EDITOR ~/.worklog/days/2026-06-23.md
worklog report 2026-06-23
```

### Пересобрать коммиты за день, не трогая ручные записи

```bash
worklog scan --since "2026-06-23 00:00" --force
worklog report 2026-06-23
```

`--force` игнорирует `state.json`, но дубли по SHA не добавляет. Ручные секции `Manual`, `Plan`, `Blockers` не перетираются.

### Перегенерить Groq summary после ручных правок

```bash
worklog standup --date 2026-06-23 --no-scan
cat ~/.worklog/summaries/2026-06-23.md
```

`summary` — это сохранённый результат Groq. `report` — локальный Telegram-ready отчёт без AI.

### Web UI в фоне

```bash
worklog web start
worklog web status
worklog web stop
worklog web restart
```

Обычный foreground-запуск тоже остаётся:

```bash
worklog web
```

## Хранилище

```text
~/.worklog/
  config.json
  state.json
  days/YYYY-MM-DD.md
  summaries/YYYY-MM-DD.md
```

Пример `~/.worklog/config.json`:

```json
{
  "scan_root": "/Users/avkorkin/prj",
  "exclude_dirs": [".idea", ".gradle", "node_modules", "vendor", "target", "build", "dist"],
  "exclude_paths": ["/Users/avkorkin/prj/study/workglog"],
  "groq_model": "llama-3.3-70b-versatile",
  "groq_base_url": "https://api.groq.com/openai/v1",
  "jira_url": "https://jira.example.com",
  "jira_user": ""
}
```

## Wizard

```bash
worklog
```

Запуск без аргументов открывает меню:

```text
1) Scan commits
2) Add manual note
3) Report
4) Generate standup with Groq
5) Show standup prompt only
6) Setup keys/config
7) Exit
```

Явные команды идут напрямую и wizard не открывают.

## Настройка

```bash
worklog setup
```

Основные пункты:

- `Scan root` — корень, где искать репозитории;
- `Groq` — модель, base URL и ключ;
- `Jira` — URL Jira и токен;
- `Show config` — показать текущую конфигурацию без вывода секретов.

Секреты хранятся в macOS Keychain, не в `config.json`.

## Keychain

Рекомендуемые service names:

```bash
security add-generic-password -a "$USER" -s groq-api-token -w "YOUR_GROQ_API_KEY" -U
security add-generic-password -a "$USER" -s jira-api-token -w "YOUR_JIRA_TOKEN" -U
```

Проверить наличие без вывода секретов:

```bash
security find-generic-password -a "$USER" -s groq-api-token >/dev/null && echo "Groq ok"
security find-generic-password -a "$USER" -s jira-api-token >/dev/null && echo "Jira ok"
```

Groq key читается из Keychain `groq-api-token`. Если ключа нет — будет простое summary без AI.

Для Jira:

1. Keychain `jira-api-token`
2. legacy Keychain `worklog-jira-api-token`

## Scan

```bash
worklog scan
```

По умолчанию:

- root: `/Users/avkorkin/prj` или `config.scan_root`;
- диапазон: начало сегодняшнего дня (`YYYY-MM-DD 00:00`);
- refs: все локальные refs/ветки по умолчанию; `--current-branch` — только текущий `HEAD`;
- автор: `git config --global user.email`, fallback на `user.name`;
- исключаемые имена папок: `.idea`, `.gradle`, `node_modules`, `vendor`, `target`, `build`, `dist`.

Опции:

```bash
worklog scan --root /path/to/projects
worklog scan --since "14 days ago"   # bootstrap
worklog scan --since "30 days ago"
worklog scan --exclude node_modules --exclude .gradle
worklog scan --exclude-path /Users/avkorkin/prj/study/workglog
worklog scan --all-authors
worklog scan --author user@example.com
worklog scan --quiet
worklog scan --current-branch
worklog scan --force             # игнорировать state.json, дубли по SHA не добавятся
```

Env override:

```bash
WORKLOG_SCAN_ROOT="/path/to/projects"
WORKLOG_EXCLUDE_DIRS=".idea,.gradle,node_modules,vendor,target,build,dist"
WORKLOG_EXCLUDE_PATHS="/Users/avkorkin/prj/study/workglog"
```

Progress выводится в stderr. Если не нужен:

```bash
worklog scan --quiet
```

## Ручные записи

```bash
worklog add "ABC-123 созвон по интеграции"
worklog add --date 2026-06-22 "ABC-123 ретро по задаче"
worklog add --type plan "ABC-123 завтра доделаю"
worklog add --type blocker "ABC-123 жду доступ"

Шорткаты:

```bash
worklog add --plan "ABC-123 завтра доделаю"
worklog add --blocker "ABC-123 жду доступ"
```

Типы пишутся в разные секции дневника:

```text
## Manual    -> Что сделал
## Plan      -> Что буду делать
## Blockers  -> Блокеры
```
```

## Отчёт

```bash
worklog report
worklog report 2026-06-22
```

Вывод готов для вставки в Telegram:

```text
2026-06-23

✅ Что сделал
• ABC-123 — заголовок из Jira [status]
  - commit/manual message

📌 Что буду делать
• plan note или «посмотрю, что есть в спринте»

⛔ Блокеры
• ABC-456
  - blocker note
```

Группировка идёт по regexp:

```text
\b[A-Z][A-Z0-9]+-\d+\b
```

Записи без номера задачи попадают в `untracked`.

## Standup

Готовый стендап через Groq:

```bash
worklog standup
```

Только prompt, без вызова Groq:

```bash
worklog standup --prompt
```

За конкретную дату:

```bash
worklog standup --date 2026-06-22
worklog standup --date 2026-06-22 --prompt
```

Без предварительного scan:

```bash
worklog standup --no-scan
```

`standup` по умолчанию берёт предыдущий рабочий день. В понедельник это пятница.

Summary сохраняется в:

```text
~/.worklog/summaries/YYYY-MM-DD.md
```

## Groq

Ключ берётся только из macOS Keychain service `groq-api-token`:

```bash
security add-generic-password -a "$USER" -s groq-api-token -w "YOUR_GROQ_API_KEY" -U
```

Модель и base URL:

```bash
export GROQ_MODEL="llama-3.3-70b-versatile"
export GROQ_BASE_URL="https://api.groq.com/openai/v1"
```

Если ключа нет, используется простое summary без AI.

## Prompt templates

По умолчанию используется встроенный prompt. Чтобы создать локальный шаблон:

```bash
worklog prompt init
worklog prompt path
worklog prompt print
```

Файл:

```text
~/.worklog/prompts/standup.md
```

Плейсхолдеры:

```text
{{date}}
{{done}}
{{planned}}
{{blockers}}
```

## Jira

Jira URL берётся из:

1. `WORKLOG_JIRA_URL`
2. `JIRA_URL`
3. `config.jira_url`

Jira user берётся из:

1. `WORKLOG_JIRA_USER`
2. `JIRA_USER`
3. `config.jira_user`

Auth:

- если user пустой — `Authorization: Bearer <token>`;
- если user задан — Basic auth `<user>:<token>`.

Jira нужна только для обогащения prompt/summary названием и статусом задачи. Если URL или token не заданы, `worklog` просто пропускает Jira enrichment.

## Web UI

Запуск локального web-интерфейса:

```bash
worklog web                         # foreground, держит консоль
worklog web --addr 127.0.0.1:8088
worklog web start                   # background через launchctl
worklog web stop
worklog web status
worklog web restart
```

По умолчанию слушает только localhost. Если биндить наружу, нужен token:

```bash
worklog web --addr 0.0.0.0:8088 --token "secret"
worklog web start --addr 0.0.0.0:8088 --token "secret"
```

Web UI использует то же хранилище, что и CLI:

- dashboard со статистикой задач и Groq лимитов;
- отчёт за дату с кнопкой копирования текста для Telegram;
- просмотр prompt;
- просмотр сохранённого Groq summary с кнопкой копирования;
- запуск scan, включая force rescan;
- добавление ручной заметки: сделал / буду делать / блокер;
- генерация standup;
- блокировка кнопок на время запроса;
- аккуратная setup/config page.

## Разработка

```bash
make fmt
make lint
make test
make check
make build
make install
```

## Версионирование и релиз

Версия лежит в `VERSION`.

Релиз:

```bash
git tag v$(cat VERSION)
git push origin main --tags
```

Push тега `v*.*.*` запускает GitHub Actions release workflow и публикует бинарники.

## Лицензия

MIT
