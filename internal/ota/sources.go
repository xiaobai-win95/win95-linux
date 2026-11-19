package ota

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// ConfigPath can override the manifest URL and add download accelerators
// without rebuilding the image. It is re-read on every check/install.
const ConfigPath = "/etc/win95-ota.conf"

// defaultMirrors are public GitHub accelerators used when the guest cannot
// reach github.com directly. Each entry is a URL prefix; "{url}" is also
// accepted as a template placeholder.
var defaultMirrors = []string{
	"https://gh-proxy.com/",
	"https://ghfast.top/",
	"https://ghproxy.net/",
	"https://ghproxy.cn/",
}

var otaTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout:   8 * time.Second,
	ResponseHeaderTimeout: 20 * time.Second,
	IdleConnTimeout:       60 * time.Second,
	ForceAttemptHTTP2:     true,
}

var otaClient = &http.Client{Transport: otaTransport}

// Sources is the ordered set of places to look for the manifest and package.
type Sources struct {
	ManifestURL string
	Mirrors     []string
	Direct      bool
}

func DefaultSources() Sources {
	return Sources{
		ManifestURL: DefaultManifestURL,
		Mirrors:     append([]string(nil), defaultMirrors...),
		Direct:      true,
	}
}

// LoadSources reads ConfigPath. Missing file means built-in defaults.
func LoadSources() Sources { return loadSources(ConfigPath) }

func loadSources(path string) Sources {
	s := DefaultSources()
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	var mirrors []string
	customMirrors := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "MANIFEST_URL":
			if val != "" {
				s.ManifestURL = val
			}
		case "MIRROR":
			if val != "" {
				mirrors = append(mirrors, val)
				customMirrors = true
			}
		case "DIRECT":
			s.Direct = !(val == "0" || strings.EqualFold(val, "false") || strings.EqualFold(val, "no"))
		}
	}
	if customMirrors {
		s.Mirrors = mirrors
	}
	return s
}

func githubURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "github.com" || h == "www.github.com" || strings.HasSuffix(h, ".githubusercontent.com")
}

func mirrorTarget(mirror, raw string) string {
	if strings.Contains(mirror, "{url}") {
		return strings.ReplaceAll(mirror, "{url}", raw)
	}
	return strings.TrimRight(mirror, "/") + "/" + raw
}

func (s Sources) candidates(raw string) []string {
	if raw == "" {
		return nil
	}
	if !githubURL(raw) {
		return []string{raw}
	}
	var out []string
	if s.Direct {
		out = append(out, raw)
	}
	for _, m := range s.Mirrors {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, mirrorTarget(m, raw))
		}
	}
	if len(out) == 0 {
		out = append(out, raw)
	}
	seen := make(map[string]bool, len(out))
	uniq := out[:0]
	for _, u := range out {
		if !seen[u] {
			seen[u] = true
			uniq = append(uniq, u)
		}
	}
	return uniq
}

// ManifestCandidates lists all mirror URLs for the update manifest.
func (s Sources) ManifestCandidates() []string { return s.candidates(s.ManifestURL) }

// PackageCandidates lists all mirror URLs for a package URL from the manifest.
func (s Sources) PackageCandidates(raw string) []string { return s.candidates(raw) }

// SourceLabel is a short Chinese name for logs and error messages.
func SourceLabel(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	h := u.Hostname()
	if h == "" {
		return raw
	}
	if h == "github.com" || h == "www.github.com" || strings.HasSuffix(h, ".githubusercontent.com") {
		return "GitHub 直连"
	}
	return "镜像 " + h
}

// FetchManifestAuto queries every candidate in parallel and returns the first
// valid manifest. Racing matters on networks where GitHub hangs until timeout:
// a working mirror answers while the direct connection is still stalled.
func FetchManifestAuto(ctx context.Context, urls []string, logf func(string)) (Manifest, string, error) {
	if len(urls) == 0 {
		return Manifest{}, "", errors.New("没有可用的更新源")
	}
	type result struct {
		m   Manifest
		u   string
		err error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan result, len(urls))
	for _, raw := range urls {
		go func(u string) {
			actx, acancel := context.WithTimeout(ctx, 12*time.Second)
			defer acancel()
			m, err := FetchManifest(actx, u)
			ch <- result{m: m, u: u, err: err}
		}(raw)
	}
	var errs []string
	for range urls {
		r := <-ch
		if r.err == nil {
			cancel()
			if logf != nil {
				logf("更新清单来源: " + SourceLabel(r.u))
			}
			return r.m, r.u, nil
		}
		errs = append(errs, SourceLabel(r.u)+": "+r.err.Error())
	}
	return Manifest{}, "", fmt.Errorf("所有更新源均不可用：%s", strings.Join(errs, "；"))
}

func probeURL(ctx context.Context, raw string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "win95-linux-ota/1")
	req.Header.Set("Range", "bytes=0-0")
	resp, err := otaClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
}

// rankSources probes each URL with a one-byte range request and returns the
// reachable ones first, fastest first. Unreachable URLs stay in the list so a
// probe failure caused by a proxy's Range handling does not hide a working
// download source.
func rankSources(ctx context.Context, urls []string, timeout time.Duration) []string {
	if len(urls) < 2 {
		return urls
	}
	type result struct {
		u   string
		ok  bool
		dur time.Duration
	}
	ch := make(chan result, len(urls))
	for _, raw := range urls {
		go func(u string) {
			start := time.Now()
			pctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			ok := probeURL(pctx, u)
			ch <- result{u: u, ok: ok, dur: time.Since(start)}
		}(raw)
	}
	res := make([]result, 0, len(urls))
	for range urls {
		res = append(res, <-ch)
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].ok != res[j].ok {
			return res[i].ok
		}
		return res[i].dur < res[j].dur
	})
	out := make([]string, 0, len(res))
	for _, r := range res {
		out = append(out, r.u)
	}
	return out
}

// DownloadPackageAuto picks the fastest reachable source and falls back to the
// others, resuming the ".part" file across switches.
func DownloadPackageAuto(ctx context.Context, urls []string, dest string, progress func(done, total int64), logf func(string)) error {
	if len(urls) == 0 {
		return errors.New("没有可用的下载源")
	}
	ranked := rankSources(ctx, urls, 5*time.Second)
	var errs []string
	for i, raw := range ranked {
		if err := ctx.Err(); err != nil {
			return err
		}
		if logf != nil && len(ranked) > 1 {
			logf(fmt.Sprintf("下载尝试 %d/%d: %s", i+1, len(ranked), SourceLabel(raw)))
		}
		if err := DownloadFile(ctx, raw, dest, progress); err == nil {
			if logf != nil {
				logf("下载来源: " + SourceLabel(raw))
			}
			return nil
		} else {
			errs = append(errs, SourceLabel(raw)+": "+err.Error())
		}
	}
	return fmt.Errorf("所有下载源均失败：%s", strings.Join(errs, "；"))
}
