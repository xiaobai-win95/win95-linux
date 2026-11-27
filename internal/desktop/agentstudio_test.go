package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"win95linux/internal/agentcore"
	"win95linux/internal/ui"
)

func newAgentStudioTest(t *testing.T) (*Desktop, *AgentStudioApp, chan struct{}) {
	t.Helper()
	old := aiConfigPath
	aiConfigPath = filepath.Join(t.TempDir(), "ai.json")
	t.Cleanup(func() { aiConfigPath = old })
	wake := make(chan struct{}, 64)
	d := New(1024, 768, ModeDesktop, Host{Wake: func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}})
	return d, NewAgentStudioApp(d), wake
}

func pumpAgentPosted(t *testing.T, d *Desktop, wake <-chan struct{}, done func() bool) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for !done() {
		select {
		case <-wake:
			d.RunPosted()
		case <-timer.C:
			t.Fatal("timed out waiting for Agent Studio UI callbacks")
		}
	}
	d.RunPosted()
}

func pumpAgentModal(t *testing.T, d *Desktop, wake <-chan struct{}) *dialogApp {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for d.modal == nil {
		select {
		case <-wake:
			d.RunPosted()
		case <-timer.C:
			t.Fatal("timed out waiting for Agent Studio confirmation modal")
		}
	}
	dialog, ok := d.modal.App.(*dialogApp)
	if !ok {
		t.Fatalf("modal app = %T, want confirmation dialog", d.modal.App)
	}
	return dialog
}

func TestAgentStudioDefaultsInputAndSingleton(t *testing.T) {
	d, a, _ := newAgentStudioTest(t)
	a.win = d.Open(a)
	if a.permission != agentcore.PermissionAsk {
		t.Fatalf("default permission = %v, want ask", a.permission)
	}
	if a.root != agentStudioRoot {
		t.Fatalf("default root = %q", a.root)
	}
	if opened := d.openAgentStudio(); opened != a {
		t.Fatal("openAgentStudio did not reuse the existing app")
	}
	a.insertTaskText("读取 /root/note.txt")
	if string(a.task) != "读取 /root/note.txt" || a.taskCaret != len([]rune(a.task)) {
		t.Fatalf("task input = %q, caret = %d", a.task, a.taskCaret)
	}
	canvas := ui.NewCanvas(920, 640)
	a.Paint(d, nil, canvas, ui.Rect{W: 900, H: 620})
	if a.taskR.W <= 0 || a.traceR.W <= 0 || a.searchR.W <= 0 {
		t.Fatalf("studio layout is incomplete: task=%+v trace=%+v search=%+v", a.taskR, a.traceR, a.searchR)
	}
}

func TestAgentStudioConfirmPostsDialogAndWaitsWithoutDeadlock(t *testing.T) {
	d, a, wake := newAgentStudioTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan bool, 1)
	go func() {
		result <- a.confirmAction(ctx, agentcore.Action{
			Kind:   agentcore.ActionDeleteFile,
			Target: "/root/old.txt",
			Risk:   agentcore.RiskHigh,
		})
	}()
	dialog := pumpAgentModal(t, d, wake)
	if dialog.title != "Agent Studio 操作授权" || !strings.Contains(strings.Join(dialog.lines, "\n"), "完全访问") {
		t.Fatalf("danger confirmation is not explicit: %#v", dialog)
	}
	dialog.finish(1)
	select {
	case ok := <-result:
		if ok {
			t.Fatal("denied action was approved")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("confirmation worker deadlocked")
	}
}

