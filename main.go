package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
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

var taskRE = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)

var (
	version = "dev"
	commit  = "none"
	builtAt = "unknown"
)

type State struct {
	Seen     map[string][]string `json:"seen"`
	LastScan string              `json:"last_scan,omitempty"`
}

type Commit struct {
	Repo    string
	SHA     string
	Unix    int64
	Subject string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "scan":
		return cmdScan(args[1:])
	case "add":
		return cmdAdd(args[1:])
	case "report":
		return cmdReport(args[1:])
	case "summarize":
		return cmdSummarize(args[1:])
	case "version":
		return cmdVersion()
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func usage() {
	fmt.Println(`worklog

Commands:
  worklog scan [--root /path] [--since "14 days ago"] [--all-authors]
  worklog add [--date YYYY-MM-DD] "ABC-123 text"
  worklog report [YYYY-MM-DD]
  worklog summarize [YYYY-MM-DD] [--prompt] [--ai] [--model grok-4]
  worklog version

Env:
  WORKLOG_HOME      default ~/.worklog
  WORKLOG_SCAN_ROOT default /Users/avkorkin/prj
  WORKLOG_AI_MODEL  default grok-4
  XAI_API_KEY       optional; fallback macOS Keychain service worklog-xai-api-key`)
}

func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	root := fs.String("root", envDefault("WORKLOG_SCAN_ROOT", defaultRoot), "projects root")
	since := fs.String("since", "14 days ago", "git log since")
	allAuthors := fs.Bool("all-authors", false, "include all authors")
	author := fs.String("author", "", "git log author filter")
	keep := fs.Int("keep", 5000, "seen SHAs to keep per repo")
	if err := fs.Parse(args); err != nil {
		return err
	}

	home := worklogHome()
	state, err := loadState(home)
	if err != nil {
		return err
	}
	if state.Seen == nil {
		state.Seen = map[string][]string{}
	}

	authorFilter := *author
	if authorFilter == "" && !*allAuthors {
		authorFilter = gitGlobal("user.email")
		if authorFilter == "" {
			authorFilter = gitGlobal("user.name")
		}
	}

	repos, err := findRepos(*root)
	if err != nil {
		return err
	}
	added := 0
	for _, repo := range repos {
		commits, err := readCommits(repo, *since, authorFilter)
		if err != nil {
			continue
		}
		key := repo
		seen := sliceSet(state.Seen[key])
		var seenList []string
		for _, c := range commits {
			if seen[c.SHA] {
				continue
			}
			date := time.Unix(c.Unix, 0).Format("2006-01-02")
			line := fmt.Sprintf("- %s `%s` `%s` %s", time.Unix(c.Unix, 0).Format("15:04"), c.Repo, c.SHA, c.Subject)
			if err := appendUnderSection(dayPath(home, date), date, "Commits", line); err != nil {
				return err
			}
			seen[c.SHA] = true
			added++
		}
		for sha := range seen {
			seenList = append(seenList, sha)
		}
		sort.Strings(seenList)
		if len(seenList) > *keep {
			seenList = seenList[len(seenList)-*keep:]
		}
		state.Seen[key] = seenList
	}
	state.LastScan = time.Now().Format(time.RFC3339)
	if err := saveState(home, state); err != nil {
		return err
	}
	fmt.Printf("added %d commit(s)\n", added)
	return nil
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	date := fs.String("date", time.Now().Format("2006-01-02"), "entry date")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return errors.New("empty entry")
	}
	line := fmt.Sprintf("- %s %s", time.Now().Format("15:04"), text)
	if err := appendUnderSection(dayPath(worklogHome(), *date), *date, "Manual", line); err != nil {
		return err
	}
	fmt.Printf("added manual entry to %s\n", dayPath(worklogHome(), *date))
	return nil
}

func cmdReport(args []string) error {
	date := todayOrArg(args)
	items, err := readItems(dayPath(worklogHome(), date))
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Printf("no entries for %s\n", date)
		return nil
	}
	groups := groupByTask(items)
	fmt.Printf("# %s\n", date)
	printGroups(groups)
	return nil
}

