package app

import (
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wl "github.com/PiomClone/workglog/internal/worklog"
)

func cmdWeb(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "start":
			return webServiceStart(args[1:])
		case "stop":
			return webServiceStop()
		case "restart":
			_ = webServiceStop()
			return webServiceStart(args[1:])
		case "status":
			return webServiceStatus()
		}
	}
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8088", "listen address")
	token := fs.String("token", "", "optional access token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if isPublicAddr(*addr) && *token == "" {
		return fmt.Errorf("--token is required when listening on %s", *addr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/favicon.svg", webFavicon)
	mux.HandleFunc("/", webIndex)
	mux.HandleFunc("/report", webReport)
	mux.HandleFunc("/prompt", webPrompt)
	mux.HandleFunc("/summary", webSummary)
	mux.HandleFunc("/setup", webSetup)
	mux.HandleFunc("/scan", webScanAction)
	mux.HandleFunc("/add", webAddAction)
	mux.HandleFunc("/generate", webGenerateAction)

	handler := http.Handler(mux)
	if *token != "" {
		handler = tokenAuth(handler, *token)
	}
	fmt.Printf("worklog web listening on http://%s\n", *addr)
	return http.ListenAndServe(*addr, handler)
}

const webLaunchLabel = "com.worklog.web"

func webServiceStart(args []string) error {
	fs := flag.NewFlagSet("web start", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8088", "listen address")
	token := fs.String("token", "", "optional access token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if isPublicAddr(*addr) && *token == "" {
		return fmt.Errorf("--token is required when listening on %s", *addr)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	home := wl.Home()
	if err := os.MkdirAll(home, 0755); err != nil {
		return err
	}
	plist := webLaunchPlistPath()
	if err := os.MkdirAll(filepath.Dir(plist), 0755); err != nil {
		return err
	}
	content := webLaunchPlist(exe, *addr, *token, filepath.Join(home, "web.out.log"), filepath.Join(home, "web.err.log"))
	if err := os.WriteFile(plist, []byte(content), 0644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", "gui/"+fmt.Sprint(os.Getuid()), plist).Run()
	if err := exec.Command("launchctl", "bootstrap", "gui/"+fmt.Sprint(os.Getuid()), plist).Run(); err != nil {
		return err
	}
	if err := exec.Command("launchctl", "kickstart", "-k", "gui/"+fmt.Sprint(os.Getuid())+"/"+webLaunchLabel).Run(); err != nil {
		return err
	}
	fmt.Printf("started %s at http://%s\n", webLaunchLabel, *addr)
	fmt.Printf("logs: %s %s\n", filepath.Join(home, "web.out.log"), filepath.Join(home, "web.err.log"))
	return nil
}

func webServiceStop() error {
	plist := webLaunchPlistPath()
	_ = exec.Command("launchctl", "bootout", "gui/"+fmt.Sprint(os.Getuid()), plist).Run()
	fmt.Printf("stopped %s\n", webLaunchLabel)
	return nil
}

func webServiceStatus() error {
	cmd := exec.Command("launchctl", "print", "gui/"+fmt.Sprint(os.Getuid())+"/"+webLaunchLabel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("%s is not running\n", webLaunchLabel)
		return nil
	}
	fmt.Print(string(out))
	return nil
}

func webLaunchPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", webLaunchLabel+".plist")
}

func webLaunchPlist(exe, addr, token, stdout, stderr string) string {
	args := ""
	for _, arg := range []string{"web", "--addr", addr} {
		args += "\n    <string>" + xmlEscape(arg) + "</string>"
	}
	if token != "" {
		args += "\n    <string>--token</string>\n    <string>" + xmlEscape(token) + "</string>"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + webLaunchLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + xmlEscape(exe) + `</string>` + args + `
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>` + xmlEscape(stdout) + `</string>
  <key>StandardErrorPath</key>
  <string>` + xmlEscape(stderr) + `</string>
</dict>
</plist>
`
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func webFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = w.Write([]byte(faviconSVG))
}

const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 256 256" role="img" aria-label="worklog icon">
  <defs><linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#0f172a"/><stop offset="1" stop-color="#111827"/></linearGradient><linearGradient id="accent" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#5eead4"/><stop offset="1" stop-color="#60a5fa"/></linearGradient></defs>
  <rect width="256" height="256" rx="56" fill="url(#bg)"/>
  <path d="M74 48h78l38 38v122H74z" fill="#f8fafc" opacity=".96"/>
  <path d="M152 48v40h38" fill="#cbd5e1"/>
  <path d="M92 111h72M92 139h72M92 167h44" stroke="#334155" stroke-width="10" stroke-linecap="round"/>
  <path d="M56 196c28-16 38-16 62 0s34 16 62 0" fill="none" stroke="url(#accent)" stroke-width="12" stroke-linecap="round"/>
  <circle cx="56" cy="196" r="13" fill="#5eead4"/><circle cx="118" cy="196" r="13" fill="#60a5fa"/><circle cx="180" cy="196" r="13" fill="#a78bfa"/>
  <path d="M197 114l7 17 18 7-18 7-7 18-7-18-18-7 18-7z" fill="#facc15"/>
</svg>`

func isPublicAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host == "" || host == "0.0.0.0" || host == "::"
}

func tokenAuth(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == token || strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == token {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

type webPage struct {
	Title       string
	Path        string
	Date        string
	PrevDate    string
	NextDate    string
	Config      Config
	JiraOK      bool
	JiraTokenOK bool
	GroqOK      bool
	Items       []string
	Groups      map[string][]string
	Ordered     []wl.TaskGroup
	Prompt      string
	Summary     string
	Stats       wl.WorklogStats
}

var pageTpl = template.Must(template.New("page").Funcs(template.FuncMap{"join": joinCSV}).Parse(`<!doctype html>
<html lang="ru" class="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · worklog</title>
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<script src="https://unpkg.com/htmx.org@1.9.10"></script>
<script src="https://cdn.tailwindcss.com"></script>
<style>
body{background:radial-gradient(circle at 10% 0,#0ea5e933,transparent 28%),radial-gradient(circle at 90% 10%,#14b8a633,transparent 24%),#0f172a;color:#f8fafc}.htmx-indicator{display:none}.htmx-request .htmx-indicator,.htmx-request.htmx-indicator{display:block}pre{white-space:pre-wrap}.card{background:rgba(15,23,42,.78);border:1px solid rgba(51,65,85,.9);border-radius:1.25rem;padding:1.25rem;box-shadow:0 18px 50px rgba(0,0,0,.22)}.muted{color:#94a3b8}.badge{display:inline-flex;align-items:center;gap:.35rem;border-radius:999px;padding:.2rem .6rem;font-size:.75rem;font-weight:700}.badge-ok{background:#064e3b;color:#a7f3d0}.badge-bad{background:#7f1d1d;color:#fecaca}.badge-info{background:#1e3a8a;color:#bfdbfe}.nav{color:#cbd5e1;border:1px solid #334155;background:rgba(15,23,42,.72);border-radius:999px;padding:.55rem .85rem}.nav:hover{background:#1e293b;color:white}.nav-active{background:#0284c7;border-color:#38bdf8;color:white}a{color:#38bdf8}code{background:#020617;border:1px solid #334155;border-radius:.45rem;padding:.15rem .4rem;color:#bae6fd}select,input{background:#020617;border:1px solid #334155;border-radius:.85rem;padding:.75rem .9rem;color:#e2e8f0;outline:none}input{min-width:16rem}input[type=checkbox]{min-width:0;width:1rem;height:1rem;accent-color:#38bdf8}select:focus,input:focus{border-color:#38bdf8;box-shadow:0 0 0 3px #38bdf833}.btn{display:inline-flex;align-items:center;justify-content:center;gap:.45rem;border-radius:.9rem;padding:.75rem 1rem;font-weight:800;transition:.15s;color:white;border:1px solid transparent;background:linear-gradient(135deg,#0284c7,#2563eb);box-shadow:0 10px 24px rgba(2,132,199,.18)}.btn:hover{transform:translateY(-1px);filter:brightness(1.08)}.btn[disabled],button[disabled]{opacity:.55;cursor:not-allowed;transform:none;filter:grayscale(.3)}.btn2{background:#1e293b;color:#e2e8f0;border-color:#475569;box-shadow:none}.btn-copy{background:#0369a1;color:#e0f2fe;border-color:#0284c7;box-shadow:none;padding:.42rem .7rem;border-radius:.65rem;font-size:.8rem}.btn-green{background:linear-gradient(135deg,#059669,#0d9488)}.task{border-left:3px solid #38bdf8;background:rgba(15,23,42,.72);border-radius:.9rem;padding:1rem;margin:.75rem 0}.entry{padding:.35rem 0;border-top:1px solid rgba(51,65,85,.65)}.entry:first-child{border-top:0}.field{display:flex;flex-direction:column;gap:.35rem}.field label{font-size:.75rem;color:#94a3b8;font-weight:800;text-transform:uppercase;letter-spacing:.06em}
@keyframes loading{0%{transform:scaleX(0)}50%{transform:scaleX(.7)}100%{transform:scaleX(1);opacity:0}}
</style>
</head>
<body class="min-h-screen p-4 md:p-8 font-sans" hx-indicator="#spinner">
<div id="progress-bar" class="fixed top-0 left-0 w-full h-1 z-50 htmx-indicator"><div class="h-full bg-sky-500 animate-[loading_2s_ease-in-out_infinite] origin-left"></div></div>
<div id="spinner" class="fixed top-4 right-4 z-50 htmx-indicator"><div class="animate-spin rounded-full h-8 w-8 border-b-2 border-sky-400"></div></div>
<div class="max-w-6xl mx-auto">
<header class="flex flex-col md:flex-row justify-between gap-4 md:items-center mb-8">
  <div class="flex items-center gap-4"><img src="/favicon.svg" alt="worklog" class="w-14 h-14 rounded-xl shadow-lg"><div><h1 class="text-3xl font-bold text-sky-400">{{.Title}}</h1><p class="muted text-sm mt-1">коммиты · заметки · Jira · standup</p></div></div>
  <nav class="flex flex-wrap gap-2 text-sm"><a class="nav {{if eq .Path "/"}}nav-active{{end}}" href="/">Главная</a><a class="nav {{if eq .Path "/report"}}nav-active{{end}}" href="/report?date={{.Date}}">Отчёт</a><a class="nav {{if eq .Path "/prompt"}}nav-active{{end}}" href="/prompt?date={{.Date}}">Промпт</a><a class="nav {{if eq .Path "/summary"}}nav-active{{end}}" href="/summary?date={{.Date}}">Summary</a><a class="nav {{if eq .Path "/setup"}}nav-active{{end}}" href="/setup">Настройки</a></nav>
</header>
{{template "body" .}}
<footer class="mt-12 text-center text-slate-600 text-xs border-t border-slate-800 pt-8">worklog · local only</footer>
</div>
<script>
function lockForm(form){
  form.querySelectorAll('button').forEach(function(btn){
    if(btn.disabled) return;
    btn.dataset.originalText = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<span class="animate-spin inline-block">⏳</span> ' + btn.innerHTML;
  });
}
function unlockForm(form){
  form.querySelectorAll('button').forEach(function(btn){
    if(btn.dataset.originalText){ btn.innerHTML = btn.dataset.originalText; delete btn.dataset.originalText; }
    btn.disabled = false;
  });
}
document.addEventListener('submit', function(e){ lockForm(e.target); });
document.body.addEventListener('htmx:beforeRequest', function(e){
  var form = e.target.closest ? e.target.closest('form') : null;
  if(form) lockForm(form);
});
document.body.addEventListener('htmx:afterRequest', function(e){
  var form = e.target.closest ? e.target.closest('form') : null;
  if(form) unlockForm(form);
});
function copyText(id, btn){
  var el = document.getElementById(id);
  if(!el) return;
  navigator.clipboard.writeText(el.innerText).then(function(){
    var old = btn.innerHTML;
    btn.innerHTML = '✓ Скопировано';
    setTimeout(function(){ btn.innerHTML = old; }, 1500);
  });
}
</script>
</body></html>`))

func joinCSV(values []string) string {
	return strings.Join(values, ",")
}

func render(w http.ResponseWriter, title string, data webPage, body string) {
	tpl, err := pageTpl.Clone()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tpl = template.Must(tpl.New("body").Parse(body))
	data.Title = title
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, "page", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func webBase(r *http.Request) webPage {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		d = time.Now()
		date = d.Format("2006-01-02")
	}
	cfg := wl.LoadConfig(wl.Home())
	return webPage{
		Path:        r.URL.Path,
		Date:        date,
		PrevDate:    d.AddDate(0, 0, -1).Format("2006-01-02"),
		NextDate:    d.AddDate(0, 0, 1).Format("2006-01-02"),
		Config:      cfg,
		JiraOK:      wl.JiraURL(cfg) != "",
		JiraTokenOK: wl.JiraAPIToken() != "",
		GroqOK:      wl.GroqAPIKey() != "",
		Stats:       wl.Stats(wl.Home()),
	}
}

func webIndex(w http.ResponseWriter, r *http.Request) {
	data := webBase(r)
	render(w, "Главная", data, `<div class="grid grid-cols-2 md:grid-cols-4 gap-4 my-4"><div class="card text-center"><div class="muted text-[10px] uppercase font-bold">Записей сегодня</div><div class="text-2xl font-black mt-1">{{.Stats.TodayEntries}}</div></div><div class="card text-center"><div class="muted text-[10px] uppercase font-bold">Задач сегодня</div><div class="text-2xl font-black text-sky-300 mt-1">{{.Stats.TodayTasks}}</div></div><div class="card text-center"><div class="muted text-[10px] uppercase font-bold">Summary</div><div class="text-2xl font-black text-emerald-400 mt-1">{{.Stats.SummaryFiles}}</div></div><div class="card text-center"><div class="muted text-[10px] uppercase font-bold">Last scan</div><div class="text-xs font-bold text-slate-200 mt-2">{{if .Stats.LastScan}}{{.Stats.LastScan}}{{else}}никогда{{end}}</div></div></div><div class="mb-4 px-4 py-3 bg-indigo-500/10 border border-indigo-500/20 rounded-xl text-xs text-indigo-200">{{if .Stats.GroqStats}}{{.Stats.GroqStats}}{{else}}🤖 Статистика Groq будет доступна после первого AI-запроса{{end}}</div><div class="grid grid-cols-1 md:grid-cols-3 gap-4 my-4"><div class="card"><p class="muted text-sm">Рабочая папка</p><h2 class="text-lg font-bold text-sky-300 mt-1">{{if .Config.ScanRoot}}{{.Config.ScanRoot}}{{else}}/Users/avkorkin/prj{{end}}</h2></div><div class="card"><p class="muted text-sm">Интеграции</p><div class="mt-2 flex flex-wrap gap-2">{{if .JiraOK}}<span class="badge badge-ok">Jira URL</span>{{else}}<span class="badge badge-bad">Jira URL</span>{{end}}{{if .JiraTokenOK}}<span class="badge badge-ok">Jira token</span>{{else}}<span class="badge badge-bad">Jira token</span>{{end}}{{if .GroqOK}}<span class="badge badge-ok">Groq</span>{{else}}<span class="badge badge-info">без AI</span>{{end}}</div></div><div class="card"><p class="muted text-sm">Модель</p><h2 class="text-lg font-bold text-sky-300 mt-1">{{.Config.GroqModel}}</h2></div></div><div class="grid grid-cols-1 lg:grid-cols-2 gap-4"><div class="card"><h2 class="text-xl font-bold text-sky-300 mb-1">Быстрые действия</h2><p class="muted mb-4">Скан, генерация и переход к отчёту за день.</p><form class="flex flex-wrap gap-3 items-end my-3" method="post" action="/scan"><div class="field"><label>Сканировать с</label><input name="since" value="today 00:00"></div><label class="flex items-center gap-2 text-sm text-slate-300 pb-3"><input type="checkbox" name="force" value="1"> force rescan</label><button class="btn">↻ Сканировать</button></form><form class="flex flex-wrap gap-3 items-end my-3" method="post" action="/generate"><div class="field"><label>Дата отчёта</label><input name="date" value="{{.Date}}"></div><button class="btn btn-green">✦ Сгенерировать</button><button class="btn btn2" name="prompt" value="1">Показать промпт</button></form></div><div class="card"><h2 class="text-xl font-bold text-sky-300 mb-1">Открыть день</h2><p class="muted mb-4">Готовый текст для Telegram и ручные заметки.</p><form class="flex flex-wrap gap-3 items-end my-3" action="/report"><div class="field"><label>Дата</label><input name="date" value="{{.Date}}"></div><button class="btn">Открыть отчёт</button></form><p class="muted"><a href="/report?date={{.PrevDate}}">← {{.PrevDate}}</a> · <a href="/report?date={{.NextDate}}">{{.NextDate}} →</a></p></div></div>`)
}

func webReport(w http.ResponseWriter, r *http.Request) {
	data := webBase(r)
	entries, err := wl.ReadEntries(wl.DayPath(wl.Home(), data.Date))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	doneItems := wl.EntryTexts(entries, wl.KindDone)
	planItems := wl.EntryTexts(entries, wl.KindPlan)
	blockerItems := wl.EntryTexts(entries, wl.KindBlocker)
	allItems := append(append([]string{}, doneItems...), append(planItems, blockerItems...)...)
	jira := wl.LoadJiraIssues(data.Config, allItems)
	planned := wl.OrderedGroupsWithJira(wl.GroupByTask(planItems), jira)
	data.Items = allItems
	data.Summary = wl.TelegramReport(data.Date, wl.GroupByTask(doneItems), jira, planned, wl.GroupByTask(blockerItems))
	render(w, "Отчёт "+data.Date, data, `<div class="grid grid-cols-1 lg:grid-cols-[1fr_1.2fr] gap-4 my-4"><div class="card"><p class="muted mb-4"><a href="/report?date={{.PrevDate}}">← {{.PrevDate}}</a> · <a href="/report?date={{.NextDate}}">{{.NextDate}} →</a></p><h2 class="text-xl font-bold text-sky-300 mb-3">Добавить заметку</h2><form class="flex flex-col gap-3" method="post" action="/add"><input type="hidden" name="date" value="{{.Date}}"><div class="field"><label>Тип</label><select name="type"><option value="done">✅ Что сделал</option><option value="plan">📌 Что буду делать</option><option value="blocker">⛔ Блокер</option></select></div><div class="field"><label>Текст</label><input name="text" placeholder="ABC-123 коротко что произошло"></div><button class="btn">Добавить</button></form><hr class="border-slate-800 my-5"><form class="flex flex-wrap gap-3" method="post" action="/generate"><input type="hidden" name="date" value="{{.Date}}"><button class="btn btn-green">✦ Groq summary</button><button class="btn btn2" name="prompt" value="1">Промпт</button></form></div><div class="card">{{if not .Items}}<p class="muted">Записей за день пока нет.</p>{{else}}<div class="flex items-center justify-between gap-3 mb-3"><h2 class="text-xl font-bold text-sky-300">Текст для Telegram</h2><button type="button" class="btn btn-copy" onclick="copyText('telegram-text', this)">📋 copy</button></div><pre id="telegram-text" class="bg-slate-950 border border-slate-800 rounded-xl p-4 overflow-auto text-slate-100">{{.Summary}}</pre>{{end}}</div></div>`)
}

func webPrompt(w http.ResponseWriter, r *http.Request) {
	data := webBase(r)
	entries, err := wl.ReadEntries(wl.DayPath(wl.Home(), data.Date))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	doneItems := wl.EntryTexts(entries, wl.KindDone)
	planItems := wl.EntryTexts(entries, wl.KindPlan)
	blockerItems := wl.EntryTexts(entries, wl.KindBlocker)
	allItems := append(append([]string{}, doneItems...), append(planItems, blockerItems...)...)
	jira := wl.LoadJiraIssues(data.Config, allItems)
	planned := wl.OrderedGroupsWithJira(wl.GroupByTask(planItems), jira)
	data.Items = allItems
	data.Groups = wl.GroupByTask(doneItems)
	data.Prompt = wl.BuildPrompt(data.Date, data.Groups, jira, planned, wl.GroupByTask(blockerItems))
	render(w, "Prompt "+data.Date, data, `<div class="card my-4">{{if not .Items}}<p class="muted">No entries.</p>{{else}}<pre class="bg-slate-950 border border-slate-800 rounded-xl p-4 overflow-auto text-slate-100">{{.Prompt}}</pre>{{end}}</div>`)
}

func webSummary(w http.ResponseWriter, r *http.Request) {
	data := webBase(r)
	b, err := wl.ReadSummary(wl.Home(), data.Date)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	data.Summary = b
	render(w, "Summary "+data.Date, data, `<div class="card my-4">{{if .Summary}}<div class="flex items-center justify-between gap-3 mb-3"><h2 class="text-xl font-bold text-sky-300">Groq summary</h2><button type="button" class="btn btn-copy" onclick="copyText('summary-text', this)">📋 copy</button></div><pre id="summary-text" class="bg-slate-950 border border-slate-800 rounded-xl p-4 overflow-auto text-slate-100">{{.Summary}}</pre>{{else}}<p class="muted">No saved summary.</p>{{end}}</div>`)
}

func webScanAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	since := r.FormValue("since")
	if since == "" {
		since = "14 days ago"
	}
	args := []string{"--since", since}
	if r.FormValue("force") != "" {
		args = append(args, "--force")
	}
	if err := cmdScan(args); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func webAddAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	date := r.FormValue("date")
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Error(w, "empty note", 400)
		return
	}
	kind := r.FormValue("type")
	args := []string{"--type", kind, text}
	if date != "" {
		args = []string{"--date", date, "--type", kind, text}
	}
	if err := cmdAdd(args); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	http.Redirect(w, r, "/report?date="+date, http.StatusSeeOther)
}

func webGenerateAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	date := r.FormValue("date")
	if date == "" {
		date = wl.PreviousWorkday(time.Now()).Format("2006-01-02")
	}
	if r.FormValue("prompt") != "" {
		http.Redirect(w, r, "/prompt?date="+date, http.StatusSeeOther)
		return
	}
	if err := cmdSummarize([]string{date, "--ai"}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/summary?date="+date, http.StatusSeeOther)
}

func webSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		cfg := wl.LoadConfig(wl.Home())
		cfg.ScanRoot = strings.TrimSpace(r.FormValue("scan_root"))
		cfg.ExcludeDirs = wl.SplitCSV(r.FormValue("exclude_dirs"))
		cfg.ExcludePaths = wl.SplitCSV(r.FormValue("exclude_paths"))
		cfg.GroqModel = strings.TrimSpace(r.FormValue("groq_model"))
		cfg.GroqBaseURL = strings.TrimRight(strings.TrimSpace(r.FormValue("groq_base_url")), "/")
		cfg.JiraURL = strings.TrimRight(strings.TrimSpace(r.FormValue("jira_url")), "/")
		cfg.JiraUser = strings.TrimSpace(r.FormValue("jira_user"))
		if token := strings.TrimSpace(r.FormValue("groq_token")); token != "" {
			if err := wl.StoreKeychainSecret("groq-api-token", token); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if token := strings.TrimSpace(r.FormValue("jira_token")); token != "" {
			if err := wl.StoreKeychainSecret("jira-api-token", token); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if err := wl.SaveConfig(wl.Home(), cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	data := webBase(r)
	render(w, "Настройки", data, `<form method="post" action="/setup" class="space-y-4 my-4"><div class="grid grid-cols-1 lg:grid-cols-3 gap-4"><section class="card"><div class="flex items-center justify-between mb-4"><h2 class="text-xl font-bold text-sky-300">Scan</h2><span class="badge badge-info">local</span></div><div class="field mb-3"><label>Root</label><input class="w-full" name="scan_root" value="{{.Config.ScanRoot}}" placeholder="/Users/avkorkin/prj"></div><div class="field mb-3"><label>Exclude dirs</label><input class="w-full" name="exclude_dirs" value="{{join .Config.ExcludeDirs}}" placeholder=".idea,.gradle,node_modules"></div><div class="field"><label>Exclude paths</label><input class="w-full" name="exclude_paths" value="{{join .Config.ExcludePaths}}" placeholder="/path/to/repo"></div></section><section class="card"><div class="flex items-center justify-between mb-4"><h2 class="text-xl font-bold text-sky-300">Groq</h2>{{if .GroqOK}}<span class="badge badge-ok">token ok</span>{{else}}<span class="badge badge-bad">no token</span>{{end}}</div><div class="field mb-3"><label>Model</label><input class="w-full" name="groq_model" value="{{.Config.GroqModel}}" placeholder="llama-3.3-70b-versatile"></div><div class="field mb-3"><label>Base URL</label><input class="w-full" name="groq_base_url" value="{{.Config.GroqBaseURL}}" placeholder="https://api.groq.com/openai/v1"></div><div class="field"><label>Token</label><input class="w-full" name="groq_token" type="password" placeholder="leave empty to keep current"><p class="muted text-xs mt-1">Stored in Keychain as groq-api-token.</p></div></section><section class="card"><div class="flex items-center justify-between mb-4"><h2 class="text-xl font-bold text-sky-300">Jira</h2>{{if .JiraTokenOK}}<span class="badge badge-ok">token ok</span>{{else}}<span class="badge badge-bad">no token</span>{{end}}</div><div class="field mb-3"><label>URL</label><input class="w-full" name="jira_url" value="{{.Config.JiraURL}}" placeholder="https://jira.example.com"></div><div class="field mb-3"><label>User/email</label><input class="w-full" name="jira_user" value="{{.Config.JiraUser}}" placeholder="empty = Bearer token"></div><div class="field"><label>Token</label><input class="w-full" name="jira_token" type="password" placeholder="leave empty to keep current"><p class="muted text-xs mt-1">Stored in Keychain as jira-api-token.</p></div></section></div><div class="card flex flex-col md:flex-row md:items-center justify-between gap-3"><div><h2 class="text-lg font-bold text-sky-300">Сохранить настройки</h2><p class="muted text-sm">Секреты пишутся в Keychain, не в config.json.</p></div><button class="btn">💾 Save setup</button></div></form>`)
}
