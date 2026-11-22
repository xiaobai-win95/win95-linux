// Package lanshare serves a tiny HTTP file drop for phones and other
// computers on the same LAN. Uploads land in a directory chosen by the caller.
package lanshare

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultPort = 8765
	maxPort     = 8784
)

type Server struct {
	dir string

	mu      sync.Mutex
	srv     *http.Server
	ln      net.Listener
	port    int
	lastErr string

	uploads   atomic.Int64
	lastFile  atomic.Value // string
	lastWhen  atomic.Value // time.Time
	uploadErr atomic.Value // string

	filesMu sync.Mutex
	files   map[string]string // token -> absolute path
}

func New(dir string) *Server {
	return &Server{dir: dir, files: map[string]string{}}
}

// RegisterFile makes an arbitrary file downloadable through a random token
// URL, so files outside the drop folder can be sent to a phone without
// copying them first.
func (s *Server) RegisterFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("只能分享普通文件")
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().UnixNano())))
	token := hex.EncodeToString(sum[:8])
	s.filesMu.Lock()
	s.files[token] = path
	s.filesMu.Unlock()
	if !s.Running() {
		return "", fmt.Errorf("局域网快传服务未启动")
	}
	return fmt.Sprintf("http://%s:%d/s/%s", HostIP(), s.Port(), token), nil
}

func (s *Server) Dir() string { return s.dir }

func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.srv != nil
}

func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

func (s *Server) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

func (s *Server) Stats() (uploads int64, last string, when time.Time, uploadErr string) {
	uploads = s.uploads.Load()
	if v, ok := s.lastFile.Load().(string); ok {
		last = v
	}
	if v, ok := s.lastWhen.Load().(time.Time); ok {
		when = v
	}
	if v, ok := s.uploadErr.Load().(string); ok {
		uploadErr = v
	}
	return
}

// Start binds the first free port in the 8765–8784 range. Calling Start on a
// running server is a no-op.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		s.lastErr = "创建共享文件夹失败: " + err.Error()
		return errors.New(s.lastErr)
	}
	var ln net.Listener
	var err error
	port := 0
	for p := defaultPort; p <= maxPort; p++ {
		ln, err = net.Listen("tcp4", ":"+strconv.Itoa(p))
		if err == nil {
			port = p
			break
		}
	}
	if ln == nil {
		s.lastErr = "无法监听端口 8765–8784: " + err.Error()
		return errors.New(s.lastErr)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/d/", s.handleDownload)
	mux.HandleFunc("/s/", s.handleToken)
	mux.HandleFunc("/api/info", s.handleInfo)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	s.srv = srv
	s.ln = ln
	s.port = port
	s.lastErr = ""
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.lastErr = "局域网共享服务已停止: " + err.Error()
			s.srv = nil
			s.mu.Unlock()
		}
	}()
	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.ln = nil
	s.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// URL returns the browser address for this share. The host part is the first
// private IPv4 address of the machine, so phones on the same Wi-Fi can reach
// it without DNS.
func (s *Server) URL() string {
	if !s.Running() {
		return ""
	}
	return fmt.Sprintf("http://%s:%d/", HostIP(), s.Port())
}

