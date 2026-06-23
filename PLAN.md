# Worklog на Go + AI summary

## Цель

Сделать локальный дневник работы без Git hooks и без изменений существующих репозиториев.

Утилита сканирует Git-репозитории, собирает коммиты и ручные записи в общий дневник, а затем может подготовить summary для standup через Grok/xAI.

## Почему не hooks

- Не конфликтует с `core.hooksPath`, `.opencode/githooks`, `.husky`, `.git/hooks`.
- Работает одинаково для IDEA, Codex, OpenCode и CLI.
- Не требует менять `.git/config` и файлы проектов.
- Можно запускать вручную или по расписанию через `launchd`.

## Почему Go

- Один бинарь без Python runtime/shebang.
- Быстрый старт.
- Просто поставить в `~/.local/bin/worklog`.
- Удобно дергать `git` как subprocess.
- xAI API вызывается обычным HTTP JSON без SDK.

## Команды

```bash
worklog scan
worklog add "ABC-123 что делал"
worklog report
worklog report 2026-06-22
worklog summarize --prompt
worklog summarize --ai
worklog summarize --ai --model grok-4
```

## Хранилище

```text
~/.worklog/
  state.json
  days/
    2026-06-23.md
  summaries/
    2026-06-23.md
```

## Формат дневника

```md
# 2026-06-23

## Commits

- 10:51 `sbp-client-api` `883a1820f678` TRANSFERS-15587 Исключена работа с просроченными сертификатами шифрования

## Manual

- 12:30 ABC-123 разбирал интеграцию
```

## Scan

`worklog scan`:

- сканирует `/Users/avkorkin/prj` по умолчанию;
- ищет Git-репозитории;
- выполняет `git log --since ... --format ... --no-merges`;
- фильтрует по текущему git user/email, если не указан `--all-authors`;
- пишет новые коммиты в дневной markdown;
- хранит просмотренные SHA в `state.json`, чтобы не дублировать записи.

Опции:

```bash
worklog scan --root /path/to/projects
worklog scan --since "30 days ago"
worklog scan --all-authors
```

## Manual entries

`worklog add` добавляет ручную запись за сегодня:

```bash
worklog add "ABC-123 созвон по интеграции"
```

Опционально:

```bash
worklog add --date 2026-06-22 "ABC-123 ретро по задаче"
```

## Report

`worklog report` группирует записи по regexp:

```text
\b[A-Z][A-Z0-9]+-\d+\b
```

Записи без номера задачи попадают в `untracked`.

## AI summarize

`worklog summarize --prompt`:

- читает дневник за дату;
- группирует по задачам;
- формирует prompt для AI;
- печатает prompt в stdout.

`worklog summarize --ai`:

- берёт `XAI_API_KEY` из env;
- использует модель из `--model` или `WORKLOG_AI_MODEL`;
- отправляет prompt в xAI/Grok;
- сохраняет результат в `~/.worklog/summaries/YYYY-MM-DD.md`.

Env:

```bash
export XAI_API_KEY="..."
export WORKLOG_AI_MODEL="grok-4"
```

Ключ не хранить в репозитории.

## Формат standup summary

```md
# Standup 2026-06-23

## Что сделал

- TRANSFERS-15587 — исключил работу с просроченными сертификатами шифрования, проверил поведение в sbp-client-api.

## В процессе

- ABC-123 — продолжаю анализ интеграции.

## Блокеры

- Нет.
```

## Автоматизация

Через `launchd` запускать scan раз в 10 минут:

```bash
worklog scan
```

Summary лучше запускать вручную перед standup:

```bash
worklog summarize --ai
```

## Этапы реализации

1. Переписать текущий `worklog.py` на Go.
2. Сохранить совместимый формат `~/.worklog`.
3. Реализовать команды `scan`, `add`, `report`.
4. Добавить `summarize --prompt`.
5. Добавить `summarize --ai` через xAI API.
6. Собрать бинарь и установить в `~/.local/bin/worklog`.
7. После проверки удалить или оставить Python-прототип как reference.
