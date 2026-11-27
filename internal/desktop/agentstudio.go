package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"win95linux/internal/agentcore"
	"win95linux/internal/ui"
)

const (
	agentStudioRoot       = "/root"
	agentStudioContextMax = 6000
	agentStudioMaxActions = 12
)

type agentStudioChange struct {
	Target string
	Kind   agentcore.ActionKind
}

type agentStudioPending struct {
	win   *Window
	reply chan bool
}

func (p *agentStudioPending) respond(ok bool) {
	select {
	case p.reply <- ok:
	default:
	}
}

type agentStudioPlan struct {
	Message string                  `json:"message"`
	Actions []agentStudioPlanAction `json:"actions"`
}

type agentStudioPlanAction struct {
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Query       string `json:"query"`
	Content     string `json:"content"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

// AgentStudioApp is a local tool-execution surface. All mutable UI state is
// changed on the desktop thread; workers only exchange events and confirmation
// replies through Desktop.Post and channels.
type AgentStudioApp struct {
	Base
	d   *Desktop
	win *Window

	root       string
	permission agentcore.Permission
	searcher   *agentcore.Searcher

	task      []rune
	taskCaret int
	taskFocus bool
	status    string

	running     bool
	compressing bool
	closed      bool
	cancel      context.CancelFunc
	pending     *agentStudioPending

	trace         []agentcore.Event
	searchQuery   string
	searchResults []agentcore.SearchResult
	changes       []agentStudioChange
	context       []agentcore.ContextEntry

	traceScroll, searchScroll, fileScroll VScroll
	traceAutoBottom, searchAutoBottom     bool

	taskR                 ui.Rect
	permR                 [3]ui.Rect
	startR, stopR, clearR ui.Rect
	compressR             ui.Rect
	traceR, traceBarR     ui.Rect
	searchR, searchBarR   ui.Rect
	filesR, filesBarR     ui.Rect
	contextR              ui.Rect
}

func NewAgentStudioApp(d *Desktop) *AgentStudioApp {
	a := &AgentStudioApp{
		d:                d,
		root:             agentStudioRoot,
		permission:       agentcore.PermissionAsk,
		taskFocus:        true,
		traceAutoBottom:  true,
		searchAutoBottom: true,
	}
	a.status = a.d.tr("输入任务后开始。默认每次询问；危险操作始终二次确认。")
	return a
}

func (d *Desktop) openAgentStudio() *AgentStudioApp {
	for _, w := range d.windows {
		if a, ok := w.App.(*AgentStudioApp); ok {
			d.bringToFront(w)
			return a
		}
	}
	a := NewAgentStudioApp(d)
	a.win = d.Open(a)
	return a
}

func (a *AgentStudioApp) Title() string           { return "Agent Studio" }
func (a *AgentStudioApp) Icon() ui.IconKind       { return ui.IcoAI }
func (a *AgentStudioApp) DefaultSize() (int, int) { return 920, 640 }
func (a *AgentStudioApp) MinSize() (int, int)     { return 720, 520 }
func (a *AgentStudioApp) Resizable() bool         { return true }

func (a *AgentStudioApp) actionLabel(action agentcore.Action) string {
	switch action.Kind {
	case agentcore.ActionSearch:
		return a.d.trf("联网搜索：%s", action.Query)
	case agentcore.ActionReadFile:
		return a.d.trf("读取文件：%s", action.Target)
	case agentcore.ActionWriteFile:
		return a.d.trf("修改文件：%s", action.Target)
	case agentcore.ActionDeleteFile:
		return a.d.trf("删除文件：%s", action.Target)
	case agentcore.ActionShell:
		return a.d.trf("执行命令：%s", action.Command)
	default:
		return action.Description
	}
}

func (a *AgentStudioApp) translateActionLabel(label string) string {
	return translateActionLabelText(label, a.d.tr, a.d.trf)
}

func (a *AgentStudioApp) translateAgentText(text string) string {
	if translated := a.d.tr(text); translated != text {
		return translated
	}
	if detail, ok := strings.CutPrefix(text, "准备工具："); ok {
		return a.d.trf("准备工具：%s", a.translateActionLabel(detail))
	}
	if detail, ok := strings.CutPrefix(text, "模型规划失败，改用本地规则："); ok {
		return a.d.trf("模型规划失败，改用本地规则：%s", a.translateAgentText(detail))
	}
	var executed, changed int
	if n, err := fmt.Sscanf(text, "任务完成：执行 %d 个动作，变更 %d 个文件。", &executed, &changed); n == 2 && err == nil {
		return a.d.trf("任务完成：执行 %d 个动作，变更 %d 个文件。", executed, changed)
	}
	var kept, tokens int
	if n, err := fmt.Sscanf(text, "已压缩，保留最近 %d 条上下文（约 %d tokens）", &kept, &tokens); n == 2 && err == nil {
		return a.d.trf("已压缩，保留最近 %d 条上下文（约 %d tokens）", kept, tokens)
	}
	return translateAIError(text, a.d.tr, a.d.trf)
}

func (a *AgentStudioApp) translateAgentMessage(text string) string {
	for _, prefix := range []string{"读取结果：\n", "命令输出：\n"} {
		if detail, ok := strings.CutPrefix(text, prefix); ok {
			return a.d.trf(prefix+"%s", detail)
		}
	}
	if text == "命令执行完成（无输出）。" {
		return a.d.tr(text)
	}
	return text
}

func (a *AgentStudioApp) eventDisplayText(ev agentcore.Event) string {
	switch ev.Kind {
	case agentcore.EventThought:
		return a.d.trf("思考：%s", a.translateAgentText(ev.Text))
	case agentcore.EventSearchStart:
		return a.d.trf("联网搜索：%s", ev.Text)
	case agentcore.EventSearchResult:
		return a.d.trf("搜索完成：%d 条结果", len(ev.Results))
	case agentcore.EventReadFile:
		return a.d.trf("读取：%s", ev.Target)
	case agentcore.EventFileChange:
		return a.d.trf("已修改：%s", ev.Target)
	case agentcore.EventPermission:
		return a.d.trf("等待授权：%s", a.translateActionLabel(ev.Text))
	case agentcore.EventContextPrompt:
		return a.d.trf("上下文过长：%s", a.translateAgentText(ev.Text))
	case agentcore.EventMessage:
		return a.translateAgentMessage(ev.Text)
	case agentcore.EventError:
		return a.d.trf("错误：%s", a.translateAgentText(ev.Text))
	case agentcore.EventDone:
		if ev.Text == "" {
			return a.d.tr("完成")
		}
		return a.translateAgentText(ev.Text)
	default:
		return ev.Text
	}
}

func (a *AgentStudioApp) busy() bool { return a.running || a.compressing }

func (a *AgentStudioApp) Close() {
	a.closed = true
	if a.cancel != nil {
		a.cancel()
	}
	a.cancelPending()
}

func (a *AgentStudioApp) cancelPending() {
	p := a.pending
	if p == nil {
		return
	}
	a.pending = nil
	if p.win != nil {
		a.d.CloseWindowForced(p.win)
	}
	p.respond(false)
}

func (a *AgentStudioApp) insertTaskText(text string) {
	for _, r := range []rune(text) {
		a.task = append(a.task, 0)
		copy(a.task[a.taskCaret+1:], a.task[a.taskCaret:])
		a.task[a.taskCaret] = r
		a.taskCaret++
	}
	a.d.Invalidate()
}

func (a *AgentStudioApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Ctrl || ev.Alt {
		return false
	}
	switch ev.Name {
	case "Esc":
		if a.busy() {
			a.stop(d)
		} else {
			d.CloseWindow(w)
		}
		return true
	case "Enter":
		a.start(d)
		return true
	case "Backspace":
		if a.taskCaret > 0 {
			a.task = append(a.task[:a.taskCaret-1], a.task[a.taskCaret:]...)
			a.taskCaret--
			d.Invalidate()
		}
		return true
	case "Delete":
		if a.taskCaret < len(a.task) {
			a.task = append(a.task[:a.taskCaret], a.task[a.taskCaret+1:]...)
			d.Invalidate()
		}
		return true
	case "Left":
		if a.taskCaret > 0 {
			a.taskCaret--
			d.Invalidate()
		}
		return true
	case "Right":
		if a.taskCaret < len(a.task) {
			a.taskCaret++
			d.Invalidate()
		}
		return true
	case "Home":
		a.taskCaret = 0
		d.Invalidate()
		return true
	case "End":
		a.taskCaret = len(a.task)
		d.Invalidate()
		return true
	case "PgUp":
		a.traceAutoBottom = false
		a.traceScroll.ScrollBy(-maxInt(1, a.traceScroll.Visible/3))
		d.Invalidate()
		return true
	case "PgDn":
		a.traceAutoBottom = false
		a.traceScroll.ScrollBy(maxInt(1, a.traceScroll.Visible/3))
		d.Invalidate()
		return true
	}
	if ev.Rune >= 32 {
		a.taskFocus = true
		a.insertTaskText(string(ev.Rune))
		return true
	}
	return false
}

func (a *AgentStudioApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseWheel {
		changed := false
		if a.traceR.Contains(ev.X, ev.Y) {
			a.traceAutoBottom = false
			changed, _ = a.traceScroll.Mouse(ev, a.traceBarR)
		} else if a.searchR.Contains(ev.X, ev.Y) {
			a.searchAutoBottom = false
			changed, _ = a.searchScroll.Mouse(ev, a.searchBarR)
		} else if a.filesR.Contains(ev.X, ev.Y) {
			changed, _ = a.fileScroll.Mouse(ev, a.filesBarR)
		}
		if changed {
			d.Invalidate()
		}
		return
	}
	if ev.Button != 1 {
		return
	}
	if ev.Kind == MouseMove || ev.Kind == MouseUp {
		changed := false
		if c, _ := a.traceScroll.Mouse(ev, a.traceBarR); c {
			changed = true
		}
		if c, _ := a.searchScroll.Mouse(ev, a.searchBarR); c {
			changed = true
		}
		if c, _ := a.fileScroll.Mouse(ev, a.filesBarR); c {
			changed = true
		}
		if changed {
			d.Invalidate()
		}
		return
	}
	if ev.Kind != MouseDown {
		return
	}
	switch {
	case a.taskR.Contains(ev.X, ev.Y):
		a.taskFocus = true
	case !a.busy() && a.permR[0].Contains(ev.X, ev.Y):
		a.permission = agentcore.PermissionAsk
	case !a.busy() && a.permR[1].Contains(ev.X, ev.Y):
		a.permission = agentcore.PermissionAuto
	case !a.busy() && a.permR[2].Contains(ev.X, ev.Y):
		a.permission = agentcore.PermissionFull
	case a.startR.Contains(ev.X, ev.Y):
		a.start(d)
	case a.stopR.Contains(ev.X, ev.Y):
		a.stop(d)
	case a.clearR.Contains(ev.X, ev.Y):
		a.clear(d)
	case a.compressR.Contains(ev.X, ev.Y):
		a.compressNow(d)
	default:
		if c, _ := a.traceScroll.Mouse(ev, a.traceBarR); c {
			a.traceAutoBottom = false
		} else if c, _ := a.searchScroll.Mouse(ev, a.searchBarR); c {
			a.searchAutoBottom = false
		} else if c, _ := a.fileScroll.Mouse(ev, a.filesBarR); c {
			// The file list has its own scrollbar.
		}
	}
	d.Invalidate()
}

func (a *AgentStudioApp) clear(d *Desktop) {
	if a.busy() {
		return
	}
	a.task = nil
	a.taskCaret = 0
	a.trace = nil
	a.searchQuery = ""
	a.searchResults = nil
	a.changes = nil
	a.context = nil
	a.traceScroll = VScroll{}
	a.searchScroll = VScroll{}
	a.fileScroll = VScroll{}
	a.status = a.d.tr("已清空任务与轨迹。")
	d.Invalidate()
}

func (a *AgentStudioApp) stop(d *Desktop) {
	if !a.busy() {
		return
	}
	if a.cancel != nil {
		a.cancel()
	}
	a.cancelPending()
	a.status = a.d.tr("正在停止…")
	d.Invalidate()
}

func (a *AgentStudioApp) start(d *Desktop) {
	if a.busy() {
		return
	}
	task := strings.TrimSpace(string(a.task))
	if task == "" {
		a.status = a.d.tr("请先输入任务。")
		d.Invalidate()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.running = true
	a.trace = nil
	a.searchQuery = ""
	a.searchResults = nil
	a.changes = nil
	a.context = []agentcore.ContextEntry{{Role: "user", Text: task}}
	a.traceScroll = VScroll{}
	a.searchScroll = VScroll{}
	a.fileScroll = VScroll{}
	a.traceAutoBottom = true
	a.searchAutoBottom = true
	a.applyEvent(agentcore.Event{Kind: agentcore.EventThought, Text: a.d.tr("分析任务并选择工具。")})
	cfg := loadAIConfig()
	permission := a.permission
	d.Invalidate()
	go a.run(ctx, cfg, task, permission)
}

func (a *AgentStudioApp) run(ctx context.Context, cfg aiConfig, task string, permission agentcore.Permission) {
	defer a.d.Post(func() {
		if a.closed {
			return
		}
		a.running = false
		a.cancel = nil
		if ctx.Err() != nil {
			a.status = a.d.tr("任务已停止。")
		}
		a.d.Invalidate()
	})

	actions, message, err := a.plan(ctx, cfg, task)
	if err != nil {
		if !a.emit(ctx, agentcore.Event{Kind: agentcore.EventError, Text: err.Error()}) {
			return
		}
		_ = a.emit(ctx, agentcore.Event{Kind: agentcore.EventDone, Text: a.d.tr("任务未执行。")})
		return
	}
	if message != "" && !a.emit(ctx, agentcore.Event{Kind: agentcore.EventMessage, Text: message}) {
		return
	}
	if len(actions) == 0 {
		_ = a.emit(ctx, agentcore.Event{Kind: agentcore.EventDone, Text: a.d.tr("没有需要执行的动作。")})
		return
	}

	executor := &agentcore.Executor{
		Root:       a.root,
		Permission: permission,
		Searcher:   a.searcher,
		Confirm: func(action agentcore.Action) bool {
			// Execute invokes this on its worker goroutine. confirmAction posts
			// the dialog to the UI thread and waits on a buffered reply channel.
			return a.confirmAction(ctx, action)
		},
	}
	executed := 0
	for _, action := range actions {
		if ctx.Err() != nil {
			return
		}
		if !a.emit(ctx, agentcore.Event{Kind: agentcore.EventThought, Text: a.d.trf("准备工具：%s", a.actionLabel(action))}) {
			return
		}
		switch action.Kind {
		case agentcore.ActionSearch:
			if !a.emit(ctx, agentcore.Event{Kind: agentcore.EventSearchStart, Text: action.Query}) {
				return
			}
		case agentcore.ActionReadFile:
			if !a.emit(ctx, agentcore.Event{Kind: agentcore.EventReadFile, Target: action.Target}) {
				return
			}
		}

		var result agentcore.ExecResult
		result, err = executor.Execute(ctx, action)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			text := err.Error()
			if strings.TrimSpace(result.Text) != "" {
				text += "\n" + a.capAgentText(result.Text, 4000)
			}
			text = translateCommandTruncationText(text, a.d.tr)
			if !a.emit(ctx, agentcore.Event{Kind: agentcore.EventError, Text: text}) {
				return
			}
			_ = a.emit(ctx, agentcore.Event{Kind: agentcore.EventDone, Text: a.d.tr("任务已中止。")})
			return
		}
		if !a.recordResult(ctx, action, result) {
			return
		}
		executed++
		if !a.compressContext(ctx, true) {
			if ctx.Err() != nil {
				return
			}
			_ = a.emit(ctx, agentcore.Event{Kind: agentcore.EventThought, Text: a.d.tr("用户取消压缩，保留完整上下文。")})
		}
	}
	summary := a.d.trf("任务完成：执行 %d 个动作，变更 %d 个文件。", executed, len(a.changes))
	_ = a.emit(ctx, agentcore.Event{Kind: agentcore.EventDone, Text: summary})
}

func (a *AgentStudioApp) plan(ctx context.Context, cfg aiConfig, task string) ([]agentcore.Action, string, error) {
	if strings.TrimSpace(cfg.APIKey) != "" && cfg.Provider != "local" {
		actions, message, err := planAgentTaskWithAIWithText(cfg, task, a.d.tr, a.d.trf)
		if err == nil {
			return actions, message, nil
		}
		if !a.emit(ctx, agentcore.Event{Kind: agentcore.EventThought, Text: a.d.trf("模型规划失败，改用本地规则：%s", err.Error())}) {
			return nil, "", ctx.Err()
		}
	}
	actions, message, err := localAgentPlanWithText(task, a.d.tr)
	return actions, message, err
}

func (a *AgentStudioApp) emit(ctx context.Context, ev agentcore.Event) bool {
	done := make(chan struct{})
	a.d.Post(func() {
		defer close(done)
		if a.closed {
			return
		}
		a.applyEvent(ev)
	})
	select {
	case <-done:
		return !a.closed
	case <-ctx.Done():
		return false
	}
}

func (a *AgentStudioApp) applyEvent(ev agentcore.Event) {
	a.trace = append(a.trace, ev)
	if len(a.trace) > 200 {
		a.trace = append([]agentcore.Event(nil), a.trace[len(a.trace)-200:]...)
	}
	a.traceAutoBottom = true
	switch ev.Kind {
	case agentcore.EventSearchStart:
		a.searchQuery = ev.Text
		a.searchResults = nil
		a.searchAutoBottom = true
		a.status = a.d.trf("正在搜索：%s", ev.Text)
	case agentcore.EventSearchResult:
		a.searchResults = append([]agentcore.SearchResult(nil), ev.Results...)
		a.searchAutoBottom = true
		a.status = a.d.trf("搜索完成：%d 条结果。", len(ev.Results))
	case agentcore.EventFileChange:
		kind := agentcore.ActionWriteFile
		if ev.Text == "删除文件" {
			kind = agentcore.ActionDeleteFile
		}
		found := false
		for i := range a.changes {
			if a.changes[i].Target == ev.Target {
				a.changes[i].Kind = kind
				found = true
				break
			}
		}
		if !found {
			a.changes = append(a.changes, agentStudioChange{Target: ev.Target, Kind: kind})
		}
		a.status = a.d.trf("文件已变更：%s", ev.Target)
	case agentcore.EventThought:
		a.status = a.d.tr("正在思考 / 执行…")
	case agentcore.EventPermission:
		a.status = a.d.tr("等待用户授权…")
	case agentcore.EventContextPrompt:
		a.status = a.translateAgentText(ev.Text)
	case agentcore.EventMessage:
		a.status = a.d.tr("正在处理工具结果…")
	case agentcore.EventError:
		a.status = a.d.tr("任务出错。")
	case agentcore.EventDone:
		if ev.Text != "" {
			a.status = a.translateAgentText(ev.Text)
		} else {
			a.status = a.d.tr("任务完成。")
		}
	}
	if role, text, ok := agentEventContext(ev); ok {
		a.context = append(a.context, agentcore.ContextEntry{Role: role, Text: capAgentText(text, 12000)})
	}
	a.d.Invalidate()
}

// agentEventContext builds model input, not display text. Keep its canonical
// labels in Chinese and translate only at the UI boundary.
func agentEventContext(ev agentcore.Event) (string, string, bool) {
	switch ev.Kind {
	case agentcore.EventThought:
		return "assistant", ev.Text, true
	case agentcore.EventSearchStart:
		return "tool", "搜索：" + ev.Text, true
	case agentcore.EventSearchResult:
		var b strings.Builder
		for i, result := range ev.Results {
			fmt.Fprintf(&b, "%d. %s\n%s\n%s\n", i+1, result.Title, result.URL, result.Snippet)
		}
		return "tool", b.String(), true
	case agentcore.EventReadFile:
		return "tool", "读取文件：" + ev.Target, true
	case agentcore.EventFileChange:
		return "tool", ev.String() + " " + ev.Target, true
	case agentcore.EventPermission:
		return "assistant", ev.Text, true
	case agentcore.EventContextPrompt:
		return "assistant", ev.Text, true
	case agentcore.EventMessage:
		return "tool", ev.Text, true
	case agentcore.EventError:
		return "tool", "错误：" + ev.Text, true
	case agentcore.EventDone:
		return "assistant", ev.Text, true
	default:
		return "", "", false
	}
}

func (a *AgentStudioApp) recordResult(ctx context.Context, action agentcore.Action, result agentcore.ExecResult) bool {
	switch action.Kind {
	case agentcore.ActionSearch:
		return a.emit(ctx, agentcore.Event{Kind: agentcore.EventSearchResult, Text: action.Query, Results: result.Results})
	case agentcore.ActionReadFile:
		return a.emit(ctx, agentcore.Event{Kind: agentcore.EventMessage, Text: a.d.trf("读取结果：\n%s", a.capAgentText(result.Text, 12000))})
	case agentcore.ActionWriteFile, agentcore.ActionDeleteFile:
		// Keep these labels canonical because applyEvent uses them to distinguish
		// file deletions from writes.
		label := "修改文件"
		if action.Kind == agentcore.ActionDeleteFile {
			label = "删除文件"
		}
		return a.emit(ctx, agentcore.Event{Kind: agentcore.EventFileChange, Text: label, Target: result.Target})
	case agentcore.ActionShell:
		text := a.d.tr("命令执行完成（无输出）。")
		if strings.TrimSpace(result.Text) != "" {
			text = a.d.trf("命令输出：\n%s", a.capAgentText(result.Text, 12000))
		}
		return a.emit(ctx, agentcore.Event{Kind: agentcore.EventMessage, Text: text})
	default:
		return true
	}
}

func (a *AgentStudioApp) confirmAction(ctx context.Context, action agentcore.Action) bool {
	danger := action.Risk == agentcore.RiskHigh || action.Kind == agentcore.ActionDeleteFile || agentcore.IsDangerousCommand(action.Command)
	text := a.actionLabel(action)
	if action.Description != "" {
		text += "\n\n" + action.Description
	}
	if action.Target != "" {
		text += "\n" + a.d.trf("目标：%s", action.Target)
	}
	if action.Content != "" {
		text += "\n" + a.d.trf("内容预览：\n%s", a.capAgentText(action.Content, 1200))
	}
	if danger {
		text += "\n\n" + a.d.tr("这是危险操作。即使选择“完全访问”，也必须再次确认；不会自动跳过。")
	}
	if !a.emit(ctx, agentcore.Event{Kind: agentcore.EventPermission, Text: a.actionLabel(action)}) {
		return false
	}
	return a.confirmOnUI(ctx, a.d.tr("Agent Studio 操作授权"), text, danger, a.d.tr("仍然允许"), a.d.tr("取消"))
}

// confirmOnUI must only be called from a worker goroutine. The posted callback
// creates the modal and returns immediately; only the worker waits on reply.
func (a *AgentStudioApp) confirmOnUI(ctx context.Context, title, text string, danger bool, allow, cancel string) bool {
	if ctx.Err() != nil || a.d == nil {
		return false
	}
	reply := make(chan bool, 1)
	a.d.Post(func() {
		if a.closed || ctx.Err() != nil {
			reply <- false
			return
		}
		p := &agentStudioPending{reply: reply}
		a.pending = p
		icon := IconQuestion
		if danger {
			icon = IconWarning
		}
		p.win = a.d.MessageBox(title, text, icon, []string{allow, cancel}, func(i int) {
			if a.pending == p {
				a.pending = nil
			}
			p.respond(i == 0)
		})
	})
	select {
	case ok := <-reply:
		return ok && ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

func (a *AgentStudioApp) compressNow(d *Desktop) {
	if a.busy() || len(a.context) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.compressing = true
	needed := agentContextTokens(a.context) > agentStudioContextMax
	a.status = a.d.tr("等待上下文压缩确认…")
	d.Invalidate()
	go func() {
		ok := a.compressContext(ctx, false)
		a.d.Post(func() {
			if a.closed {
				return
			}
			a.compressing = false
			a.cancel = nil
			if ctx.Err() != nil {
				a.status = a.d.tr("上下文压缩已停止。")
			} else if !ok {
				a.status = a.d.tr("已取消上下文压缩。")
			} else if needed {
				a.status = a.d.tr("上下文已压缩。")
			} else {
				a.status = a.d.tr("上下文无需压缩。")
			}
			d.Invalidate()
		})
	}()
}

func (a *AgentStudioApp) compressContext(ctx context.Context, automatic bool) bool {
	if automatic && agentContextTokens(a.context) <= agentStudioContextMax {
		return true
	}
	next, note := agentcore.CompressContext(a.context, agentStudioContextMax)
	if note == "" {
		return true
	}
	text := a.d.trf("当前约 %d tokens，压缩后保留最近 %d 条（约 %d tokens）。\n\n较早的上下文将被丢弃，文件变更与搜索轨迹仍保留在界面中。",
		agentContextTokens(a.context), len(next), contextEntriesTokens(next))
	ok := a.confirmOnUI(ctx, a.d.tr("上下文压缩确认"), text, false, a.d.tr("压缩"), a.d.tr("取消"))
	if !ok || ctx.Err() != nil {
		return false
	}
	done := make(chan struct{})
	a.d.Post(func() {
		defer close(done)
		if a.closed || ctx.Err() != nil {
			return
		}
		a.context = next
		a.applyEvent(agentcore.Event{Kind: agentcore.EventContextPrompt, Text: note})
	})
	select {
	case <-done:
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

func contextEntriesTokens(entries []agentcore.ContextEntry) int {
	total := 0
	for _, entry := range entries {
		total += agentcore.ApproxTokens(entry.Text) + 4
	}
	return total
}

func agentContextTokens(entries []agentcore.ContextEntry) int {
	return contextEntriesTokens(entries)
}

func planAgentTaskWithAIWithText(cfg aiConfig, task string, tr func(string) string, trf func(string, ...any) string) ([]agentcore.Action, string, error) {
	const prompt = `你是 Win95 Linux Agent Studio 的规划器。只输出 JSON，不要 Markdown：
{"message":"简要计划","actions":[{"kind":"search|read|write|delete|shell","target":"工作区路径","query":"搜索词","content":"写入内容","command":"shell命令","description":"用途"}]}
文件路径必须在 /root 内。不要执行任务，只规划。危险动作必须明确。最多 12 个动作。`
	reply, err := aiCompleteWithText(cfg, []aiChatMessage{{Role: "system", Content: prompt}, {Role: "user", Content: task}}, tr, trf)
	if err != nil {
		return nil, "", err
	}
	reply = strings.TrimSpace(reply)
	if start := strings.Index(reply, "{"); start >= 0 {
		if end := strings.LastIndex(reply, "}"); end > start {
			reply = reply[start : end+1]
		}
	}
	var plan agentStudioPlan
	if err := json.Unmarshal([]byte(reply), &plan); err != nil {
		return nil, "", fmt.Errorf("%s%w", tr("无法解析模型计划："), err)
	}
	if len(plan.Actions) > agentStudioMaxActions {
		return nil, "", fmt.Errorf("%s", trf("模型计划包含过多动作（最多 %d）", agentStudioMaxActions))
	}
	actions := make([]agentcore.Action, 0, len(plan.Actions))
	for _, raw := range plan.Actions {
		action, err := agentActionFromPlanWithText(raw, tr, trf)
		if err != nil {
			return nil, "", err
		}
		actions = append(actions, action)
	}
	return actions, strings.TrimSpace(plan.Message), nil
}

func planAgentTaskWithAI(cfg aiConfig, task string) ([]agentcore.Action, string, error) {
	return planAgentTaskWithAIWithText(cfg, task, untranslatedAIText, untranslatedAITextf)
}

func agentActionFromPlanWithText(
	raw agentStudioPlanAction,
	tr func(string) string,
	trf func(string, ...any) string,
) (agentcore.Action, error) {
	action := agentcore.Action{
		Target:      strings.TrimSpace(raw.Target),
		Query:       strings.TrimSpace(raw.Query),
		Content:     raw.Content,
		Command:     strings.TrimSpace(raw.Command),
		Description: strings.TrimSpace(raw.Description),
	}
	switch strings.ToLower(strings.TrimSpace(raw.Kind)) {
	case "search":
		action.Kind = agentcore.ActionSearch
		action.Risk = agentcore.RiskLow
		if action.Query == "" {
			return action, fmt.Errorf("%s", tr("搜索动作缺少查询词"))
		}
	case "read":
		action.Kind = agentcore.ActionReadFile
		action.Risk = agentcore.RiskLow
	case "write":
		action.Kind = agentcore.ActionWriteFile
		action.Risk = agentcore.RiskMedium
	case "delete":
		action.Kind = agentcore.ActionDeleteFile
		action.Risk = agentcore.RiskHigh
	case "shell":
		action.Kind = agentcore.ActionShell
		action.Risk = agentcore.RiskHigh
		if isReadOnlyAgentShell(action.Command) {
			action.Risk = agentcore.ExplainCommand(action.Command).Risk
		}
		if action.Command == "" {
			return action, fmt.Errorf("%s", tr("命令动作为空"))
		}
	default:
		return action, fmt.Errorf("%s", trf("模型返回了未知动作：%q", raw.Kind))
	}
	if action.Kind != agentcore.ActionSearch && action.Kind != agentcore.ActionShell && action.Target == "" {
		return action, fmt.Errorf("%s", tr("文件动作缺少目标路径"))
	}
	return action, nil
}

func agentActionFromPlan(raw agentStudioPlanAction) (agentcore.Action, error) {
	return agentActionFromPlanWithText(raw, untranslatedAIText, untranslatedAITextf)
}

func isReadOnlyAgentShell(command string) bool {
	return agentcore.IsSafeCommand(command)
}

var agentStudioPathRe = regexp.MustCompile(`/[^\s"'，。；;]+`)

func localAgentPlanWithText(task string, tr func(string) string) ([]agentcore.Action, string, error) {
	task = strings.TrimSpace(task)
	low := strings.ToLower(task)
	path := agentStudioPathRe.FindString(task)
	if strings.Contains(low, "联网") || strings.Contains(low, "网上搜索") || strings.Contains(low, "搜索资料") ||
		(strings.Contains(low, "搜索") && !strings.Contains(low, "文件") && !strings.Contains(low, "文件夹")) {
		query := task
		for _, word := range []string{"请帮我", "帮我", "联网搜索", "网上搜索", "搜索一下", "搜索资料", "搜索", "资料", "关于"} {
			query = strings.ReplaceAll(query, word, " ")
		}
		query = strings.TrimSpace(strings.Trim(query, " ：:，。!！?"))
		if query == "" {
			query = task
		}
		return []agentcore.Action{{Kind: agentcore.ActionSearch, Query: query, Risk: agentcore.RiskLow}}, "", nil
	}
	if strings.Contains(low, "删除") || strings.Contains(low, "移除") {
		if path == "" {
			return nil, tr("请提供要删除的 /root 内路径。"), nil
		}
		return []agentcore.Action{{Kind: agentcore.ActionDeleteFile, Target: path, Risk: agentcore.RiskHigh}}, "", nil
	}
	if strings.Contains(low, "写入") || strings.Contains(low, "创建文件") || strings.Contains(low, "保存到") {
		if path == "" {
			return nil, tr("请提供要写入的 /root 内路径。"), nil
		}
		content := ""
		for _, marker := range []string{"内容为：", "内容为:", "内容：", "内容:", "写入内容："} {
			if i := strings.Index(task, marker); i >= 0 {
				content = strings.TrimSpace(task[i+len(marker):])
				break
			}
		}
		if content == "" {
			return nil, tr("请用“内容为：”提供要写入的完整内容。"), nil
		}
		return []agentcore.Action{{Kind: agentcore.ActionWriteFile, Target: path, Content: content, Risk: agentcore.RiskMedium}}, "", nil
	}
	if strings.Contains(low, "读取") || strings.Contains(low, "查看文件") || strings.Contains(low, "打开文件") {
		if path == "" {
			return nil, tr("请提供要读取的 /root 内路径。"), nil
		}
		return []agentcore.Action{{Kind: agentcore.ActionReadFile, Target: path, Risk: agentcore.RiskLow}}, "", nil
	}
	command := ""
	for _, prefix := range []string{"执行命令：", "执行命令:", "运行命令：", "运行命令:"} {
		if strings.HasPrefix(task, prefix) {
			command = strings.TrimSpace(strings.TrimPrefix(task, prefix))
			break
		}
	}
	if command == "" {
		command, _ = localCommand(task)
	}
	if command != "" {
		risk := agentcore.RiskHigh
		if isReadOnlyAgentShell(command) {
			risk = agentcore.ExplainCommand(command).Risk
		}
		return []agentcore.Action{{Kind: agentcore.ActionShell, Command: command, Risk: risk}}, "", nil
	}
	return nil, tr("本地规则无法规划这个任务。可输入“联网搜索…”、“读取 /root/…”、“写入 /root/… 内容为：…”或配置模型 API Key。"), nil
}

func localAgentPlan(task string) ([]agentcore.Action, string, error) {
	return localAgentPlanWithText(task, untranslatedAIText)
}

func capAgentTextWithText(text string, maxRunes int, tr func(string) string) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "\n" + tr("…（已截断）")
}

func (a *AgentStudioApp) capAgentText(text string, maxRunes int) string {
	return capAgentTextWithText(text, maxRunes, a.d.tr)
}

func capAgentText(text string, maxRunes int) string {
	return capAgentTextWithText(text, maxRunes, untranslatedAIText)
}

type agentDisplayLine struct {
	text string
	col  uint32
	bold bool
}

func (a *AgentStudioApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	header := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 46}
	c.TitleBar(header, 0x1F3864, 0x2F5496)
	ui.DrawIcon32(c, ui.IcoAI, header.X+10, header.Y+7, false)
	f.TextBold(c, header.X+52, header.Y+7, "Agent Studio", ui.ColWhite)
	f.Text(c, header.X+52, header.Y+26,
		a.d.trf("工作区 /root · %s · 危险操作不自动放行", a.d.tr(a.permission.String())), 0xC8DCFF)

	taskY := header.Bottom() + 8
	f.Text(c, r.X+10, taskY, a.d.tr("任务："), ui.ColText)
	a.taskR = ui.Rect{X: r.X + 58, Y: taskY - 4, W: r.W - 68, H: 28}
	drawAIField(c, f, a.taskR, string(a.task), a.taskFocus, false)
	if a.taskFocus {
		d.imeFocus(w, a.taskR, a.insertTaskText)
	}

	permY := a.taskR.Bottom() + 10
	permLabels := []string{
		a.d.tr(agentcore.PermissionAsk.String()),
		a.d.tr(agentcore.PermissionAuto.String()),
		a.d.tr(agentcore.PermissionFull.String()),
	}
	for i, label := range permLabels {
		a.permR[i] = ui.Rect{X: r.X + 10 + i*106, Y: permY, W: 100, H: 26}
		ui.Button(c, a.permR[i], label, a.permission == agentcore.Permission(i), !a.busy())
	}
	a.startR = ui.Rect{X: r.X + 338, Y: permY, W: 90, H: 26}
	a.stopR = ui.Rect{X: r.X + 436, Y: permY, W: 80, H: 26}
	a.clearR = ui.Rect{X: r.X + 524, Y: permY, W: 80, H: 26}
	ui.Button(c, a.startR, a.d.tr("开始"), a.running, !a.busy() && len(a.task) > 0)
	ui.Button(c, a.stopR, a.d.tr("停止"), false, a.busy())
	ui.Button(c, a.clearR, a.d.tr("清空"), false, !a.busy())
	f.Text(c, r.X+10, permY+30,
		a.d.tr("自动审批仍会确认危险命令、删除与高风险动作；完全访问也不是免确认模式。"), ui.ColTextDis)

	body := ui.Rect{X: r.X + 10, Y: permY + 50, W: r.W - 20, H: r.Bottom() - (permY + 50) - 28}
	if body.H < 1 {
		return
	}
	leftW := (body.W - 10) * 58 / 100
	rightX := body.X + leftW + 10
	rightW := body.Right() - rightX
	a.traceR = ui.Rect{X: body.X, Y: body.Y, W: leftW, H: body.H}
	searchH := body.H * 45 / 100
	filesH := body.H * 27 / 100
	a.searchR = ui.Rect{X: rightX, Y: body.Y, W: rightW, H: searchH}
	a.filesR = ui.Rect{X: rightX, Y: a.searchR.Bottom() + 10, W: rightW, H: filesH}
	a.contextR = ui.Rect{X: rightX, Y: a.filesR.Bottom() + 10, W: rightW, H: body.Bottom() - a.filesR.Bottom() - 10}

	a.paintTrace(c, f)
	a.paintSearch(c, f)
	a.paintFiles(c, f)
	a.paintContext(c, f)
	f.TextClip(c, r.X+10, r.Bottom()-20, r.W-20, a.status, ui.ColText)
}

