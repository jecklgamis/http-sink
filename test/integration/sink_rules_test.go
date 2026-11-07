package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jecklgamis/http-sink/internal/server"
)

// newAppWithRules starts an app with static rules installed before serving.
func newAppWithRules(t *testing.T, rules ...server.Rule) string {
	t.Helper()
	app := server.New(server.Options{})
	if err := app.SetRules(rules); err != nil {
		t.Fatalf("SetRules: %v", err)
	}
	return serve(t, app)
}

func TestRuleAppliesToItsExactPathOnly(t *testing.T) {
	base := newAppWithRules(t,
		server.Rule{Path: "/sink/orders", Status: "503"},
		server.Rule{Path: "/sink/slow", Delay: "60ms"},
	)

	res := do(t, "GET", base+"/sink/orders", nil)
	wantStatus(t, res, 503)
	if res.JSON(t)["injectedFailure"] != true {
		t.Fatalf("body = %s", res.Raw)
	}
	wantStatus(t, do(t, "GET", base+"/sink/orders/sub", nil), 200) // exact match only
	wantStatus(t, do(t, "GET", base+"/sink/other", nil), 200)

	res, took := timedGet(t, base+"/sink/slow")
	wantStatus(t, res, 200)
	if took < 60*time.Millisecond || res.Header.Get("X-Sink-Injected-Delay-Ms") != "60" {
		t.Fatalf("rule delay: took %v, header %q", took, res.Header.Get("X-Sink-Injected-Delay-Ms"))
	}
}

func TestRuleSupportsProbabilityLists(t *testing.T) {
	base := newAppWithRules(t, server.Rule{Path: "/sink/flaky", Status: "500:50"})
	const n = 400
	failed := 0
	for i := 0; i < n; i++ {
		if do(t, "GET", base+"/sink/flaky", nil).Status == 500 {
			failed++
		}
	}
	if failed < n*30/100 || failed > n*70/100 {
		t.Fatalf("%d/%d failed at 50%%", failed, n)
	}
}

func TestRequestSettingsOverrideRule(t *testing.T) {
	base := newAppWithRules(t, server.Rule{Path: "/sink/ovr", Status: "500", Delay: "200ms"})

	wantStatus(t, do(t, "GET", base+"/sink/ovr", nil), 500) // the rule applies by default

	// the request's own status replaces the rule's, and its fixed 1ms delay
	// replaces the rule's 200ms (the header reports what was applied)
	res := do(t, "GET", base+"/sink/ovr?sinkStatus=502&sinkDelay=1ms", nil)
	wantStatus(t, res, 502)
	if got := res.Header.Get("X-Sink-Injected-Delay-Ms"); got != "1" {
		t.Fatalf("injected delay = %q, want the request's 1ms", got)
	}

	// delay and status are overridden independently: only the status here,
	// so the rule's delay still applies
	res = do(t, "GET", base+"/sink/ovr?sinkStatus=503", nil)
	wantStatus(t, res, 503)
	if got := res.Header.Get("X-Sink-Injected-Delay-Ms"); got != "200" {
		t.Fatalf("rule delay = %q, want 200", got)
	}
}

func TestRulesEndpointListsRulesReadOnly(t *testing.T) {
	base := newAppWithRules(t,
		server.Rule{Path: "/sink/b", Status: "503:10,429:5"},
		server.Rule{Path: "/sink/a", Delay: "50ms-300ms"},
	)
	res := do(t, "GET", base+"/sink/rules", nil)
	wantStatus(t, res, 200)
	body := string(res.Raw)
	if !contains(body, `"/sink/a"`) || !contains(body, `"503:10,429:5"`) || strings.Index(body, "/sink/a") > strings.Index(body, "/sink/b") {
		t.Fatalf("rules should be listed sorted by path: %s", body)
	}

	// it is control plane (not counted as sink traffic) and cannot be modified
	stats := do(t, "GET", base+"/sink/stats", nil).JSON(t)
	if n := len(stats["recentRequests"].([]any)); n != 0 {
		t.Fatalf("/sink/rules must not be recorded as sink traffic, got %d recent", n)
	}
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		if res := do(t, m, base+"/sink/rules", map[string]any{"path": "/sink/z", "status": "500"}); res.JSON(t)["method"] != m {
			t.Fatalf("%s /sink/rules should just be echoed, got %s", m, res.Raw)
		}
	}
	if res := do(t, "GET", base+"/sink/z", nil); res.Status != 200 {
		t.Fatalf("a rule must not be creatable at runtime, got %d", res.Status)
	}
}

