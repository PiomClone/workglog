package app

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	wl "github.com/PiomClone/workglog/internal/worklog"
)

const defaultRoot = "/Users/avkorkin/prj"

var (
	version = "dev"
	commit  = "none"
	builtAt = "unknown"
)

type Config = wl.Config

type State = wl.State

type Commit = wl.Commit

func SetVersion(v, c, b string) {
	version = v
	commit = c
	builtAt = b
}

func Run(args []string) error {
	if len(args) == 0 {
		return cmdWizard()
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
	case "standup":
		return cmdStandup(args[1:])
	case "setup":
		return cmdSetup()
	case "stats":
		return cmdStats()
	case "prompt":
		return cmdPrompt(args[1:])
	case "web":
		return cmdWeb(args[1:])
	case "wizard":
		return cmdWizard()
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

Run without arguments to open the interactive wizard.

Commands:
  worklog scan [--root /path] [--since "YYYY-MM-DD 00:00"] [--all-authors] [--current-branch] [--force]
  worklog add [--date YYYY-MM-DD] [--type done|plan|blocker] "ABC-123 text"
  worklog report [YYYY-MM-DD]
  worklog summarize [YYYY-MM-DD] [--prompt] [--ai] [--model llama-3.3-70b-versatile]
  worklog standup [--date YYYY-MM-DD] [--prompt] [--no-scan]
  worklog setup
  worklog stats
  worklog prompt init|path|print
  worklog web [--addr 127.0.0.1:8088]
  worklog web start|stop|status|restart
  worklog wizard
  worklog version

Env:
  WORKLOG_HOME      default ~/.worklog
  WORKLOG_SCAN_ROOT default /Users/avkorkin/prj or config scan_root
  WORKLOG_EXCLUDE_DIRS comma-separated dir names, overrides config exclude_dirs
  WORKLOG_EXCLUDE_PATHS comma-separated paths, overrides config exclude_paths
  GROQ_MODEL        default llama-3.3-70b-versatile or config groq_model
  GROQ_BASE_URL     default https://api.groq.com/openai/v1 or config groq_base_url
  Groq token        Keychain groq-api-token; if empty, simple summary without AI
  WORKLOG_JIRA_URL optional; fallback JIRA_URL or config jira_url
  WORKLOG_JIRA_USER optional; only needed for Basic auth
  Jira token        optional; fallback Keychain jira-api-token/worklog-jira-api-token`)
}

func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	cfg := wl.LoadConfig(wl.Home())
	root := fs.String("root", wl.EnvDefault("WORKLOG_SCAN_ROOT", wl.DefaultIfEmpty(cfg.ScanRoot, defaultRoot)), "projects root")
	excludes := multiFlag{}
	excludePaths := multiFlag{}
	fs.Var(&excludes, "exclude", "directory name to skip; can be repeated")
	fs.Var(&excludePaths, "exclude-path", "repository/path to skip; can be repeated")
	since := fs.String("since", wl.StartOfToday(), "git log since")
	allAuthors := fs.Bool("all-authors", false, "include all authors")
	author := fs.String("author", "", "git log author filter")
	keep := fs.Int("keep", 5000, "seen SHAs to keep per repo")
	quiet := fs.Bool("quiet", false, "disable scan progress")
	force := fs.Bool("force", false, "ignore state.json and deduplicate by SHA in day files")
	currentBranch := fs.Bool("current-branch", false, "scan only current branch instead of all refs")
	if err := fs.Parse(args); err != nil {
		return err
	}

	home := wl.Home()
	state, err := wl.LoadState(home)
	if err != nil {
		return err
	}
	if state.Seen == nil {
		state.Seen = map[string][]string{}
	}

	authorFilter := *author
	if authorFilter == "" && !*allAuthors {
		authorFilter = wl.GitGlobal("user.email")
		if authorFilter == "" {
			authorFilter = wl.GitGlobal("user.name")
		}
	}

	repos, err := wl.FindRepos(*root, wl.EffectiveExcludeDirs(cfg, excludes), wl.EffectiveExcludePaths(cfg, excludePaths))
	if err != nil {
		return err
	}
	if !*quiet {
		fmt.Fprintf(os.Stderr, "scan: found %d repo(s) under %s\n", len(repos), *root)
	}
	added := 0
	for i, repo := range repos {
		if !*quiet {
			fmt.Fprintf(os.Stderr, "scan: [%d/%d] %s\n", i+1, len(repos), repo)
		}
		commits, err := wl.ReadCommits(repo, *since, authorFilter, !*currentBranch)
		if err != nil {
			continue
		}
		key := repo
		seen := wl.SliceSet(state.Seen[key])
		var seenList []string
		for _, c := range commits {
			date := time.Unix(c.Unix, 0).Format("2006-01-02")
			if !*force && seen[c.SHA] {
				continue
			}
			if wl.DayHasSHA(home, date, c.SHA) {
				seen[c.SHA] = true
				continue
			}
			line := fmt.Sprintf("- %s `%s` `%s` %s", time.Unix(c.Unix, 0).Format("15:04"), c.Repo, c.SHA, c.Subject)
			if err := wl.AppendUnderSection(wl.DayPath(home, date), date, "Commits", line); err != nil {
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
	if err := wl.SaveState(home, state); err != nil {
		return err
	}
	fmt.Printf("added %d commit(s)\n", added)
	return nil
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	date := fs.String("date", time.Now().Format("2006-01-02"), "entry date")
	kind := fs.String("type", wl.KindDone, "entry type: done, plan, blocker")
	plan := fs.Bool("plan", false, "shortcut for --type plan")
	blocker := fs.Bool("blocker", false, "shortcut for --type blocker")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return errors.New("empty entry")
	}
	if *plan {
		*kind = wl.KindPlan
	}
	if *blocker {
		*kind = wl.KindBlocker
	}
	section, err := manualSection(*kind)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("- %s %s", time.Now().Format("15:04"), text)
	if err := wl.AppendUnderSection(wl.DayPath(wl.Home(), *date), *date, section, line); err != nil {
		return err
	}
	fmt.Printf("added %s entry to %s\n", *kind, wl.DayPath(wl.Home(), *date))
	return nil
}

func manualSection(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", wl.KindDone, "manual":
		return "Manual", nil
	case wl.KindPlan, "todo", "next":
		return "Plan", nil
	case wl.KindBlocker, "block", "blocked":
		return "Blockers", nil
	default:
		return "", fmt.Errorf("unknown entry type: %s", kind)
	}
}

func cmdReport(args []string) error {
	date := wl.TodayOrArg(args)
	entries, err := wl.ReadEntries(wl.DayPath(wl.Home(), date))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Printf("no entries for %s\n", date)
		return nil
	}
	cfg := wl.LoadConfig(wl.Home())
	doneItems := wl.EntryTexts(entries, wl.KindDone)
	planItems := wl.EntryTexts(entries, wl.KindPlan)
	blockerItems := wl.EntryTexts(entries, wl.KindBlocker)
	allItems := append(append([]string{}, doneItems...), append(planItems, blockerItems...)...)
	jira := wl.LoadJiraIssues(cfg, allItems)
	planned := wl.OrderedGroupsWithJira(wl.GroupByTask(planItems), jira)
	planned = append(planned, wl.LoadPlannedWork(cfg)...)
	fmt.Print(wl.TelegramReport(date, wl.GroupByTask(doneItems), jira, planned, wl.GroupByTask(blockerItems)))
	return nil
}

func cmdSummarize(args []string) error {
	cfg := wl.LoadConfig(wl.Home())
	promptOnly, ai := false, false
	model := wl.GroqModel(cfg)
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
	date := wl.TodayOrArg(positional)
	entries, err := wl.ReadEntries(wl.DayPath(wl.Home(), date))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Printf("no entries for %s\n", date)
		return nil
	}
	doneItems := wl.EntryTexts(entries, wl.KindDone)
	planItems := wl.EntryTexts(entries, wl.KindPlan)
	blockerItems := wl.EntryTexts(entries, wl.KindBlocker)
	allItems := append(append([]string{}, doneItems...), append(planItems, blockerItems...)...)
	jira := wl.LoadJiraIssues(cfg, allItems)
	done := wl.GroupByTask(doneItems)
	planned := wl.OrderedGroupsWithJira(wl.GroupByTask(planItems), jira)
	planned = append(planned, wl.LoadPlannedWork(cfg)...)
	blockers := wl.GroupByTask(blockerItems)
	prompt := wl.BuildPrompt(date, done, jira, planned, blockers)
	if promptOnly || !ai {
		fmt.Print(prompt)
		return nil
	}
	answer, err := wl.SummarizeWithAIOrSimple(cfg, model, date, done, jira, planned, blockers, prompt)
	if err != nil {
		return err
	}
	path := wl.SummaryPath(wl.Home(), date)
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

func cmdStandup(args []string) error {
	fs := flag.NewFlagSet("standup", flag.ContinueOnError)
	date := fs.String("date", wl.PreviousWorkday(time.Now()).Format("2006-01-02"), "standup date")
	promptOnly := fs.Bool("prompt", false, "print prompt instead of calling AI")
	noScan := fs.Bool("no-scan", false, "do not scan before summarizing")
	model := fs.String("model", wl.GroqModel(wl.LoadConfig(wl.Home())), "Groq model")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*noScan {
		if err := cmdScan([]string{"--since", *date + " 00:00", "--force"}); err != nil {
			return err
		}
	}
	if *promptOnly {
		return cmdSummarize([]string{*date, "--prompt", "--model", *model})
	}
	return cmdSummarize([]string{*date, "--ai", "--model", *model})
}

func cmdStats() error {
	stats := wl.Stats(wl.Home())
	fmt.Printf("entries_today: %d\n", stats.TodayEntries)
	fmt.Printf("tasks_today: %d\n", stats.TodayTasks)
	fmt.Printf("summary_files: %d\n", stats.SummaryFiles)
	fmt.Printf("last_scan: %s\n", wl.DefaultIfEmpty(stats.LastScan, "never"))
	fmt.Printf("groq_stats: %s\n", wl.DefaultIfEmpty(stats.GroqStats, "not available"))
	return nil
}

func cmdPrompt(args []string) error {
	action := "path"
	if len(args) > 0 {
		action = args[0]
	}
	path := wl.PromptPath(wl.Home(), "standup")
	switch action {
	case "init":
		if err := wl.SaveDefaultPromptTemplate(wl.Home(), "standup"); err != nil {
			return err
		}
		fmt.Println(path)
	case "path":
		fmt.Println(path)
	case "print":
		fmt.Print(wl.LoadPromptTemplate(wl.Home(), "standup"))
	default:
		return fmt.Errorf("unknown prompt action: %s", action)
	}
	return nil
}

func cmdSetup() error {
	home := wl.Home()
	cfg := wl.LoadConfig(home)
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("worklog setup")
		fmt.Println("1) Scan root")
		fmt.Println("2) Groq")
		fmt.Println("3) Jira")
		fmt.Println("4) Show config")
		fmt.Println("5) Done")
		choice := ask(r, "Choose", "5")
		switch choice {
		case "1":
			cfg.ScanRoot = ask(r, "Projects root", wl.DefaultIfEmpty(cfg.ScanRoot, defaultRoot))
			cfg.ExcludeDirs = wl.SplitCSV(ask(r, "Exclude dirs", strings.Join(wl.EffectiveExcludeDirs(cfg, nil), ",")))
			cfg.ExcludePaths = wl.SplitCSV(ask(r, "Exclude paths", strings.Join(wl.EffectiveExcludePaths(cfg, nil), ",")))
			if err := wl.SaveConfig(home, cfg); err != nil {
				return err
			}
		case "2":
			cfg.GroqModel = ask(r, "Groq model", wl.GroqModel(cfg))
			cfg.GroqBaseURL = strings.TrimRight(ask(r, "Groq base URL", wl.GroqBaseURL(cfg)), "/")
			groqKey := ask(r, "Groq API key (empty to keep current)", "")
			if groqKey != "" {
				if err := wl.StoreKeychainSecret("groq-api-token", groqKey); err != nil {
					return err
				}
			}
			if err := wl.SaveConfig(home, cfg); err != nil {
				return err
			}
		case "3":
			cfg.JiraURL = strings.TrimRight(ask(r, "Jira URL (empty to skip Jira enrichment)", wl.JiraURL(cfg)), "/")
			cfg.JiraUser = ask(r, "Jira user/email (empty for Bearer token)", wl.JiraUser(cfg))
			jiraToken := ask(r, "Jira API token/PAT (empty to keep current)", "")
			if jiraToken != "" {
				if err := wl.StoreKeychainSecret("jira-api-token", jiraToken); err != nil {
					return err
				}
			}
			if err := wl.SaveConfig(home, cfg); err != nil {
				return err
			}
		case "4":
			printConfig(home, cfg)
		case "5", "", "q", "quit", "exit":
			fmt.Printf("saved config to %s\n", wl.ConfigPath(home))
			return nil
		default:
			fmt.Println("unknown choice")
		}
		fmt.Println()
	}
}

func cmdWizard() error {
	r := bufio.NewReader(os.Stdin)
	for {
		printWizardHeader()
		fmt.Println("1) Scan commits")
		fmt.Println("2) Add manual note")
		fmt.Println("3) Report")
		fmt.Println("4) Generate standup with Grok")
		fmt.Println("5) Show standup prompt only")
		fmt.Println("6) Setup keys/config")
		fmt.Println("7) Exit")
		choice := ask(r, "Choose", "4")
		switch choice {
		case "1":
			return cmdScan(nil)
		case "2":
			kind := ask(r, "Type done/plan/blocker", wl.KindDone)
			text := ask(r, "Note", "")
			if text == "" {
				return errors.New("empty note")
			}
			return cmdAdd([]string{"--type", kind, text})
		case "3":
			date := ask(r, "Date", time.Now().Format("2006-01-02"))
			return cmdReport([]string{date})
		case "4":
			date := ask(r, "Date", wl.PreviousWorkday(time.Now()).Format("2006-01-02"))
			return cmdStandup([]string{"--date", date})
		case "5":
			date := ask(r, "Date", wl.PreviousWorkday(time.Now()).Format("2006-01-02"))
			return cmdStandup([]string{"--date", date, "--prompt"})
		case "6":
			return cmdSetup()
		case "7", "", "q", "quit", "exit":
			return nil
		default:
			fmt.Println("unknown choice")
		}
	}
}

func printWizardHeader() {
	cfg := wl.LoadConfig(wl.Home())
	fmt.Println("worklog wizard")
	fmt.Printf("root: %s\n", wl.DefaultIfEmpty(cfg.ScanRoot, defaultRoot))
	fmt.Printf("jira: %s, token: %s\n", configured(wl.JiraURL(cfg) != ""), configured(wl.JiraAPIToken() != ""))
	fmt.Printf("groq: %s, model: %s\n", configured(wl.GroqAPIKey() != ""), wl.GroqModel(cfg))
	fmt.Println("commands: scan | add | report | standup(Grok) | standup --prompt(no Grok) | setup")
	fmt.Println()
}

func cmdVersion() error {
	fmt.Printf("worklog %s\ncommit: %s\nbuilt: %s\n", version, commit, builtAt)
	return nil
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, wl.SplitCSV(value)...)
	return nil
}

func printGroups(groups map[string][]string, jira map[string]wl.JiraIssue) {
	for _, group := range wl.OrderedGroupsWithJira(groups, jira) {
		fmt.Printf("\n## %s\n", group.Title)
		for _, item := range group.Items {
			fmt.Printf("- %s\n", item)
		}
	}
}

func printConfig(home string, cfg Config) {
	fmt.Printf("config: %s\n", wl.ConfigPath(home))
	fmt.Printf("scan_root: %s\n", wl.DefaultIfEmpty(cfg.ScanRoot, defaultRoot))
	fmt.Printf("exclude_dirs: %s\n", strings.Join(wl.EffectiveExcludeDirs(cfg, nil), ","))
	fmt.Printf("exclude_paths: %s\n", strings.Join(wl.EffectiveExcludePaths(cfg, nil), ","))
	fmt.Printf("jira_url: %s\n", wl.JiraURL(cfg))
	fmt.Printf("jira_user: %s\n", wl.JiraUser(cfg))
	fmt.Printf("groq_model: %s\n", wl.GroqModel(cfg))
	fmt.Printf("groq_base_url: %s\n", wl.GroqBaseURL(cfg))
	fmt.Printf("groq_key: %s\n", configured(wl.GroqAPIKey() != ""))
	fmt.Printf("jira_token: %s\n", configured(wl.JiraAPIToken() != ""))
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