func TestAgentStudioStopCancelsPendingConfirmation(t *testing.T) {
	d, a, wake := newAgentStudioTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.running = true
	result := make(chan bool, 1)
	go func() {
		result <- a.confirmAction(ctx, agentcore.Action{Kind: agentcore.ActionShell, Command: "rm -rf /root/x", Risk: agentcore.RiskHigh})
	}()
	pumpAgentModal(t, d, wake)
	a.stop(d)
	select {
	case ok := <-result:
		if ok {
			t.Fatal("cancelled action was approved")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled confirmation deadlocked")
	}
	if d.modal != nil {
		t.Fatal("stop left the confirmation modal open")
	}
}

func TestAgentCoreFullPermissionStillConfirmsDanger(t *testing.T) {
	called := false
	executor := &agentcore.Executor{
		Root:       t.TempDir(),
		Permission: agentcore.PermissionFull,
		Confirm: func(agentcore.Action) bool {
			called = true
			return false
		},
	}
	_, err := executor.Execute(context.Background(), agentcore.Action{
		Kind:    agentcore.ActionShell,
		Command: "rm -rf /root/example",
		Risk:    agentcore.RiskLow,
	})
	if err == nil || !called {
		t.Fatalf("full permission bypassed danger confirmation: called=%v err=%v", called, err)
	}
}

func TestAgentStudioExecutorRejectsSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	executor := &agentcore.Executor{Root: root, Permission: agentcore.PermissionFull, Confirm: func(agentcore.Action) bool { return true }}
	for _, action := range []agentcore.Action{
		{Kind: agentcore.ActionReadFile, Target: "link/secret.txt"},
		{Kind: agentcore.ActionWriteFile, Target: "link/new.txt", Content: "bad"},
		{Kind: agentcore.ActionDeleteFile, Target: "link/secret.txt"},
	} {
		if _, err := executor.Execute(t.Context(), action); err == nil {
			t.Errorf("Executor allowed a symlink escape: %+v", action)
		}
	}
	if data, err := os.ReadFile(outsideFile); err != nil || string(data) != "secret" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
	if _, err := agentcore.SafeTarget(root, "../outside"); err == nil {
		t.Fatal("lexical SafeTarget allowed parent traversal")
	}
}

func TestAgentStudioAutoDoesNotTrustCallerRisk(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	called := false
	executor := &agentcore.Executor{Root: root, Permission: agentcore.PermissionAuto, Confirm: func(agentcore.Action) bool {
		called = true
		return false
	}}
	result, err := executor.Execute(t.Context(), agentcore.Action{
		Kind: agentcore.ActionShell, Command: "touch " + strconv.Quote(marker), Risk: agentcore.RiskLow,
	})
	if err == nil || !result.Cancelled || !called {
		t.Fatalf("non-allowlisted shell was auto-approved: %+v, %v, called=%v", result, err, called)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unapproved shell created a file: %v", err)
	}
}

func TestAgentStudioCancellationWinsLateApproval(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := &agentcore.Executor{Root: root, Permission: agentcore.PermissionAsk, Confirm: func(agentcore.Action) bool {
		cancel()
		return true
	}}
	result, err := executor.Execute(ctx, agentcore.Action{Kind: agentcore.ActionWriteFile, Target: "late.txt", Content: "bad"})
	if err == nil || !result.Cancelled {
		t.Fatalf("late approval executed after cancellation: %+v, %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "late.txt")); !os.IsNotExist(err) {
		t.Fatalf("cancelled write created a file: %v", err)
	}
}

func TestLocalAgentPlanAndRiskClassification(t *testing.T) {
	actions, message, err := localAgentPlan("读取 /root/note.txt")
	if err != nil || message != "" || len(actions) != 1 || actions[0].Kind != agentcore.ActionReadFile {
		t.Fatalf("read plan = %+v / %q / %v", actions, message, err)
	}
	actions, _, _ = localAgentPlan("删除 /root/note.txt")
	if len(actions) != 1 || actions[0].Kind != agentcore.ActionDeleteFile || actions[0].Risk != agentcore.RiskHigh {
		t.Fatalf("delete plan = %+v", actions)
	}
	actions, _, _ = localAgentPlan("写入 /root/note.txt 内容为：hello")
	if len(actions) != 1 || actions[0].Content != "hello" || actions[0].Kind != agentcore.ActionWriteFile {
		t.Fatalf("write plan = %+v", actions)
	}
	actions, _, _ = localAgentPlan("联网搜索 Go 教程")
	if len(actions) != 1 || actions[0].Kind != agentcore.ActionSearch || !strings.Contains(actions[0].Query, "Go") {
		t.Fatalf("search plan = %+v", actions)
	}
	actions, _, _ = localAgentPlan("执行命令：rm /root/note.txt")
	if len(actions) != 1 || actions[0].Risk != agentcore.RiskHigh {
		t.Fatalf("mutating shell was not high risk: %+v", actions)
	}
	if isReadOnlyAgentShell("df -h; rm /root/x") {
		t.Fatal("compound mutating shell was classified read-only")
	}
	for _, command := range []string{"find /root -delete", "date -s 2030-01-01", "ip link set eth0 down"} {
		if isReadOnlyAgentShell(command) {
			t.Errorf("mutating shell was classified read-only: %q", command)
		}
	}
	if _, message, err := localAgentPlan("删除旧文件"); err != nil || message == "" {
		t.Fatalf("pathless delete plan = %q, %v", message, err)
	}
}

func TestAgentStudioSearchAndChangeVisualizationState(t *testing.T) {
	d, a, _ := newAgentStudioTest(t)
	results := []agentcore.SearchResult{{Title: "Result", URL: "https://example.invalid", Snippet: "snippet"}}
	a.applyEvent(agentcore.Event{Kind: agentcore.EventSearchStart, Text: "query"})
	a.applyEvent(agentcore.Event{Kind: agentcore.EventSearchResult, Results: results})
	if a.searchQuery != "query" || len(a.searchResults) != 1 || a.searchResults[0].Title != "Result" {
		t.Fatalf("search state = %q / %+v", a.searchQuery, a.searchResults)
	}
	a.applyEvent(agentcore.Event{Kind: agentcore.EventFileChange, Text: "修改文件", Target: "/root/a.txt"})
	a.applyEvent(agentcore.Event{Kind: agentcore.EventFileChange, Text: "删除文件", Target: "/root/a.txt"})
	if len(a.changes) != 1 || a.changes[0].Kind != agentcore.ActionDeleteFile {
		t.Fatalf("change state = %+v", a.changes)
	}
	canvas := ui.NewCanvas(920, 640)
	a.Paint(d, nil, canvas, ui.Rect{W: 900, H: 620})
}

func TestAgentStudioContextCompressionRequiresConfirmation(t *testing.T) {
	d, a, wake := newAgentStudioTest(t)
	a.context = []agentcore.ContextEntry{
		{Role: "user", Text: "task"},
		{Role: "tool", Text: strings.Repeat("a", 16000)},
		{Role: "tool", Text: strings.Repeat("b", 16000)},
	}
	before := len(a.context)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan bool, 1)
	go func() { result <- a.compressContext(ctx, false) }()
	dialog := pumpAgentModal(t, d, wake)
	if dialog.title != "上下文压缩确认" || len(a.context) != before {
		t.Fatalf("compression changed context before approval: title=%q entries=%d", dialog.title, len(a.context))
	}
	dialog.finish(1)
	if ok := <-result; ok {
		t.Fatal("denied context compression was applied")
	}
	if len(a.context) != before {
		t.Fatal("denied compression discarded context")
	}

	go func() { result <- a.compressContext(ctx, false) }()
	dialog = pumpAgentModal(t, d, wake)
	dialog.finish(0)
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-wake:
			d.RunPosted()
		case ok := <-result:
			if !ok {
				t.Fatal("approved context compression was not applied")
			}
			goto compressed
		case <-timer.C:
			t.Fatal("compression worker deadlocked")
		}
	}
