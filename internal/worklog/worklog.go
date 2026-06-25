package worklog

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultRoot = "/Users/avkorkin/prj"

var defaultExcludeDirs = []string{".idea", ".gradle", "node_modules", "vendor", "target", "build", "dist"}

var taskRE = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)

var rawGroqStatRE = regexp.MustCompile(`x-ratelimit-([a-z-]+): ([^·]+)`)

var (
	version = "dev"
	commit  = "none"
	builtAt = "unknown"
)

type Config struct {
	ScanRoot     string   `json:"scan_root,omitempty"`
	ExcludeDirs  []string `json:"exclude_dirs,omitempty"`
	ExcludePaths []string `json:"exclude_paths,omitempty"`
	GroqModel    string   `json:"groq_model,omitempty"`
	GroqBaseURL  string   `json:"groq_base_url,omitempty"`
	JiraURL      string   `json:"jira_url,omitempty"`
	JiraUser     string   `json:"jira_user,omitempty"`
}

type State struct {
	Seen      map[string][]string `json:"seen"`
	LastScan  string              `json:"last_scan,omitempty"`
	GroqStats string              `json:"groq_stats,omitempty"`
}

type WorklogStats struct {
	TodayEntries int
	TodayTasks   int
	SummaryFiles int
	LastScan     string
	GroqStats    string
}

type Commit struct {
	Repo    string
	SHA     string
	Unix    int64
	Subject string
}

type Entry struct {
	Kind string
	Text string
}

const (
	KindDone    = "done"
	KindPlan    = "plan"
	KindBlocker = "blocker"
)

func Home() string {
	if v := os.Getenv("WORKGLOG_HOME"); v != "" {
		return ExpandHome(v)
	}
	if v := os.Getenv("WORKLOG_HOME"); v != "" {
		return ExpandHome(v)
	}
	newHome := filepath.Join(userHome(), ".workglog")
	legacyHome := filepath.Join(userHome(), ".worklog")
	if _, err := os.Stat(newHome); err == nil {
		return newHome
	}
	if _, err := os.Stat(legacyHome); err == nil {
		return legacyHome
	}
	return newHome
}

func ConfigPath(home string) string        { return filepath.Join(home, "config.json") }
func DayPath(home, date string) string     { return filepath.Join(home, "days", date+".md") }
func SummaryPath(home, date string) string { return filepath.Join(home, "summaries", date+".md") }
func PromptPath(home, name string) string  { return filepath.Join(home, "prompts", name+".md") }
func EnvDefault(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	if strings.HasPrefix(k, "WORKGLOG_") {
		if v := os.Getenv("WORKLOG_" + strings.TrimPrefix(k, "WORKGLOG_")); v != "" {
			return v
		}
	}
	return fallback
}

func DefaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
func userHome() string { h, _ := os.UserHomeDir(); return h }
func ExpandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(userHome(), p[2:])
	}
	return p
}

func StartOfToday() string {
	return time.Now().Format("2006-01-02") + " 00:00"
}

func TodayOrArg(args []string) string {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return args[0]
	}
	return time.Now().Format("2006-01-02")
}

func LoadConfig(home string) Config {
	b, err := os.ReadFile(ConfigPath(home))
	if err != nil {
		return Config{}
	}
	var cfg Config
	_ = json.Unmarshal(b, &cfg)
	return cfg
}

func SaveConfig(home string, cfg Config) error {
	if err := os.MkdirAll(home, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(home), append(b, '\n'), 0600)
}

func printConfig(home string, cfg Config) {
	fmt.Printf("config: %s\n", ConfigPath(home))
	fmt.Printf("scan_root: %s\n", DefaultIfEmpty(cfg.ScanRoot, defaultRoot))
	fmt.Printf("exclude_dirs: %s\n", strings.Join(EffectiveExcludeDirs(cfg, nil), ","))
	fmt.Printf("exclude_paths: %s\n", strings.Join(EffectiveExcludePaths(cfg, nil), ","))
	fmt.Printf("jira_url: %s\n", JiraURL(cfg))
	fmt.Printf("jira_user: %s\n", JiraUser(cfg))
	fmt.Printf("groq_model: %s\n", GroqModel(cfg))
	fmt.Printf("groq_base_url: %s\n", GroqBaseURL(cfg))
	fmt.Printf("groq_key: %s\n", configured(GroqAPIKey() != ""))
	fmt.Printf("jira_token: %s\n", configured(JiraAPIToken() != ""))
}