// HostIP prefers the address the kernel would use for a public route; this is
// normally the LAN address and avoids picking docker/vpn interfaces.
func HostIP() string {
	if c, err := net.Dial("udp4", "223.5.5.5:53"); err == nil {
		ip := c.LocalAddr().(*net.UDPAddr).IP.String()
		c.Close()
		if ip != "" && ip != "127.0.0.1" {
			return ip
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	fallback := ""
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, ad := range addrs {
			ipnet, ok := ad.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			ip := ip4.String()
			if isPrivate(ip4) {
				return ip
			}
			if fallback == "" && !strings.HasPrefix(ip, "169.254.") {
				fallback = ip
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return "127.0.0.1"
}

func isPrivate(ip net.IP) bool {
	return ip[0] == 10 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
		(ip[0] == 192 && ip[1] == 168)
}

type shareFile struct {
	Name string
	Size int64
	Mod  time.Time
	URL  string
}

func (s *Server) list() []shareFile {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	out := make([]shareFile, 0, len(ents))
	for _, de := range ents {
		if de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		info, err := de.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, shareFile{
			Name: de.Name(),
			Size: info.Size(),
			Mod:  info.ModTime(),
			URL:  "/d/" + url.PathEscape(de.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod.After(out[j].Mod) })
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

var indexTmpl = template.Must(template.New("index").Funcs(template.FuncMap{
	"size": sizeText,
	"time": func(t time.Time) string { return t.Format("01-02 15:04") },
}).Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Windows 95 Linux · 局域网快传</title>
<style>
* { box-sizing: border-box; }
body { margin: 0; background: #008080; font-family: "MS Sans Serif", "PingFang SC", "Microsoft YaHei", sans-serif; color: #000; }
.shell { max-width: 760px; margin: 0 auto; padding: 14px; }
.title { background: linear-gradient(90deg, #000080, #1084d0); color: #fff; font-weight: bold; padding: 7px 10px; display: flex; align-items: center; gap: 8px; }
.title .ico { width: 16px; height: 16px; background: #c0c0c0; border: 2px solid; border-color: #fff #404040 #404040 #fff; display: inline-block; }
.win { background: #c0c0c0; border: 2px solid; border-color: #dfdfdf #404040 #404040 #dfdfdf; box-shadow: 2px 2px 0 #000; padding: 12px; margin-bottom: 14px; }
h2 { font-size: 15px; margin: 0 0 8px; }
.hint { font-size: 13px; color: #404040; line-height: 1.5; }
.drop { border: 2px dashed #808080; background: #fff; padding: 22px 12px; text-align: center; margin: 10px 0; }
.drop.hover { border-color: #000080; background: #e8e8ff; }
input[type=file] { max-width: 100%; }
button, .btn { font-family: inherit; font-size: 14px; background: #c0c0c0; border: 2px solid; border-color: #fff #404040 #404040 #fff; padding: 7px 18px; cursor: pointer; box-shadow: 1px 1px 0 #000; text-decoration: none; color: #000; display: inline-block; }
button:active { border-color: #404040 #fff #fff #404040; }
.bar { height: 18px; background: #fff; border: 1px solid #808080; margin-top: 10px; display: none; }
.bar > i { display: block; height: 100%; width: 0; background: #000080; }
.list { width: 100%; border-collapse: collapse; background: #fff; margin-top: 8px; font-size: 13px; }
.list th { background: #c0c0c0; text-align: left; padding: 4px 6px; border: 1px solid #808080; font-weight: normal; }
.list td { padding: 6px; border-bottom: 1px solid #d0d0d0; word-break: break-all; }
.list tr:nth-child(even) td { background: #f4f4f4; }
a { color: #000080; }
.status { font-size: 13px; margin-top: 8px; min-height: 18px; color: #000080; }
.foot { color: #d0ffff; font-size: 12px; text-align: center; padding: 4px 0 18px; }
textarea { width: 100%; height: 54px; font-family: monospace; font-size: 12px; }
</style>
</head>
<body>
<div class="shell">
  <div class="win">
    <div class="title"><span class="ico"></span>局域网快传 · Windows 95 Linux</div>
    <div style="padding-top:10px">
      <h2>把文件传到这台电脑</h2>
      <div class="hint">手机和这台电脑连同一个 Wi-Fi / 局域网即可。上传的文件会保存到 <b>/root/下载</b>。</div>
      <form id="up" method="post" action="/upload" enctype="multipart/form-data">
        <div class="drop" id="drop">
          <input type="file" id="files" name="files" multiple required>
          <p class="hint">也可以把文件拖到这里</p>
          <button type="submit">开始上传</button>
        </div>
        <div class="bar" id="bar"><i id="fill"></i></div>
        <div class="status" id="status">{{if .Uploaded}}已收到 {{.Uploaded}} 个文件。{{end}}</div>
      </form>
    </div>
  </div>
  <div class="win">
    <div class="title"><span class="ico"></span>共享地址</div>
    <div style="padding-top:10px">
      <div class="hint">在手机浏览器打开：</div>
      <p><a href="{{.Self}}"><b>{{.Self}}</b></a></p>
      <div class="hint">把下面的代码复制到另一台电脑的地址栏也能访问：</div>
      <textarea readonly onclick="this.select()">{{.Self}}</textarea>
    </div>
  </div>
  <div class="win">
    <div class="title"><span class="ico"></span>可下载的文件（{{len .Files}}）</div>
    <div style="padding-top:10px">
      {{if .Files}}
      <table class="list">
        <tr><th>文件名</th><th>大小</th><th>修改时间</th></tr>
        {{range .Files}}
        <tr><td><a href="{{.URL}}">{{.Name}}</a></td><td>{{size .Size}}</td><td>{{time .Mod}}</td></tr>
        {{end}}
      </table>
      {{else}}
      <div class="hint">这里还没有文件。你可以在 Windows 95 桌面上选择文件，用“发送到手机”生成二维码。</div>
      {{end}}
    </div>
  </div>
  <div class="foot">Windows 95 Linux · 局域网快传</div>
</div>
<script>
const form = document.getElementById('up');
const drop = document.getElementById('drop');
const input = document.getElementById('files');
const bar = document.getElementById('bar');
const fill = document.getElementById('fill');
const status = document.getElementById('status');
drop.addEventListener('dragover', e => { e.preventDefault(); drop.classList.add('hover'); });
drop.addEventListener('dragleave', () => drop.classList.remove('hover'));
drop.addEventListener('drop', e => {
  e.preventDefault(); drop.classList.remove('hover');
  if (e.dataTransfer.files.length) { input.files = e.dataTransfer.files; }
});
form.addEventListener('submit', e => {
  e.preventDefault();
  if (!input.files.length) { status.textContent = '请先选择文件。'; return; }
  const fd = new FormData();
  for (const f of input.files) fd.append('files', f);
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/upload');
  bar.style.display = 'block';
  xhr.upload.onprogress = ev => {
    if (ev.lengthComputable) {
      const pct = Math.round(ev.loaded * 100 / ev.total);
      fill.style.width = pct + '%';
      status.textContent = '正在上传… ' + pct + '%';
    }
  };
  xhr.onload = () => {
    if (xhr.status >= 200 && xhr.status < 300) {
      try {
        const data = JSON.parse(xhr.responseText);
        status.textContent = '上传完成：' + data.files.join('、');
        fill.style.width = '100%';
        setTimeout(() => location.href = '/', 700);
        return;
      } catch (err) {}
      location.href = '/';
      return;
    }
    status.textContent = '上传失败：' + (xhr.responseText || xhr.status);
    bar.style.display = 'none';
  };
  xhr.onerror = () => { status.textContent = '网络错误，上传失败。'; bar.style.display = 'none'; };
  xhr.send(fd);
});
</script>
</body>
</html>`))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uploaded, _ := strconv.Atoi(r.URL.Query().Get("uploaded"))
	data := struct {
		Self     string
		Files    []shareFile
		Uploaded int
	}{
		Self:     s.URL(),
		Files:    s.list(),
		Uploaded: uploaded,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, data)
}

func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	switch name {
	case "", ".", "..", "/":
		return "未命名文件"
	}
	runes := []rune(name)
	if len(runes) > 180 {
		name = string(runes[:180])
	}
	return name
}

func (s *Server) uniquePath(name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	candidate := filepath.Join(s.dir, name)
	for i := 2; i < 10000; i++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(s.dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	return candidate
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<30)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "需要 multipart/form-data", http.StatusBadRequest)
		return
	}
	var saved []string
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.uploadErr.Store("读取上传失败: " + err.Error())
			http.Error(w, "读取上传失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		if part.FileName() == "" {
			part.Close()
			continue
		}
		name := s.savePart(part)
		part.Close()
		if name != "" {
			saved = append(saved, name)
		}
	}
	if len(saved) == 0 {
		http.Error(w, "没有收到文件", http.StatusBadRequest)
		return
	}
	s.uploads.Add(int64(len(saved)))
	s.lastFile.Store(saved[len(saved)-1])
	s.lastWhen.Store(time.Now())
	s.uploadErr.Store("")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "files": saved})
}

func (s *Server) savePart(part *multipart.Part) string {
	name := sanitizeName(part.FileName())
	dst := s.uniquePath(name)
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		s.uploadErr.Store("无法写入文件: " + err.Error())
		return ""
	}
	if _, err := io.Copy(f, part); err != nil {
		f.Close()
		os.Remove(tmp)
		s.uploadErr.Store("保存上传失败: " + err.Error())
		return ""
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		s.uploadErr.Store("保存上传失败: " + err.Error())
		return ""
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		s.uploadErr.Store("保存上传失败: " + err.Error())
		return ""
	}
	return filepath.Base(dst)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/d/"))
	if err != nil || raw == "" {
		http.NotFound(w, r)
		return
	}
	name := sanitizeName(raw)
	if name != filepath.Base(raw) {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(s.dir, name)
	st, err := os.Stat(full)
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	http.ServeFile(w, r, full)
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/s/")
	s.filesMu.Lock()
	path := s.files[token]
	s.filesMu.Unlock()
	if path == "" {
		http.NotFound(w, r)
		return
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(path)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	http.ServeFile(w, r, path)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"url":     s.URL(),
		"port":    s.Port(),
		"running": s.Running(),
		"dir":     s.dir,
	})
}