func cmdSummarize(args []string) error {
	promptOnly, ai := false, false
	model := envDefault("WORKLOG_AI_MODEL", "grok-4")
	var positional []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--prompt":
			promptOnly = true
		case args[i] == "--ai":
			ai = true
		case args[i] == "--model" && i+1 < len(args):
			model = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--model="):
			model = strings.TrimPrefix(args[i], "--model=")
		default:
			positional = append(positional, args[i])
		}
	}
	date := todayOrArg(positional)
	items, err := readItems(dayPath(worklogHome(), date))
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Printf("no entries for %s\n", date)
		return nil
	}
	prompt := buildPrompt(date, groupByTask(items))
	if promptOnly || !ai {
		fmt.Print(prompt)
		return nil
	}
	answer, err := callXAI(model, prompt)
	if err != nil {
		return err
	}
	path := summaryPath(worklogHome(), date)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(answer), 0644); err != nil {
		return err
	}
	fmt.Println(answer)
	fmt.Printf("\nsaved to %s\n", path)
	return nil
}

func cmdVersion() error {
	fmt.Printf("worklog %s\ncommit: %s\nbuilt: %s\n", version, commit, builtAt)
	return nil
}

func worklogHome() string {
	if v := os.Getenv("WORKLOG_HOME"); v != "" {
		return expandHome(v)
	}
	return filepath.Join(userHome(), ".worklog")
}

func dayPath(home, date string) string     { return filepath.Join(home, "days", date+".md") }
func summaryPath(home, date string) string { return filepath.Join(home, "summaries", date+".md") }
func envDefault(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func userHome() string { h, _ := os.UserHomeDir(); return h }
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(userHome(), p[2:])
	}
	return p
}

func todayOrArg(args []string) string {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return args[0]
	}
	return time.Now().Format("2006-01-02")
}

func findRepos(root string) ([]string, error) {
	var repos []string
	skip := map[string]bool{".idea": true, ".gradle": true, "node_modules": true, "vendor": true, "target": true, "build": true, "dist": true}
	err := filepath.WalkDir(expandHome(root), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
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

func readCommits(repo, since, author string) ([]Commit, error) {
	args := []string{"log", "--since=" + since, "--format=%H%x1f%at%x1f%s", "--no-merges"}
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

func gitGlobal(key string) string {
	cmd := exec.Command("git", "config", "--global", "--get", key)
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func loadState(home string) (State, error) {
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

func saveState(home string, state State) error {
	if err := os.MkdirAll(home, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, "state.json"), append(b, '\n'), 0644)
}

func appendUnderSection(path, date, section, line string) error {
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
	return os.WriteFile(path, []byte("# "+date+"\n\n## Commits\n\n## Manual\n"), 0644)
}

func readItems(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "- ") {
			items = append(items, strings.TrimPrefix(line, "- "))
		}
	}
	return items, nil
}

func groupByTask(items []string) map[string][]string {
	groups := map[string][]string{}
	for _, item := range items {
		task := "untracked"
		if m := taskRE.FindString(item); m != "" {
			task = m
		}
		groups[task] = append(groups[task], item)
	}
	return groups
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

func buildPrompt(date string, groups map[string][]string) string {
	var b strings.Builder
	b.WriteString("Сделай краткое standup summary на русском языке по рабочему дневнику.\n")
	b.WriteString("Формат строго markdown: ## Что сделал, ## В процессе, ## Блокеры.\n")
	b.WriteString("Не выдумывай блокеры. Если блокеров нет, напиши '- Нет'.\n")
	b.WriteString("Объединяй записи по задачам, убирай технический шум, оставляй конкретный результат.\n\n")
	b.WriteString("Дата: " + date + "\n\n")
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("### " + k + "\n")
		for _, item := range groups[k] {
			b.WriteString("- " + item + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func keychainSecret(service string) string {
	cmd := exec.Command("security", "find-generic-password", "-s", service, "-a", os.Getenv("USER"), "-w")
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
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

func callXAI(model, prompt string) (string, error) {
	key := os.Getenv("XAI_API_KEY")
	if key == "" {
		key = keychainSecret("worklog-xai-api-key")
	}
	if key == "" {
		return "", errors.New("XAI_API_KEY is empty and Keychain service worklog-xai-api-key is not set")
	}
	body, _ := json.Marshal(chatReq{Model: model, Messages: []message{{Role: "user", Content: prompt}}})
	req, err := http.NewRequest("POST", "https://api.x.ai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("xAI error %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	var parsed chatResp
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("xAI response has no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func sliceSet(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}