func configured(v bool) string {
	if v {
		return "configured"
	}
	return "missing"
}

func ask(r *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	text, _ := r.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return def
	}
	return text
}

func PreviousWorkday(t time.Time) time.Time {
	d := t.AddDate(0, 0, -1)
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, SplitCSV(value)...)
	return nil
}

func EffectiveExcludeDirs(cfg Config, cli []string) []string {
	if len(cli) > 0 {
		return NormalizeList(cli)
	}
	if env := os.Getenv("WORKGLOG_EXCLUDE_DIRS"); env != "" {
		return SplitCSV(env)
	}
	if len(cfg.ExcludeDirs) > 0 {
		return NormalizeList(cfg.ExcludeDirs)
	}
	return defaultExcludeDirs
}

func EffectiveExcludePaths(cfg Config, cli []string) []string {
	if len(cli) > 0 {
		return normalizePaths(cli)
	}
	if env := os.Getenv("WORKGLOG_EXCLUDE_PATHS"); env != "" {
		return normalizePaths(SplitCSV(env))
	}
	return normalizePaths(cfg.ExcludePaths)
}

func SplitCSV(value string) []string {
	parts := strings.Split(value, ",")
	return NormalizeList(parts)
}

func NormalizeList(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func normalizePaths(values []string) []string {
	list := NormalizeList(values)
	var result []string
	for _, value := range list {
		path, err := filepath.Abs(ExpandHome(value))
		if err != nil {
			continue
		}
		result = append(result, filepath.Clean(path))
	}
	return result
}

func isExcludedPath(path string, excludePaths []string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	for _, excluded := range excludePaths {
		if abs == excluded || strings.HasPrefix(abs, excluded+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func FindRepos(root string, excludeDirs []string, excludePaths []string) ([]string, error) {
	var repos []string
	skip := map[string]bool{}
	for _, dir := range excludeDirs {
		if dir != "" {
			skip[dir] = true
		}
	}
	err := filepath.WalkDir(ExpandHome(root), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if isExcludedPath(path, excludePaths) {
			return filepath.SkipDir
		}
		name := d.Name()
		if name == ".git" {
			repos = append(repos, filepath.Dir(path))
			return filepath.SkipDir
		}
		if skip[name] {
			return filepath.SkipDir
		}
		return nil
	})
	return repos, err
}

func ReadCommits(repo, since, author string, allRefs bool) ([]Commit, error) {
	args := []string{"log", "--since=" + since, "--format=%H%x1f%at%x1f%s", "--no-merges"}
	if allRefs {
		args = append(args, "--all")
	}
	if author != "" {
		args = append(args, "--author="+author)
	}
	out, err := git(repo, args...)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\x1f", 3)
		if len(parts) != 3 {
			continue
		}
		ts, _ := strconv.ParseInt(parts[1], 10, 64)
		sha := parts[0]
		if len(sha) > 12 {
			sha = sha[:12]
		}
		commits = append(commits, Commit{Repo: filepath.Base(repo), SHA: sha, Unix: ts, Subject: parts[2]})
	}
	sort.Slice(commits, func(i, j int) bool { return commits[i].Unix < commits[j].Unix })
	return commits, nil
}

func git(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}

func GitGlobal(key string) string {
	cmd := exec.Command("git", "config", "--global", "--get", key)
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func LoadState(home string) (State, error) {
	path := filepath.Join(home, "state.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Seen: map[string][]string{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	if s.Seen == nil {
		s.Seen = map[string][]string{}
	}
	return s, nil
}

func SaveState(home string, state State) error {
	if err := os.MkdirAll(home, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, "state.json"), append(b, '\n'), 0644)
}

func DayHasSHA(home, date, sha string) bool {
	if sha == "" {
		return false
	}
	b, err := os.ReadFile(DayPath(home, date))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "`"+sha+"`")
}

func AppendUnderSection(path, date, section, line string) error {
	if err := ensureDay(path, date); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(b)
	header := "## " + section
	if !strings.Contains(text, header) {
		text = strings.TrimRight(text, "\n") + "\n\n" + header + "\n"
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	idx := -1
	for i, l := range lines {
		if l == header {
			idx = i + 1
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("section not found: %s", section)
	}
	for idx < len(lines) && strings.TrimSpace(lines[idx]) == "" {
		idx++
	}
	for idx < len(lines) && !strings.HasPrefix(lines[idx], "## ") {
		idx++
	}
	lines = append(lines[:idx], append([]string{line}, lines[idx:]...)...)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func ensureDay(path, date string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("# "+date+"\n\n## Commits\n\n## Manual\n\n## Plan\n\n## Blockers\n"), 0644)
}

func ReadItems(path string) ([]string, error) {
	entries, err := ReadEntries(path)
	if err != nil {
		return nil, err
	}
	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		items = append(items, entry.Text)
	}
	return items, nil
}

func ReadEntries(path string) ([]Entry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	kind := KindDone
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			kind = sectionKind(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			entries = append(entries, Entry{Kind: kind, Text: strings.TrimPrefix(trimmed, "- ")})
		}
	}
	return entries, nil
}

func sectionKind(section string) string {
	s := strings.ToLower(strings.TrimSpace(section))
	switch {
	case strings.Contains(s, "block") || strings.Contains(s, "блок"):
		return KindBlocker
	case strings.Contains(s, "plan") || strings.Contains(s, "буд") || strings.Contains(s, "next"):
		return KindPlan
	default:
		return KindDone
	}
}

func EntryTexts(entries []Entry, kind string) []string {
	var result []string
	for _, entry := range entries {
		if entry.Kind == kind {
			result = append(result, entry.Text)
		}
	}
	return result
}

type TaskGroup struct {
	Task   string
	Title  string
	Status string
	Items  []string
}

func TaskKey(text string) string {
	if m := taskRE.FindString(text); m != "" {
		return m
	}
	return ""
}

func TaskKeysFromItems(items []string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, item := range items {
		key := TaskKey(item)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func PrefixTask(text, task string) string {
	text = strings.TrimSpace(text)
	task = strings.TrimSpace(task)
	if task == "" || text == "" || TaskKey(text) != "" {
		return text
	}
	return task + " " + text
}

func GroupByTask(items []string) map[string][]string {
	groups := map[string][]string{}
	for _, item := range items {
		task := TaskKey(item)
		if task == "" {
			task = "untracked"
		}
		groups[task] = append(groups[task], item)
	}
	for task := range groups {
		sort.SliceStable(groups[task], func(i, j int) bool {
			return itemSortMinute(groups[task][i]) < itemSortMinute(groups[task][j])
		})
	}
	return groups
}

func OrderedGroups(groups map[string][]string) []TaskGroup {
	return OrderedGroupsWithJira(groups, nil)
}

func OrderedGroupsWithJira(groups map[string][]string, jira map[string]JiraIssue) []TaskGroup {
	result := make([]TaskGroup, 0, len(groups))
	for task, items := range groups {
		group := TaskGroup{Task: task, Title: task, Items: items}
		if issue, ok := jira[task]; ok {
			group.Title = FormatTaskLabel(task, issue)
			group.Status = issue.Status
		}
		result = append(result, group)
	}
	sort.SliceStable(result, func(i, j int) bool {
		iMin := groupSortMinute(result[i].Items)
		jMin := groupSortMinute(result[j].Items)
		if iMin == jMin {
			return result[i].Task < result[j].Task
		}
		return iMin < jMin
	})
	return result
}

func FormatTaskLabel(key string, issue JiraIssue) string {
	label := key
	if issue.Summary != "" {
		label += " — " + issue.Summary
	}
	if issue.Status != "" {
		label += " [" + issue.Status + "]"
	}
	return label
}

func groupSortMinute(items []string) int {
	if len(items) == 0 {
		return 24 * 60 * 2
	}
	return itemSortMinute(items[0])
}

func itemSortMinute(item string) int {
	if len(item) < 5 || item[2] != ':' {
		return 24 * 60 * 2
	}
	hour, errH := strconv.Atoi(item[:2])
	minute, errM := strconv.Atoi(item[3:5])
	if errH != nil || errM != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 24 * 60 * 2
	}
	return hour*60 + minute
}

func printGroups(groups map[string][]string) {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("\n## %s\n", k)
		for _, item := range groups[k] {
			fmt.Printf("- %s\n", item)
		}
	}
}

func SummarizeWithAIOrSimple(cfg Config, model, date string, done map[string][]string, jira map[string]JiraIssue, planned []TaskGroup, blockers map[string][]string, prompt string) (string, error) {
	key := GroqAPIKey()
	if key == "" {
		return SimpleStandup(date, done, jira, planned, blockers), nil
	}
	return callGroq(cfg, key, model, prompt)
}

func SimpleStandup(date string, done map[string][]string, jira map[string]JiraIssue, planned []TaskGroup, blockers map[string][]string) string {
	var b strings.Builder
	b.WriteString("# Standup " + date + "\n\n")
	b.WriteString("## Что сделал\n\n")
	for _, group := range OrderedGroupsWithJira(done, jira) {
		b.WriteString("- " + group.Title + "\n")
		for _, item := range group.Items {
			b.WriteString("  - " + item + "\n")
		}
	}
	planned = EnsurePlannedFallback(planned)
	if len(planned) > 0 {
		b.WriteString("\n## Что буду делать\n\n")
		for _, group := range planned {
			if group.Title != "" {
				b.WriteString("- " + group.Title + "\n")
			}
			for _, item := range group.Items {
				if group.Title == "" {
					b.WriteString("- " + item + "\n")
				} else {
					b.WriteString("  - " + item + "\n")
				}
			}
		}
	}
	if len(blockers) > 0 {
		b.WriteString("\n## Блокеры\n\n")
		for _, group := range OrderedGroupsWithJira(blockers, jira) {
			b.WriteString("- " + group.Title + "\n")
			for _, item := range group.Items {
				b.WriteString("  - " + item + "\n")
			}
		}
	}
	return b.String()
}

const DefaultStandupPromptTemplate = `Сделай краткий отчёт для вставки в Telegram на русском языке.

Формат вывода строго такой:

✅ Что сделал
• TASK-123 — заголовок задачи
  - короткий результат без времени, repo, sha и технического шума

📌 Что буду делать
• TASK-123 — заголовок задачи
  - короткий план по этой задаче

⛔ Блокеры
• TASK-123 — заголовок задачи
  - только явно указанный блокер

Правила:
- Не используй markdown-заголовки ## и ###.
- Не добавляй дату.
- Не добавляй секцию блокеры, если их нет во входных данных.
- Не выдумывай планы, статусы и блокеры.
- В секции "Что буду делать" сохраняй привязку к задаче: если во входе есть TASK-123 и заголовок Jira, выводи пункт под этой задачей, а не отдельным безымянным планом.
- Если в секции "Что буду делать" входной пункт без TASK-123/заголовка, выводи его одной строкой вида: • посмотрю, что есть в спринте; не добавляй фразы вроде "нет информации о конкретных задачах".
- Объединяй дублирующиеся commit messages.
- Убирай время, repo, sha и лишние технические детали.
- Сохраняй номер задачи и человекочитаемый заголовок из Jira.
- Исправляй грамматические ошибки.

Входные данные:

Дата: {{date}}

Что сделал:
{{done}}

Что буду делать:
{{planned}}

Блокеры:
{{blockers}}`

func BuildPrompt(date string, done map[string][]string, jira map[string]JiraIssue, planned []TaskGroup, blockers map[string][]string) string {
	template := LoadPromptTemplate(Home(), "standup")
	return RenderPromptTemplate(template, date, done, jira, planned, blockers)
}

func LoadPromptTemplate(home, name string) string {
	b, err := os.ReadFile(PromptPath(home, name))
	if err == nil && strings.TrimSpace(string(b)) != "" {
		return string(b)
	}
	return DefaultStandupPromptTemplate
}

func SaveDefaultPromptTemplate(home, name string) error {
	path := PromptPath(home, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(DefaultStandupPromptTemplate+"\n"), 0644)
}

func RenderPromptTemplate(template, date string, done map[string][]string, jira map[string]JiraIssue, planned []TaskGroup, blockers map[string][]string) string {
	replacements := map[string]string{
		"{{date}}":     date,
		"{{done}}":     promptGroups(OrderedGroupsWithJira(done, jira), true),
		"{{planned}}":  promptSection("## Вход: что буду делать", EnsurePlannedFallback(planned)),
		"{{blockers}}": promptSection("## Вход: блокеры", OrderedGroupsWithJira(blockers, jira)),
	}
	out := template
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	return strings.TrimSpace(out) + "\n"
}

func promptSection(title string, groups []TaskGroup) string {
	if len(groups) == 0 {
		return ""
	}
	return title + "\n\n" + promptGroups(groups, false)
}

func promptGroups(groups []TaskGroup, alwaysTitle bool) string {
	var b strings.Builder
	for _, group := range groups {
		if group.Title != "" || alwaysTitle {
			b.WriteString("### " + group.Title + "\n")
		}
		for _, item := range group.Items {
			b.WriteString("- " + item + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func CleanTelegramItem(item string) string {
	item = strings.TrimSpace(item)
	if len(item) >= 6 && item[2] == ':' && item[5] == ' ' {
		item = strings.TrimSpace(item[6:])
	}
	item = regexp.MustCompile("`[^`]+`\\s+`[0-9a-f]{7,40}`\\s*").ReplaceAllString(item, "")
	return strings.TrimSpace(item)
}

func TelegramReport(date string, done map[string][]string, jira map[string]JiraIssue, planned []TaskGroup, blockers map[string][]string) string {
	var b strings.Builder
	b.WriteString(date + "\n\n")
	writeTelegramGroups(&b, "✅ Что сделал", OrderedGroupsWithJira(done, jira))
	planned = EnsurePlannedFallback(planned)
	if len(planned) > 0 {
		b.WriteString("\n")
		writeTelegramGroups(&b, "📌 Что буду делать", planned)
	}
	if len(blockers) > 0 {
		b.WriteString("\n")
		writeTelegramGroups(&b, "⛔ Блокеры", OrderedGroupsWithJira(blockers, jira))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func writeTelegramGroups(b *strings.Builder, title string, groups []TaskGroup) {
	b.WriteString(title + "\n")
	for _, group := range groups {
		if group.Title != "" {
			b.WriteString("• " + group.Title + "\n")
		}
		seen := map[string]bool{}
		for _, item := range group.Items {
			clean := CleanTelegramItem(item)
			if clean == "" || seen[clean] {
				continue
			}
			seen[clean] = true
			if group.Title == "" {
				b.WriteString("• " + clean + "\n")
			} else {
				b.WriteString("  - " + clean + "\n")
			}
		}
	}
}

func defaultGroqBaseURL() string {
	return "https://api.groq.com/openai/v1"
}

func GroqBaseURL(cfg Config) string {
	return strings.TrimRight(EnvDefault("GROQ_BASE_URL", DefaultIfEmpty(cfg.GroqBaseURL, defaultGroqBaseURL())), "/")
}

func GroqModel(cfg Config) string {
	return EnvDefault("GROQ_MODEL", DefaultIfEmpty(cfg.GroqModel, "llama-3.3-70b-versatile"))
}

func GroqAPIKey() string {
	return firstKeychainSecret("groq-api-token")
}

func SaveGroqStats(home, model string, headers http.Header) {
	state, err := LoadState(home)
	if err != nil {
		return
	}
	state.GroqStats = BuildGroqStats(model, headerMap(headers), time.Now())
	_ = SaveState(home, state)
}

func BuildGroqStats(model string, values map[string]string, updated time.Time) string {
	parts := []string{"🤖 Groq", model}
	if requests := quotaText("requests", values["limit-requests"], values["remaining-requests"], values["reset-requests"]); requests != "" {
		parts = append(parts, requests)
	}
	if tokens := quotaText("tokens", values["limit-tokens"], values["remaining-tokens"], values["reset-tokens"]); tokens != "" {
		parts = append(parts, tokens)
	}
	parts = append(parts, "updated "+updated.Format("15:04"))
	return strings.Join(parts, " · ")
}

func NormalizeGroqStats(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.Contains(raw, "x-ratelimit-") {
		return raw
	}
	values := map[string]string{}
	for _, match := range rawGroqStatRE.FindAllStringSubmatch(raw, -1) {
		if len(match) == 3 {
			values[strings.TrimSpace(match[1])] = strings.TrimSpace(match[2])
		}
	}
	model := "unknown model"
	for _, part := range strings.Split(raw, "·") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "model: ") {
			model = strings.TrimPrefix(part, "model: ")
		}
	}
	return BuildGroqStats(model, values, time.Now())
}

func headerMap(headers http.Header) map[string]string {
	values := map[string]string{}
	for _, key := range []string{"limit-requests", "remaining-requests", "reset-requests", "limit-tokens", "remaining-tokens", "reset-tokens"} {
		if value := headers.Get("x-ratelimit-" + key); value != "" {
			values[key] = value
		}
	}
	return values
}

func quotaText(name, limit, remaining, reset string) string {
	if limit == "" && remaining == "" && reset == "" {
		return ""
	}
	used := "?"
	if limitInt, err1 := strconv.Atoi(limit); err1 == nil {
		if remainingInt, err2 := strconv.Atoi(remaining); err2 == nil {
			used = strconv.Itoa(limitInt - remainingInt)
		}
	}
	var b strings.Builder
	b.WriteString(name + ": ")
	if limit != "" && remaining != "" {
		b.WriteString(used + " used / " + remaining + " left / " + limit + " limit")
	} else if remaining != "" {
		b.WriteString(remaining + " left")
	} else if limit != "" {
		b.WriteString(limit + " limit")
	}
	if reset != "" {
		b.WriteString(", reset in " + reset)
	}
	return b.String()
}

func callGroq(cfg Config, key, model, prompt string) (string, error) {
	body, _ := json.Marshal(chatReq{Model: model, Messages: []message{{Role: "user", Content: prompt}}})
	req, err := http.NewRequest("POST", GroqBaseURL(cfg)+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	SaveGroqStats(Home(), model, resp.Header)
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("groq error %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	var parsed chatResp
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("groq response has no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func EnsurePlannedFallback(planned []TaskGroup) []TaskGroup {
	if len(planned) > 0 {
		return planned
	}
	return []TaskGroup{{Task: "", Title: "", Items: []string{"посмотрю, что есть в спринте"}}}
}

func LoadPlannedWork(cfg Config) []TaskGroup {
	root := EnvDefault("WORKGLOG_SCAN_ROOT", DefaultIfEmpty(cfg.ScanRoot, defaultRoot))
	repos, err := FindRepos(root, EffectiveExcludeDirs(cfg, nil), EffectiveExcludePaths(cfg, nil))
	if err != nil {
		return nil
	}
	groups := map[string][]string{}
	for _, repo := range repos {
		branches, err := ReadBranches(repo)
		if err != nil {
			continue
		}
		for _, branch := range branches {
			key := TaskKey(branch)
			if key == "" {
				continue
			}
			groups[key] = append(groups[key], fmt.Sprintf("`%s` `%s`", filepath.Base(repo), branch))
		}
	}
	if len(groups) == 0 {
		return nil
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	jira := LoadJiraIssuesForKeys(cfg, keys)
	filtered := map[string][]string{}
	for key, items := range groups {
		if IsInDevelopment(jira[key].Status) {
			filtered[key] = items
		}
	}
	return OrderedGroupsWithJira(filtered, jira)
}

func ReadBranches(repo string) ([]string, error) {
	out, err := git(repo, "branch", "--format=%(refname:short)")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, err
	}
	var result []string
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" || branch == "main" || branch == "master" || branch == "develop" || branch == "dev" {
			continue
		}
		result = append(result, branch)
	}
	sort.Strings(result)
	return result, nil
}

func IsInDevelopment(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return strings.Contains(s, "разработ") || strings.Contains(s, "development") || strings.Contains(s, "in progress") || strings.Contains(s, "progress")
}

type JiraIssue struct {
	Summary string
	Status  string
}

type jiraResp struct {
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name string `json:"name"`
		} `json:"status"`
	} `json:"fields"`
}

func LoadJiraIssues(cfg Config, items []string) map[string]JiraIssue {
	keys := map[string]bool{}
	for _, item := range items {
		if key := TaskKey(item); key != "" {
			keys[key] = true
		}
	}
	list := make([]string, 0, len(keys))
	for key := range keys {
		list = append(list, key)
	}
	return LoadJiraIssuesForKeys(cfg, list)
}

func LoadJiraIssuesForKeys(cfg Config, keys []string) map[string]JiraIssue {
	result := map[string]JiraIssue{}
	if JiraURL(cfg) == "" {
		return result
	}
	token := JiraAPIToken()
	if token == "" {
		return result
	}
	seen := map[string]bool{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if issue, err := fetchJiraIssue(cfg, token, key); err == nil {
			result[key] = issue
		}
	}
	return result
}

func fetchJiraIssue(cfg Config, token, key string) (JiraIssue, error) {
	url := strings.TrimRight(JiraURL(cfg), "/") + "/rest/api/2/issue/" + key + "?fields=summary,status"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return JiraIssue{}, err
	}
	if user := JiraUser(cfg); user != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+token)))
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return JiraIssue{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return JiraIssue{}, fmt.Errorf("jira %s", resp.Status)
	}
	var parsed jiraResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return JiraIssue{}, err
	}
	return JiraIssue{Summary: parsed.Fields.Summary, Status: parsed.Fields.Status.Name}, nil
}