func TestNoRulesMeansEmptyList(t *testing.T) {
	res := do(t, "GET", newApp(t)+"/sink/rules", nil)
	wantStatus(t, res, 200)
	if strings.TrimSpace(string(res.Raw)) != "[]" {
		t.Fatalf("body = %s, want []", res.Raw)
	}
}

func TestRuntimeConfigAPIIsGone(t *testing.T) {
	base := newApp(t)
	// the old endpoints no longer exist; they are plain echoed sink paths
	for _, path := range []string{"/sink/config/latency", "/sink/config/failure", "/sink/projects"} {
		for _, m := range []string{"GET", "POST", "DELETE"} {
			res := do(t, m, base+path, nil)
			wantStatus(t, res, 200)
			if got := res.JSON(t); got["method"] != m || got["path"] != path {
				t.Fatalf("%s %s should be echoed, got %s", m, path, res.Raw)
			}
		}
	}
	// ...and posting a "config" has no effect on traffic
	do(t, "POST", base+"/sink/config/failure", map[string]any{"path": "/sink/p", "rate": 1, "statusCode": 500})
	wantStatus(t, do(t, "GET", base+"/sink/p", nil), 200)
}

func TestSetRulesValidation(t *testing.T) {
	tests := []struct {
		name string
		rule server.Rule
	}{
		{"path outside /sink", server.Rule{Path: "/other", Status: "500"}},
		{"empty path", server.Rule{Status: "500"}},
		{"no delay or status", server.Rule{Path: "/sink/a"}},
		{"bad delay", server.Rule{Path: "/sink/a", Delay: "soon"}},
		{"delay over the maximum", server.Rule{Path: "/sink/a", Delay: "11s"}},
		{"bad status", server.Rule{Path: "/sink/a", Status: "200"}},
		{"percentages over 100", server.Rule{Path: "/sink/a", Status: "500:70,503:70"}},
		{"reserved path", server.Rule{Path: "/sink/stats", Status: "500"}},
		{"reserved rules path", server.Rule{Path: "/sink/rules", Delay: "1s"}},
	}
	for _, tc := range tests {
		if err := server.New(server.Options{}).SetRules([]server.Rule{tc.rule}); err == nil {
			t.Errorf("%s: SetRules should fail", tc.name)
		}
	}
	dup := []server.Rule{{Path: "/sink/a", Status: "500"}, {Path: "/sink/a", Delay: "1s"}}
	if err := server.New(server.Options{}).SetRules(dup); err == nil {
		t.Error("duplicate paths should fail")
	}
	// a bare /sink rule is allowed
	if err := server.New(server.Options{}).SetRules([]server.Rule{{Path: "/sink", Status: "500:1"}}); err != nil {
		t.Errorf("/sink rule rejected: %v", err)
	}
}

func TestLoadRulesFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	good := write("good.json", `{"rules":[{"path":"/sink/orders","delay":"50ms-300ms:20","status":"503:10,429:5"}]}`)
	rules, err := server.LoadRulesFile(good)
	if err != nil || len(rules) != 1 || rules[0].Path != "/sink/orders" || rules[0].Status != "503:10,429:5" {
		t.Fatalf("good file: %+v, %v", rules, err)
	}

	for name, content := range map[string]string{
		"malformed.json": `{"rules": [`,
		"unknown.json":   `{"rules":[{"path":"/sink/a","status":"500","statuss":"typo"}]}`,
		"topkey.json":    `{"rule":[]}`,
	} {
		if _, err := server.LoadRulesFile(write(name, content)); err == nil {
			t.Errorf("%s should fail (typos must not be silently ignored)", name)
		}
	}
	if _, err := server.LoadRulesFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("a missing file should fail")
	}
}

func TestReplicasWithTheSameRulesBehaveIdentically(t *testing.T) {
	rule := server.Rule{Path: "/sink/orders", Status: "503"}
	a, b := newAppWithRules(t, rule), newAppWithRules(t, rule)
	for _, base := range []string{a, b} {
		wantStatus(t, do(t, "GET", base+"/sink/orders", nil), 503)
		wantStatus(t, do(t, "GET", base+"/sink/other", nil), 200)
	}
}
