package agentcore

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPermissionGates(t *testing.T) {
	search := Action{Kind: ActionSearch, Risk: RiskLow}
	write := Action{Kind: ActionWriteFile, Risk: RiskMedium}
	remove := Action{Kind: ActionDeleteFile, Risk: RiskHigh}
	if !PermissionAsk.Allows(search) || PermissionAsk.Allows(write) {
		t.Fatal("ask mode must gate writes")
	}
	if !PermissionAuto.Allows(write) || PermissionAuto.Allows(remove) {
		t.Fatal("auto mode must allow ordinary writes but not destructive deletes")
	}
	if !PermissionFull.Allows(remove) {
		t.Fatal("full access should permit all actions")
	}
}

func TestCompressContextKeepsLatest(t *testing.T) {
	entries := []ContextEntry{{Role: "user", Text: strings.Repeat("a", 40)}, {Role: "assistant", Text: "latest"}}
	got, note := CompressContext(entries, 12)
	if note == "" || len(got) == 0 || got[len(got)-1].Text != "latest" {
		t.Fatalf("CompressContext = %+v, %q", got, note)
	}
}

func TestSearcherParsesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a class="result__a" href="https://example.test/a">Alpha</a><a class="result__snippet">First result</a><a class="result__a" href="https://example.test/b">Beta</a><a class="result__snippet">Second result</a>`))
	}))
	defer srv.Close()
	s := &Searcher{Client: srv.Client(), Endpoint: srv.URL + "/?q="}
	got, err := s.Search(t.Context(), "win95")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Alpha" || got[1].Snippet != "Second result" {
		t.Fatalf("Search = %+v", got)
	}
}

func TestSafeTargetRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	inside, err := SafeTarget(root, root+"/sub/file.txt")
	if err != nil || !strings.HasPrefix(inside, root) {
		t.Fatalf("inside = %q, %v", inside, err)
	}
	if _, err := SafeTarget(root, root+"/../escape"); err == nil {
		t.Fatal("traversal must be rejected")
	}
}