func JiraURL(cfg Config) string {
	if v := EnvDefault("WORKGLOG_JIRA_URL", ""); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := os.Getenv("JIRA_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(cfg.JiraURL, "/")
}

func JiraUser(cfg Config) string {
	if v := EnvDefault("WORKGLOG_JIRA_USER", ""); v != "" {
		return v
	}
	if v := os.Getenv("JIRA_USER"); v != "" {
		return v
	}
	return cfg.JiraUser
}

func JiraAPIToken() string {
	return firstKeychainSecret("jira-api-token", "worklog-jira-api-token")
}

func firstKeychainSecret(services ...string) string {
	for _, service := range services {
		if value := keychainSecret(service); value != "" {
			return value
		}
	}
	return ""
}

func keychainSecret(service string) string {
	cmd := exec.Command("security", "find-generic-password", "-s", service, "-a", os.Getenv("USER"), "-w")
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func StoreKeychainSecret(service, value string) error {
	cmd := exec.Command("security", "add-generic-password", "-a", os.Getenv("USER"), "-s", service, "-w", value, "-U")
	return cmd.Run()
}

type chatReq struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResp struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Error any `json:"error,omitempty"`
}

func SliceSet(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func Stats(home string) WorklogStats {
	today := time.Now().Format("2006-01-02")
	items, _ := ReadItems(DayPath(home, today))
	groups := GroupByTask(items)
	if _, ok := groups["untracked"]; ok && len(groups) == 1 {
		// keep untracked as task only when it is the only group
	}
	summaryFiles := 0
	if entries, err := os.ReadDir(filepath.Join(home, "summaries")); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				summaryFiles++
			}
		}
	}
	state, _ := LoadState(home)
	return WorklogStats{
		TodayEntries: len(items),
		TodayTasks:   len(groups),
		SummaryFiles: summaryFiles,
		LastScan:     state.LastScan,
		GroqStats:    NormalizeGroqStats(state.GroqStats),
	}
}

func ReadSummary(home, date string) (string, error) {
	b, err := os.ReadFile(SummaryPath(home, date))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}