func (a *AgentStudioApp) paintTrace(c *ui.Canvas, f *ui.Font) {
	ui.GroupBox(c, a.traceR, a.d.tr("思考 / 工具轨迹"))
	a.traceBarR = ui.Rect{X: a.traceR.Right() - 18, Y: a.traceR.Y + 10, W: 16, H: a.traceR.H - 20}
	view := ui.Rect{X: a.traceR.X + 8, Y: a.traceR.Y + 10, W: a.traceR.W - 34, H: a.traceR.H - 20}
	var lines []agentDisplayLine
	for _, ev := range a.trace {
		text := a.eventDisplayText(ev)
		col := ui.ColText
		switch ev.Kind {
		case agentcore.EventError:
			col = ui.ColRed
		case agentcore.EventPermission, agentcore.EventContextPrompt:
			col = ui.ColNavy
		case agentcore.EventFileChange:
			col = ui.ColGreen
		case agentcore.EventSearchStart, agentcore.EventSearchResult:
			col = ui.ColBlue
		}
		wrapped := wrapText(text, maxInt(1, view.W))
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for i, line := range wrapped {
			lines = append(lines, agentDisplayLine{text: line, col: col, bold: i == 0})
		}
		lines = append(lines, agentDisplayLine{})
	}
	if len(a.trace) == 0 {
		lines = append(lines, agentDisplayLine{text: a.d.tr("尚未开始。输入任务后，这里会显示思考、搜索与工具调用。"), col: ui.ColTextDis})
	}
	paintAgentList(c, f, view, a.traceBarR, lines, &a.traceScroll, a.traceAutoBottom)
}

