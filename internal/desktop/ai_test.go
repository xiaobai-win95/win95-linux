package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIDangerDetection(t *testing.T) {
	danger := []string{
		"rm -rf /",
		"rm -r /",
		"rm -fr /root/文档",
		"find /root -delete",
		"find /root -exec rm {} +",
		"echo $(touch /tmp/injected)",
		`echo "$(touch /tmp/injected)"`,
		"sh -c 'rm -rf /'",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		"shutdown -h now",
		"reboot",
		"chmod -R 777 /",
		"apk del alpine-base",
	}
	for _, cmd := range danger {
		if !dangerousCommand(cmd) {
			t.Errorf("dangerousCommand(%q) = false, want true", cmd)
		}
	}
	safe := []string{"df -h", "ls -la /root", "find /root -maxdepth 1 -type f -print", "ping -c 4 8.8.8.8", "apk add htop", "rm /tmp/a.txt"}
	for _, cmd := range safe {
		if dangerousCommand(cmd) {
			t.Errorf("dangerousCommand(%q) = true, want false", cmd)
		}
	}
}

func TestAICommandConfirmationAllowlist(t *testing.T) {
	for _, command := range []string{"df -h", "ls -la /root", "find /root -maxdepth 1 -type f -print", "printf '%s' hello"} {
		if requiresCommandConfirmation(command) {
			t.Errorf("safe command requires confirmation: %s", command)
		}
	}
	for _, command := range []string{"rm /tmp/a.txt", "apk add htop", "ping -c 4 8.8.8.8", "df -h; rm -r /", "echo $(touch /tmp/x)"} {
		if !requiresCommandConfirmation(command) {
			t.Errorf("non-allowlisted command skipped confirmation: %s", command)
		}
	}
}

func TestAILocalCommandRules(t *testing.T) {
	cases := []struct {
		prompt string
		cmd    string
	}{
		{"查看磁盘空间", "df -h"},
		{"显示内存", "free -h"},
		{"列出正在监听的端口", "ss -tulpn"},
		{"查看当前目录", "pwd"},
		{"安装 htop", "apk add --no-cache htop"},
		{"删除 /root/文档/旧文件.txt", "rm -rf /root/文档/旧文件.txt"},
		{"查找 report 文件", "find / -iname '*report*' -print"},
		{"查看进程", "ps aux"},
		{"查看 CPU 负载", "uptime"},
	}
	for _, c := range cases {
		cmd, expl := localCommand(c.prompt)
		if cmd != c.cmd {
			t.Errorf("localCommand(%q) = %q (%s), want %q", c.prompt, cmd, expl, c.cmd)
		}
	}
	if cmd, _ := localCommand("删除旧文件"); cmd != "" {
		t.Errorf("delete without a path returned %q", cmd)
	}
}

func TestAIConfigRoundTrip(t *testing.T) {
	old := aiConfigPath
	aiConfigPath = filepath.Join(t.TempDir(), "ai.json")
	defer func() { aiConfigPath = old }()
	cfg := aiConfig{Provider: "qwen", BaseURL: "https://example.com/v1", Model: "qwen-plus", APIKey: "secret"}
	if err := saveAIConfig(cfg); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(aiConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", st.Mode().Perm())
	}
	got := loadAIConfig()
	if got != cfg {
		t.Fatalf("loaded config = %+v, want %+v", got, cfg)
	}
}

func TestAIConfigRepairsLegacyPermissions(t *testing.T) {
	old := aiConfigPath
	aiConfigPath = filepath.Join(t.TempDir(), "ai.json")
	defer func() { aiConfigPath = old }()
	cfg := aiConfig{Provider: "qwen", BaseURL: "https://example.com/v1", Model: "qwen-plus", APIKey: "secret"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aiConfigPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadAIConfig(); got != cfg {
		t.Fatalf("loaded config = %+v, want %+v", got, cfg)
	}
	info, err := os.Stat(aiConfigPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("legacy config was not repaired: %v, %v", info, err)
	}
}

func TestAIConfigSaveReplacesSymlinkWithoutFollowingIt(t *testing.T) {
	old := aiConfigPath
	dir := t.TempDir()
	aiConfigPath = filepath.Join(dir, "ai.json")
	defer func() { aiConfigPath = old }()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, aiConfigPath); err != nil {
		t.Fatal(err)
	}
	cfg := aiConfig{Provider: "deepseek", BaseURL: "https://example.com/v1", Model: "deepseek-chat", APIKey: "new-secret"}
	if err := saveAIConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside-secret" {
		t.Fatalf("save followed the config symlink: %q, %v", data, err)
	}
	info, err := os.Lstat(aiConfigPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("config was not atomically replaced with a private file: %v, %v", info, err)
	}
	if got := loadAIConfig(); got != cfg {
		t.Fatalf("loaded replacement = %+v, want %+v", got, cfg)
	}
}

