package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"win95linux/internal/ui"
)

func newMarkdownTestAI(t *testing.T) (*Desktop, *AIApp) {
	t.Helper()
	old := aiConfigPath
	aiConfigPath = filepath.Join(t.TempDir(), "ai.json")
	t.Cleanup(func() { aiConfigPath = old })
	d := New(1024, 768, ModeDesktop, Host{})
	return d, NewAIApp(d)
}

func TestAIChatRendersMarkdownAndCopiesCode(t *testing.T) {
	_, a := newMarkdownTestAI(t)
	code := "printf 'hello-ai'\necho done"
	content := strings.Repeat("一段用于滚动的 Markdown 正文。\n\n", 24) +
		"# 标题\n\n**粗体** 与 *斜体*，数学 $x^2$。\n\n" +
		"| 命令 | 用途 |\n| --- | --- |\n| df -h | 磁盘 |\n\n" +
		"```sh\n" + code + "\n```"
	a.chat = []aiChatMessage{{Role: "user", Content: content}}
	a.chatInput = []rune("保留输入")
	a.rebuildChatMarkdown()
	var copied string
	a.copyCode = func(text string) { copied = text }

	canvas := ui.NewCanvas(720, 520)
	body := ui.Rect{X: 0, Y: 0, W: 700, H: 500}
	a.paintChat(canvas, ui.DefaultFont(), body)
	if a.chatRender.ContentHeight <= a.chatR.H || a.chatRender.MaxScroll <= 0 {
		t.Fatalf("chat did not produce a scrollable Markdown document: %+v", a.chatRender)
	}
	if a.chatRender.ContentRect.Y != a.chatR.Y-a.chatRender.MaxScroll {
		t.Fatalf("newest message is not at the bottom: rect=%+v viewport=%+v", a.chatRender.ContentRect, a.chatR)
	}
	if len(a.chatRender.CopyButtons) == 0 {
		t.Fatal("visible fenced code has no Copy button")
	}
	button := a.chatRender.CopyButtons[len(a.chatRender.CopyButtons)-1]
	if !a.chatRender.Activate(button.Rect.X+button.Rect.W/2, button.Rect.Y+button.Rect.H/2) {
		t.Fatal("Copy button did not activate")
	}
	if copied != code {
		t.Fatalf("copied code = %q, want %q", copied, code)
	}
	if string(a.chatInput) != "保留输入" || a.chatInR.W <= 0 {
		t.Fatalf("chat input was not preserved: %q, rect=%+v", a.chatInput, a.chatInR)
	}
}

func TestAIChatMarkdownScrollAndWheel(t *testing.T) {
	d, a := newMarkdownTestAI(t)
	a.tab = 1
	a.chat = []aiChatMessage{{Role: "assistant", Content: strings.Repeat("滚动行。\n\n", 80)}}
	a.rebuildChatMarkdown()
	canvas := ui.NewCanvas(720, 520)
	body := ui.Rect{X: 0, Y: 0, W: 700, H: 500}
	a.paintChat(canvas, ui.DefaultFont(), body)
	bottomOffset := a.chatR.Y - a.chatRender.ContentRect.Y
	if bottomOffset != a.chatRender.MaxScroll {
		t.Fatalf("initial offset = %d, want max %d", bottomOffset, a.chatRender.MaxScroll)
	}

	a.Key(d, nil, KeyEvent{Name: "PgUp"})
	if a.chatScroll <= 0 {
		t.Fatal("PgUp did not scroll the Markdown document")
	}
	a.paintChat(canvas, ui.DefaultFont(), body)
	if got := a.chatR.Y - a.chatRender.ContentRect.Y; got >= bottomOffset {
		t.Fatalf("PgUp offset = %d, want less than %d", got, bottomOffset)
	}

	before := a.chatScroll
	a.Mouse(d, nil, body, MouseEvent{Kind: MouseWheel, X: a.chatR.X + 20, Y: a.chatR.Y + 20, Wheel: 1})
	if a.chatScroll <= before {
		t.Fatalf("wheel scroll = %d, want greater than %d", a.chatScroll, before)
	}
	a.chatScroll = 0
	a.paintChat(canvas, ui.DefaultFont(), body)
	if got := a.chatR.Y - a.chatRender.ContentRect.Y; got != a.chatRender.MaxScroll {
		t.Fatalf("return-to-bottom offset = %d, want %d", got, a.chatRender.MaxScroll)
	}
}

func TestAICommandExplainAndDangerConfirmation(t *testing.T) {
	d, a := newMarkdownTestAI(t)
	a.cmd = "rm -rf /root/旧文件"
	a.explainCommand(d)
	for _, want := range []string{"用途：", "风险等级：高", "参数：", "警告："} {
		if !strings.Contains(a.output, want) {
			t.Fatalf("command explanation lacks %q: %q", want, a.output)
		}
	}
	if !requiresCommandConfirmation(a.cmd) {
		t.Fatal("dangerous command did not require confirmation")
	}
	if a.outScroll == 0 {
		t.Fatal("command explanation did not scroll back to its beginning")
	}
	a.execute(d)
	if d.modal == nil {
		t.Fatal("dangerous command executed without a confirmation modal")
	}
	dialog, ok := d.modal.App.(*dialogApp)
	if !ok || dialog.title != "危险命令二次确认" || !strings.Contains(strings.Join(dialog.lines, "\n"), "rm -rf") {
		t.Fatalf("unexpected confirmation dialog: %#v", d.modal.App)
	}
	if a.running {
		t.Fatal("dangerous command started running before confirmation")
	}

	a.cmd = "df -h"
	a.explainCommand(d)
	if !strings.Contains(a.output, "-h") || !strings.Contains(a.output, "风险等级：低") {
		t.Fatalf("safe command explanation incomplete: %q", a.output)
	}
}