func (a *AgentStudioApp) paintSearch(c *ui.Canvas, f *ui.Font) {
	ui.GroupBox(c, a.searchR, a.d.tr("搜索可视化"))
	a.searchBarR = ui.Rect{X: a.searchR.Right() - 18, Y: a.searchR.Y + 10, W: 16, H: a.searchR.H - 20}
	view := ui.Rect{X: a.searchR.X + 8, Y: a.searchR.Y + 10, W: a.searchR.W - 34, H: a.searchR.H - 20}
	var lines []agentDisplayLine
	if a.searchQuery != "" {
		lines = append(lines, agentDisplayLine{text: a.d.trf("查询：%s", a.searchQuery), col: ui.ColNavy, bold: true})
	}
	if len(a.searchResults) == 0 {
		text := a.d.tr("暂无搜索结果。")
		if a.searchQuery != "" {
			text = a.d.tr("正在搜索或等待结果…")
		}
		lines = append(lines, agentDisplayLine{text: text, col: ui.ColTextDis})
	}
	for i, result := range a.searchResults {
		lines = append(lines, agentDisplayLine{text: fmt.Sprintf("%d. %s", i+1, result.Title), col: ui.ColBlue, bold: true})
		lines = append(lines, agentDisplayLine{text: result.URL, col: ui.ColTextDis})
		if result.Snippet != "" {
			for _, line := range wrapText(result.Snippet, maxInt(1, view.W)) {
				lines = append(lines, agentDisplayLine{text: line})
			}
		}
		lines = append(lines, agentDisplayLine{})
	}
	paintAgentList(c, f, view, a.searchBarR, lines, &a.searchScroll, a.searchAutoBottom)
}