func TestAIConfigLoadRejectsSymlink(t *testing.T) {
	old := aiConfigPath
	dir := t.TempDir()
	aiConfigPath = filepath.Join(dir, "ai.json")
	defer func() { aiConfigPath = old }()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"provider":"qwen","apiKey":"outside-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, aiConfigPath); err != nil {
		t.Fatal(err)
	}
	if got := loadAIConfig(); got.APIKey != "" || got.Provider != "local" {
		t.Fatalf("loaded through a symlink: %+v", got)
	}
}

func TestAICompleteHTTP(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["model"] != "test-model" {
			t.Errorf("model = %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"COMMAND: df -h\nEXPLAIN: 查看磁盘占用"}}]}`))
	}))
	defer srv.Close()
	cfg := aiConfig{Provider: "custom", BaseURL: srv.URL, Model: "test-model", APIKey: "secret"}
	cmd, expl, err := aiGenerateWithLLM(cfg, "磁盘满了怎么办")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "df -h" || !strings.Contains(expl, "磁盘") {
		t.Fatalf("parsed command = %q / %q", cmd, expl)
	}
	if auth != "Bearer secret" {
		t.Fatalf("authorization = %q", auth)
	}
}

func TestAIExecuteLocalCommand(t *testing.T) {
	out := execShell(context.Background(), "printf 'hello-ai'")
	if !strings.Contains(out, "hello-ai") {
		t.Fatalf("shell output = %q", out)
	}
}

func TestAIExecShellDoesNotEvaluateCommandSubstitution(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "injected")
	out := execShell(context.Background(), `printf '%s' '$(touch `+marker+`)'`)
	if !strings.Contains(out, "$(touch ") {
		t.Fatalf("single-quoted text was changed: %q", out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("single-quoted command substitution ran: %v", err)
	}
	out = execShell(context.Background(), `printf '%s' "$(touch `+marker+`)"`)
	if !strings.Contains(out, "拒绝命令替换") {
		t.Fatalf("double-quoted substitution was not rejected: %q", out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("double-quoted command substitution ran: %v", err)
	}
}

func TestAIOfflineChatFallsBack(t *testing.T) {
	old := aiConfigPath
	aiConfigPath = filepath.Join(t.TempDir(), "ai.json")
	defer func() { aiConfigPath = old }()
	d := New(1024, 768, ModeDesktop, Host{})
	a := NewAIApp(d)
	a.chatInput = []rune("你好")
	a.chatReply(d)
	d.RunPosted()
	if a.chatBusy {
		t.Fatal("chat stayed busy after offline fallback")
	}
	if len(a.chat) < 3 {
		t.Fatalf("chat messages = %d", len(a.chat))
	}
	last := a.chat[len(a.chat)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content, "本地离线模式") {
		t.Fatalf("offline reply = %+v", last)
	}
}

func TestAIAppLocalGenerate(t *testing.T) {
	old := aiConfigPath
	aiConfigPath = filepath.Join(t.TempDir(), "ai.json")
	defer func() { aiConfigPath = old }()
	d := New(1024, 768, ModeDesktop, Host{})
	a := d.openAI(0)
	a.prompt = []rune("查看磁盘空间")
	a.generate(d)
	if a.cmd != "df -h" {
		t.Fatalf("generated command = %q", a.cmd)
	}
	if dangerousCommand(a.cmd) {
		t.Fatal("df -h flagged as dangerous")
	}
}
