package agentcore

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Permission int

const (
	PermissionAsk Permission = iota
	PermissionAuto
	PermissionFull
)

func (p Permission) String() string {
	switch p {
	case PermissionAsk:
		return "每次询问"
	case PermissionAuto:
		return "自动审批"
	case PermissionFull:
		return "完全访问"
	default:
		return "未知权限"
	}
}

type ActionKind int

const (
	ActionSearch ActionKind = iota
	ActionReadFile
	ActionWriteFile
	ActionDeleteFile
	ActionShell
)

type Risk int

const (
	RiskLow Risk = iota
	RiskMedium
	RiskHigh
)

func (r Risk) String() string {
	switch r {
	case RiskLow:
		return "低"
	case RiskMedium:
		return "中"
	case RiskHigh:
		return "高"
	default:
		return "未知"
	}
}

type Action struct {
	Kind        ActionKind
	Description string
	Target      string
	Query       string
	Content     string
	Command     string
	Risk        Risk
}

func (a Action) Label() string {
	switch a.Kind {
	case ActionSearch:
		return "联网搜索：" + a.Query
	case ActionReadFile:
		return "读取文件：" + a.Target
	case ActionWriteFile:
		return "修改文件：" + a.Target
	case ActionDeleteFile:
		return "删除文件：" + a.Target
	case ActionShell:
		return "执行命令：" + a.Command
	default:
		return a.Description
	}
}

func (p Permission) Allows(a Action) bool {
	switch p {
	case PermissionFull:
		return true
	case PermissionAuto:
		return a.Risk < RiskHigh && a.Kind != ActionDeleteFile
	case PermissionAsk:
		return a.Kind == ActionSearch || a.Kind == ActionReadFile
	default:
		return false
	}
}

type EventKind int

const (
	EventThought EventKind = iota
	EventSearchStart
	EventSearchResult
	EventReadFile
	EventFileChange
	EventPermission
	EventContextPrompt
	EventMessage
	EventError
	EventDone
)

type Event struct {
	Kind    EventKind
	Text    string
	Target  string
	Results []SearchResult
}

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

func (e Event) String() string {
	switch e.Kind {
	case EventThought:
		return "思考：" + e.Text
	case EventSearchStart:
		return "联网搜索：" + e.Text
	case EventSearchResult:
		return fmt.Sprintf("搜索完成：%d 条结果", len(e.Results))
	case EventReadFile:
		return "读取：" + e.Target
	case EventFileChange:
		return "已修改：" + e.Target
	case EventPermission:
		return "等待授权：" + e.Text
	case EventContextPrompt:
		return "上下文过长：" + e.Text
	case EventMessage:
		return e.Text
	case EventError:
		return "错误：" + e.Text
	case EventDone:
		return "完成"
	default:
		return e.Text
	}
}

type ContextEntry struct {
	Role string
	Text string
}

func ApproxTokens(text string) int {
	n := len([]rune(text))
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

func CompressContext(entries []ContextEntry, maxTokens int) ([]ContextEntry, string) {
	if maxTokens < 1 {
		maxTokens = 1
	}
	total := 0
	for _, e := range entries {
		total += ApproxTokens(e.Text) + 4
	}
	if total <= maxTokens {
		return append([]ContextEntry(nil), entries...), ""
	}
	out := make([]ContextEntry, 0, len(entries))
	used := 0
	for i := len(entries) - 1; i >= 0; i-- {
		cost := ApproxTokens(entries[i].Text) + 4
		if used+cost > maxTokens {
			break
		}
		out = append(out, entries[i])
		used += cost
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out, fmt.Sprintf("已压缩，保留最近 %d 条上下文（约 %d tokens）", len(out), used)
}

func SafeTarget(root, target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("路径为空")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = "/"
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if abs != base && !strings.HasPrefix(abs, strings.TrimRight(base, "/")+"/") {
		return "", fmt.Errorf("目标路径越界：%s", target)
	}
	return abs, nil
}
