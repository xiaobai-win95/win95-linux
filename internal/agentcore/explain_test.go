package agentcore

import (
	"strings"
	"testing"
)

func TestExplainCommand(t *testing.T) {
	ex := ExplainCommand("cp -r old new")
	if ex.Purpose == "" || ex.Risk != RiskMedium || len(ex.Arguments) != 2 {
		t.Fatalf("ExplainCommand = %+v", ex)
	}
	if !strings.Contains(ex.Summary(), "old") || !strings.Contains(ex.Summary(), "new") {
		t.Fatalf("summary missing arguments: %s", ex.Summary())
	}
}

func TestExplainDangerousAndQuoted(t *testing.T) {
	if got := ExplainCommand("rm -rf /"); got.Risk != RiskHigh {
		t.Fatalf("rm risk = %v", got.Risk)
	}
	got := ExplainCommand(`echo "hello world"`)
	if len(got.Arguments) != 1 || got.Arguments[0].Value != "hello world" {
		t.Fatalf("quoted argument = %+v", got.Arguments)
	}
}
