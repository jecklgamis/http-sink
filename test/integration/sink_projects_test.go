package integration

import (
	"testing"
)

func TestCreatesProjectAndReturnsToken(t *testing.T) {
	base := newApp(t)
	name := uniqueName("proj")
	res := do(t, "POST", base+"/sink/projects", map[string]any{"name": name})
	wantStatus(t, res, 201)
	body := res.JSON(t)
	if body["name"] != name {
		t.Fatalf("name = %v, want %s", body["name"], name)
	}
	if tok, _ := body["token"].(string); len(tok) <= 10 {
		t.Fatalf("token = %q, want a string longer than 10 chars", tok)
	}
}

func TestRejectsDuplicateProjectName(t *testing.T) {
	base := newApp(t)
	name := uniqueName("proj")
	do(t, "POST", base+"/sink/projects", map[string]any{"name": name})
	wantStatus(t, do(t, "POST", base+"/sink/projects", map[string]any{"name": name}), 409)
}

func TestRejectsInvalidProjectName(t *testing.T) {
	base := newApp(t)
	wantStatus(t, do(t, "POST", base+"/sink/projects", map[string]any{"name": "has spaces"}), 400)
}

func TestListsProjectsWithoutTokens(t *testing.T) {
	base := newApp(t)
	name, token := createProject(t, base)
	res := do(t, "GET", base+"/sink/projects", nil)
	wantStatus(t, res, 200)
	if got := string(res.Raw); !contains(got, name) || contains(got, token) {
		t.Fatalf("project list should include the name but not the token: %s", got)
	}
}

func TestRejectsLatencyConfigOnBareSink(t *testing.T) {
	base := newApp(t)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency",
		map[string]any{"path": "/sink", "jitterMs": 100}), 400)
}

func TestRejectsConfigForNonexistentProject(t *testing.T) {
	base := newApp(t)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency",
		map[string]any{"path": "/sink/no-such-project-xyz/foo", "jitterMs": 100}), 404)
}

func TestConfigRequiresRightToken(t *testing.T) {
	base := newApp(t)
	name, token := createProject(t, base)
	path := "/sink/" + name + "/scenario"
	cfg := map[string]any{"path": path, "jitterMs": 50}

	wantStatus(t, do(t, "POST", base+"/sink/config/latency", cfg), 403)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency", cfg, withHeader("X-Sink-Token", "wrong-token")), 403)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency", cfg, withHeader("X-Sink-Token", token)), 201)

	del := base + "/sink/config/latency?path=" + path
	wantStatus(t, do(t, "DELETE", del, nil, withHeader("X-Sink-Token", "wrong-token")), 403)
	wantStatus(t, do(t, "DELETE", del, nil, withHeader("X-Sink-Token", token)), 204)
}

func TestAllowsConfiguringProjectRootPath(t *testing.T) {
	base := newApp(t)
	name, token := createProject(t, base)
	path := "/sink/" + name

	wantStatus(t, do(t, "POST", base+"/sink/config/failure",
		map[string]any{"path": path, "rate": 0.5, "statusCode": 500}, withHeader("X-Sink-Token", token)), 201)
	wantStatus(t, do(t, "DELETE", base+"/sink/config/failure?path="+path, nil,
		withHeader("X-Sink-Token", token)), 204)
}

func TestValidatesConfigValues(t *testing.T) {
	base := newApp(t)
	name, token := createProject(t, base)
	path := "/sink/" + name + "/v"
	tok := withHeader("X-Sink-Token", token)

	wantStatus(t, do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path, "jitterMs": -1}, tok), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path, "jitterMs": "abc"}, tok), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/latency", map[string]any{"path": path, "jitterMs": "25"}, tok), 201)
	wantStatus(t, do(t, "POST", base+"/sink/config/failure", map[string]any{"path": path, "rate": 1.5, "statusCode": 500}, tok), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/failure", map[string]any{"path": path, "rate": 0.5, "statusCode": 200}, tok), 400)
	wantStatus(t, do(t, "POST", base+"/sink/config/failure", map[string]any{"path": path, "rate": 0.5, "statusCode": 500.5}, tok), 400)
	wantStatus(t, do(t, "DELETE", base+"/sink/config/latency", nil, tok), 400) // path query param required
}

func TestRejectsDeletingProjectWithoutRightToken(t *testing.T) {
	base := newApp(t)
	name := uniqueName("proj")
	do(t, "POST", base+"/sink/projects", map[string]any{"name": name})

	wantStatus(t, do(t, "DELETE", base+"/sink/projects/"+name, nil), 403)
	wantStatus(t, do(t, "DELETE", base+"/sink/projects/"+name, nil, withHeader("X-Sink-Token", "wrong-token")), 403)
}

func TestDeletingProjectFreesNameAndCleansUpConfigs(t *testing.T) {
	base := newApp(t)
	name, token := createProject(t, base)
	path := "/sink/" + name + "/foo"

	wantStatus(t, do(t, "POST", base+"/sink/config/failure",
		map[string]any{"path": path, "rate": 0.5, "statusCode": 500}, withHeader("X-Sink-Token", token)), 201)

	wantStatus(t, do(t, "DELETE", base+"/sink/projects/"+name, nil, withHeader("X-Sink-Token", token)), 204)

	cfgs := do(t, "GET", base+"/sink/config/failure", nil)
	if contains(string(cfgs.Raw), path) {
		t.Fatalf("failure config for %s survived project deletion: %s", path, cfgs.Raw)
	}

	recreate := do(t, "POST", base+"/sink/projects", map[string]any{"name": name})
	wantStatus(t, recreate, 201)
	if recreate.JSON(t)["token"] == token {
		t.Fatal("recreated project reused the old token")
	}
}
