package agentcore

import (
	"fmt"
	"path/filepath"
	"strings"
)

type CommandArgument struct {
	Flag       string
	Value      string
	Meaning    string
	IsOptional bool
}

type CommandExplanation struct {
	Command   string
	Purpose   string
	Arguments []CommandArgument
	Risk      Risk
	Warning   string
}

func ExplainCommand(command string) CommandExplanation {
	words := shellWords(command)
	ex := CommandExplanation{Command: command, Purpose: "通过系统 Shell 执行一组命令。", Risk: RiskMedium}
	if len(words) == 0 {
		ex.Purpose = "空命令，不会执行任何操作。"
		ex.Risk = RiskLow
		return ex
	}
	name := filepath.Base(words[0])
	ex.Purpose = commandPurpose(name)
	for i := 1; i < len(words); i++ {
		w := words[i]
		if strings.HasPrefix(w, "-") && i+1 < len(words) && !strings.HasPrefix(words[i+1], "-") {
			ex.Arguments = append(ex.Arguments, CommandArgument{Flag: w, Value: words[i+1], Meaning: flagMeaning(name, w), IsOptional: true})
			i++
		} else {
			ex.Arguments = append(ex.Arguments, CommandArgument{Value: w, Meaning: argumentMeaning(name, w)})
		}
	}
	if IsDangerousCommand(command) {
		ex.Risk = RiskHigh
		ex.Warning = "可能删除数据、修改分区或中断系统；执行前必须再次确认。"
	} else if name == "rm" || name == "mv" || name == "cp" || name == "chmod" || name == "chown" {
		ex.Risk = RiskMedium
		ex.Warning = "会修改文件或权限，请确认目标路径。"
	} else if isReadOnlyCommand(name) {
		ex.Risk = RiskLow
	}
	return ex
}

func (e CommandExplanation) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "用途：%s\n风险等级：%s\n", e.Purpose, e.Risk)
	if len(e.Arguments) > 0 {
		b.WriteString("参数：\n")
		for _, a := range e.Arguments {
			if a.Flag != "" {
				fmt.Fprintf(&b, "  %s %s — %s\n", a.Flag, a.Value, a.Meaning)
			} else {
				fmt.Fprintf(&b, "  %s — %s\n", a.Value, a.Meaning)
			}
		}
	}
	if e.Warning != "" {
		fmt.Fprintf(&b, "注意：%s", e.Warning)
	}
	return b.String()
}

func commandPurpose(name string) string {
	switch name {
	case "ls":
		return "列出目录内容。"
	case "cat":
		return "读取并输出一个或多个文件。"
	case "cp":
		return "复制文件或目录。"
	case "mv":
		return "移动或重命名文件。"
	case "rm":
		return "删除文件或目录。"
	case "mkdir":
		return "创建目录。"
	case "touch":
		return "创建空文件或更新时间戳。"
	case "find":
		return "按名称、类型或属性查找文件。"
	case "grep":
		return "在文本中搜索匹配行。"
	case "sed", "awk":
		return "处理或转换文本流。"
	case "chmod":
		return "修改文件访问权限。"
	case "chown":
		return "修改文件所有者。"
	case "df":
		return "显示文件系统容量与剩余空间。"
	case "du":
		return "统计文件或目录占用空间。"
	case "ps":
		return "列出当前进程。"
	case "kill":
		return "向进程发送信号。"
	case "systemctl":
		return "管理系统服务与电源状态。"
	case "pacman", "apk":
		return "安装、升级或删除软件包。"
	case "git":
		return "执行 Git 版本控制操作。"
	case "curl", "wget":
		return "从网络下载数据。"
	case "tar":
		return "打包或解包归档文件。"
	case "echo", "printf":
		return "输出文本。"
	case "pwd":
		return "显示当前工作目录。"
	default:
		return "执行 " + name + " 程序。"
	}
}

func flagMeaning(name, flag string) string {
	switch flag {
	case "-l":
		return "使用详细列表格式"
	case "-a":
		return "包含隐藏项目"
	case "-r", "-R":
		return "递归处理"
	case "-f":
		return "强制执行或覆盖"
	case "-v":
		return "显示详细过程"
	case "-i":
		return "交互式确认"
	case "-n":
		return "限制数量或显示行号"
	case "-e":
		return "指定表达式或启用退出检查"
	case "-c":
		return "执行指定的配置或命令"
	default:
		return name + " 的命令选项"
	}
}

func argumentMeaning(name, value string) string {
	if strings.HasPrefix(value, "/") || strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return "文件或目录路径"
	}
	switch name {
	case "kill":
		return "进程号或进程选择表达式"
	case "chmod":
		return "权限模式，例如 755 或 u+x"
	case "systemctl":
		return "服务名或操作目标"
	case "pacman", "apk":
		return "软件包名称"
	case "grep":
		return "搜索模式"
	default:
		return "命令输入或目标"
	}
}

func isReadOnlyCommand(name string) bool {
	switch name {
	case "ls", "cat", "pwd", "df", "du", "ps", "find", "grep", "head", "tail", "stat", "uname", "whoami", "id":
		return true
	default:
		return false
	}
}

func shellWords(s string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == ';' || r == '|' || r == '&' {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return out
}