func (a *AgentStudioApp) paintFiles(c *ui.Canvas, f *ui.Font) {
	ui.GroupBox(c, a.filesR, a.d.tr("修改文件列表"))
	a.filesBarR = ui.Rect{X: a.filesR.Right() - 18, Y: a.filesR.Y + 10, W: 16, H: a.filesR.H - 20}
	view := ui.Rect{X: a.filesR.X + 8, Y: a.filesR.Y + 10, W: a.filesR.W - 34, H: a.filesR.H - 20}
	var lines []agentDisplayLine
	for _, change := range a.changes {
		text := a.d.trf("[修改] %s", change.Target)
		if change.Kind == agentcore.ActionDeleteFile {
			text = a.d.trf("[删除] %s", change.Target)
		}
		for _, line := range wrapText(text, maxInt(1, view.W)) {
			lines = append(lines, agentDisplayLine{text: line, col: ui.ColGreen})
		}
	}
	if len(lines) == 0 {
		lines = append(lines, agentDisplayLine{text: a.d.tr("尚无文件变更。"), col: ui.ColTextDis})
	}
	paintAgentList(c, f, view, a.filesBarR, lines, &a.fileScroll, false)
}

func (a *AgentStudioApp) paintContext(c *ui.Canvas, f *ui.Font) {
	ui.GroupBox(c, a.contextR, a.d.tr("上下文"))
	f.TextClip(c, a.contextR.X+10, a.contextR.Y+12, a.contextR.W-20,
		a.d.trf("%d 条 · 约 %d / %d tokens", len(a.context), agentContextTokens(a.context), agentStudioContextMax), ui.ColText)
	a.compressR = ui.Rect{X: a.contextR.Right() - 104, Y: a.contextR.Bottom() - 36, W: 94, H: 26}
	ui.Button(c, a.compressR, a.d.tr("压缩上下文"), false, !a.busy() && len(a.context) > 0)
	f.TextClip(c, a.contextR.X+10, a.contextR.Bottom()-31, maxInt(1, a.compressR.X-a.contextR.X-18), a.d.tr("压缩前需要确认"), ui.ColTextDis)
}

func paintAgentList(c *ui.Canvas, f *ui.Font, view, bar ui.Rect, lines []agentDisplayLine, scroll *VScroll, autoBottom bool) {
	const lineH = 18
	scroll.Total = len(lines) * lineH
	scroll.Visible = maxInt(1, view.H)
	if autoBottom {
		scroll.Pos = scroll.Max()
	}
	scroll.Clamp()
	scroll.Draw(c, bar)
	start := scroll.Pos / lineH
	visible := (view.H + lineH - 1) / lineH
	for i := 0; i < visible && start+i < len(lines); i++ {
		line := lines[start+i]
		y := view.Y + i*lineH
		if line.bold {
			f.TextBold(c, view.X, y, line.text, line.col)
		} else {
			f.TextClip(c, view.X, y, view.W, line.text, line.col)
		}
	}
}
