package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

func TestARobotWorksTasksThroughTheAPIAsItsScopesAllow(t *testing.T) {
	t.Parallel()
	s, srv, _ := agentSite(t, newFakeEndpoint(t))
	o := must(s.OrgBySlug("alpha"))
	key := func(name string, scopes ...string) string {
		secret, _, err := s.CreateKey(o.ID, name, scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, writer := key("reader", "read"), key("planner", "tasks:write")
	tasks := srv.URL + "/o/alpha/api/v1/tasks"
	brief := map[string]any{"title": "Yachts", "brief": "Yacht builders with a configurator."}

	if code, body := asKey(t, "POST", tasks, reader, brief); code != http.StatusForbidden || !strings.Contains(body, "tasks:write") {
		t.Fatalf("a read key starting a task: %d %s", code, body)
	}
	code, body := asKey(t, "POST", tasks, writer, brief)
	var task api.Task
	if json.Unmarshal([]byte(body), &task); code != http.StatusCreated || task.Title != "Yachts" || task.CreatedBy != "agent:planner" || task.ClosedAt != nil {
		t.Fatalf("a tasks key starting a task: %d %s", code, body)
	}

	ask := fmt.Sprintf("%s/%d/requests", tasks, task.Id)
	code, body = asKey(t, "POST", ask, writer, map[string]any{"text": "Now some in Switzerland."})
	var req api.TaskRequest
	if json.Unmarshal([]byte(body), &req); code != http.StatusCreated || req.TaskId != task.Id || req.Text != "Now some in Switzerland." || req.DoneAt != nil {
		t.Fatalf("asking for more: %d %s", code, body)
	}
	if code, _ := asKey(t, "POST", fmt.Sprintf("%s/999/requests", tasks), writer, map[string]any{"text": "More."}); code != http.StatusNotFound {
		t.Fatalf("asking a task that isn't there: %d", code)
	}

	request := fmt.Sprintf("%s/o/alpha/api/v1/task-requests/%d", srv.URL, req.Id)
	code, body = asKey(t, "PATCH", request, writer, map[string]any{"done": true})
	if json.Unmarshal([]byte(body), &req); code != http.StatusOK || req.DoneAt == nil || req.DoneBy != "agent:planner" {
		t.Fatalf("marking a request done: %d %s", code, body)
	}
	code, body = asKey(t, "PATCH", request, writer, map[string]any{"done": false})
	if json.Unmarshal([]byte(body), &req); code != http.StatusOK || req.DoneAt != nil {
		t.Fatalf("marking it not done again: %d %s", code, body)
	}

	change := fmt.Sprintf("%s/%d", tasks, task.Id)
	code, body = asKey(t, "PATCH", change, writer, map[string]any{"closed": true})
	if json.Unmarshal([]byte(body), &task); code != http.StatusOK || task.ClosedAt == nil {
		t.Fatalf("closing the task: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", ask, writer, map[string]any{"text": "One more."}); code == http.StatusCreated {
		t.Fatalf("asking a closed task for more: %d %s", code, body)
	}
	code, body = asKey(t, "PATCH", change, writer, map[string]any{"closed": false})
	if json.Unmarshal([]byte(body), &task); code != http.StatusOK || task.ClosedAt != nil {
		t.Fatalf("opening it again: %d %s", code, body)
	}

	beta := mustOrg(t, s, "beta")
	theirs, _, _ := s.CreateKey(beta.ID, "theirs", []string{"tasks:write"}, "Grace Hopper")
	if code, _ := asKey(t, "PATCH", change, theirs, map[string]any{"closed": true}); code != http.StatusForbidden {
		t.Fatalf("another organisation's key: %d", code)
	}
	if code, _ := asKey(t, "PATCH", srv.URL+"/o/beta/api/v1/tasks/"+fmt.Sprint(task.Id), theirs, map[string]any{"closed": true}); code != http.StatusNotFound {
		t.Fatalf("another organisation's key, at its own address, reaching this task: %d", code)
	}
}
