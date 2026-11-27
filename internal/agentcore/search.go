package agentcore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Searcher struct {
	Client   *http.Client
	Endpoint string
}

func DefaultSearcher() *Searcher {
	return &Searcher{Client: &http.Client{Timeout: 15 * time.Second}, Endpoint: "https://html.duckduckgo.com/html/?q="}
}

func (s *Searcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("搜索词为空")
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = "https://html.duckduckgo.com/html/?q="
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Win95Linux/26.6)")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("搜索服务返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	return parseDuckDuckGo(data)
}

func parseDuckDuckGo(data []byte) ([]SearchResult, error) {
	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	var links []SearchResult
	var snippets []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			class := attr(n, "class")
			if n.Data == "a" && hasClass(class, "result__a") {
				href := attr(n, "href")
				if u, err := url.Parse(href); err == nil {
					if target := u.Query().Get("uddg"); target != "" {
						href = target
					}
				}
				if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
					links = append(links, SearchResult{Title: strings.TrimSpace(nodeText(n)), URL: href})
				}
			}
			if hasClass(class, "result__snippet") {
				snippets = append(snippets, strings.TrimSpace(nodeText(n)))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	for i := range links {
		if i < len(snippets) {
			links[i].Snippet = snippets[i]
		}
	}
	if len(links) > 10 {
		links = links[:10]
	}
	return links, nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(class, want string) bool {
	for _, part := range strings.Fields(class) {
		if part == want {
			return true
		}
	}
	return false
}

func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(nodeText(c))
	}
	return b.String()
}