compressed:
	if len(a.context) >= before || len(a.context) == 0 {
		t.Fatalf("compressed entries = %d, want between 1 and %d", len(a.context), before-1)
	}
	found := false
	for _, ev := range a.trace {
		if ev.Kind == agentcore.EventContextPrompt {
			found = true
		}
	}
	if !found {
		t.Fatal("compression was not recorded in the tool trace")
	}
}

func TestAgentStudioAutoWriteRecordsChangedFile(t *testing.T) {
	d, a, wake := newAgentStudioTest(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	a.root = root
	a.permission = agentcore.PermissionAuto
	a.task = []rune("写入 " + path + " 内容为：hello from studio")
	a.start(d)
	pumpAgentPosted(t, d, wake, func() bool { return !a.running })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("auto-approved write failed: %v", err)
	}
	if string(data) != "hello from studio" {
		t.Fatalf("written content = %q", data)
	}
	if len(a.changes) != 1 || a.changes[0].Target != path || a.changes[0].Kind != agentcore.ActionWriteFile {
		t.Fatalf("changed file list = %+v", a.changes)
	}
	if d.modal != nil {
		t.Fatal("auto-approved medium write unexpectedly required a modal")
	}
}

func TestAgentStudioRunsLocalReadTaskThroughPostedEvents(t *testing.T) {
	d, a, wake := newAgentStudioTest(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("hello from agent"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.root = root
	a.task = []rune("读取 " + path)
	a.start(d)
	pumpAgentPosted(t, d, wake, func() bool { return !a.running })
	if a.running {
		t.Fatal("local task stayed busy")
	}
	if !strings.Contains(a.status, "任务完成") {
		t.Fatalf("task status = %q", a.status)
	}
	var read, message bool
	for _, ev := range a.trace {
		read = read || ev.Kind == agentcore.EventReadFile
		message = message || (ev.Kind == agentcore.EventMessage && strings.Contains(ev.Text, "hello from agent"))
	}
	if !read || !message {
		t.Fatalf("tool trace lacks read/result events: %+v", a.trace)
	}
}
