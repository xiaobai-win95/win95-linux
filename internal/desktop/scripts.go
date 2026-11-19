package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := b.max - b.buf.Len()
	if room <= 0 {
		b.truncated = true
		return n, nil
	}
	if len(p) > room {
		b.buf.Write(p[:room])
		b.truncated = true
	} else {
		b.buf.Write(p)
	}
	return n, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runScript opens a terminal window that runs the script on a PTY. Output is
// streamed live, stdin stays connected to the keyboard, and the window stays
// open with the exit code after the script finishes.
func (d *Desktop) runScript(path, shell string) {
	if _, err := os.Stat(path); err != nil {
		d.MessageBox("运行脚本", "找不到脚本文件：\n"+path+"\n\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	if _, err := os.Stat(shell); err != nil {
		d.MessageBox("运行脚本", "找不到脚本解释器 "+shell+"。\n\nBash 脚本需要先安装 bash：\napk add bash", IconError, []string{"确定"}, nil)
		return
	}
	path = filepath.Clean(path)
	cwd := filepath.Dir(path)
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		cwd = "/root"
	}
	title := "终端 - " + filepath.Base(path)
	d.openTerminalCommand(title, []string{shell, path}, cwd, "script:"+path)
}
