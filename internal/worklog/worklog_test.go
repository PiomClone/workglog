package worklog

import (
	"strings"
	"testing"
)

func TestCleanTelegramItemRemovesTimeRepoAndSHA(t *testing.T) {
	got := CleanTelegramItem("10:15 `repo` `abc123def456` ABC-1 commit msg")
	want := "ABC-1 commit msg"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderPromptTemplate(t *testing.T) {
	prompt := RenderPromptTemplate("date={{date}}\ndone={{done}}\nplan={{planned}}\nblock={{blockers}}", "2026-06-23", map[string][]string{
		"ABC-1": {"10:15 ABC-1 done"},
	}, map[string]JiraIssue{
		"ABC-1": {Summary: "Task title", Status: "In Progress"},
	}, nil, map[string][]string{
		"ABC-2": {"11:00 ABC-2 blocked"},
	})
	for _, part := range []string{"date=2026-06-23", "ABC-1 — Task title [In Progress]", "посмотрю, что есть в спринте", "ABC-2 blocked"} {
		if !strings.Contains(prompt, part) {
			t.Fatalf("prompt does not contain %q:\n%s", part, prompt)
		}
	}
}

func TestReadEntriesDetectsKinds(t *testing.T) {
	home := t.TempDir()
	path := DayPath(home, "2026-06-23")
	if err := AppendUnderSection(path, "2026-06-23", "Manual", "- 10:00 ABC-1 done"); err != nil {
		t.Fatal(err)
	}
	if err := AppendUnderSection(path, "2026-06-23", "Plan", "- 11:00 ABC-1 plan"); err != nil {
		t.Fatal(err)
	}
	if err := AppendUnderSection(path, "2026-06-23", "Blockers", "- 12:00 ABC-1 blocked"); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(EntryTexts(entries, KindDone)) != 1 || len(EntryTexts(entries, KindPlan)) != 1 || len(EntryTexts(entries, KindBlocker)) != 1 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestTelegramReportDeduplicatesCleanedItems(t *testing.T) {
	report := TelegramReport("2026-06-23", map[string][]string{
		"ABC-1": {
			"10:00 `repo-a` `abc123def456` ABC-1 same message",
			"11:00 `repo-b` `def456abc123` ABC-1 same message",
		},
	}, nil, nil, nil)
	if strings.Count(report, "ABC-1 same message") != 1 {
		t.Fatalf("expected deduplicated report, got:\n%s", report)
	}
}

func TestPrefixTask(t *testing.T) {
	if got := PrefixTask("manual note", "ABC-1"); got != "ABC-1 manual note" {
		t.Fatalf("got %q", got)
	}
	if got := PrefixTask("ABC-2 manual note", "ABC-1"); got != "ABC-2 manual note" {
		t.Fatalf("got %q", got)
	}
}

func TestTaskKeysFromItems(t *testing.T) {
	got := TaskKeysFromItems([]string{"10:00 ABC-2 b", "11:00 ABC-1 a", "12:00 ABC-2 c", "no task"})
	want := []string{"ABC-1", "ABC-2"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}
