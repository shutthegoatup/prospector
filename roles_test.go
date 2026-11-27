package main

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestChangingSomeonesRole(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	grace := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	base := srv.URL + "/o/alpha/"
	role := func(c *http.Client, user int64, to string) (int, string) {
		return call(t, c, "PATCH", base+"api/v1/members/"+itoa(user), "application/json", map[string]any{"role": to})
	}
	makesKey := func(c *http.Client) int {
		code, _ := call(t, c, "POST", base+"api/v1/keys", "application/json", map[string]any{"name": "bot", "scopes": []string{"read"}})
		return code
	}

	// Settings offers Make owner, to an owner, for each member the team list names as one.
	if _, body := call(t, ada, "GET", base+"api/v1/nav", "", nil); !strings.Contains(body, `"owner":true`) {
		t.Fatalf("an owner isn't offered to make a member an owner: %s", body)
	}
	if _, body := call(t, ada, "GET", base+"api/v1/members", "", nil); !strings.Contains(body, `"email":"grace@example.com","name":"Grace Hopper","role":"member"`) {
		t.Fatalf("the team list doesn't say who is a member: %s", body)
	}
	if code, _ := role(asGrace, grace.ID, "owner"); code != http.StatusForbidden {
		t.Fatalf("a member made themself an owner: %d", code)
	}
	if code := makesKey(asGrace); code != http.StatusForbidden {
		t.Fatalf("a member made a key: %d", code)
	}
	if code, body := role(ada, grace.ID, "owner"); code != http.StatusOK {
		t.Fatalf("make owner: %d %s", code, body)
	}
	if code := makesKey(asGrace); code != http.StatusCreated {
		t.Fatalf("a new owner can't make a key: %d", code)
	}
	if code, body := role(ada, grace.ID, "member"); code != http.StatusOK {
		t.Fatalf("make member: %d %s", code, body)
	}
	if code := makesKey(asGrace); code != http.StatusForbidden {
		t.Fatalf("someone made a member again still acts as an owner: %d", code)
	}
	if code, body := role(ada, must(s.UserByEmail("ada@example.com")).ID, "member"); code != http.StatusBadRequest || !strings.Contains(body, "needs an owner") {
		t.Fatalf("the last owner stepped down: %d %s", code, body)
	}
	if code, _ := role(ada, grace.ID, "admin"); code != http.StatusBadRequest {
		t.Fatalf("an unknown role was accepted: %d", code)
	}
	outsider := must(s.CreateUser("carol@example.com", "Carol Outsider", "correct horse battery"))
	if code, _ := role(ada, outsider.ID, "owner"); code != http.StatusNotFound {
		t.Fatalf("someone outside the organisation was given a role: %d", code)
	}
	if _, err := s.Membership(o.ID, outsider.ID); err == nil {
		t.Fatal("changing a role added someone to the organisation")
	}
}

func TestChangingAKeysScopes(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	asGrace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	secret, k, err := s.CreateKey(o.ID, "bot", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	base := srv.URL + "/o/alpha/"
	bearer := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("Authorization", "Bearer "+secret)
		return http.DefaultTransport.RoundTrip(r)
	})}
	scopes := func(c *http.Client, id int64, to ...string) (int, string) {
		return call(t, c, "PATCH", base+"api/v1/keys/"+itoa(id), "application/json", map[string]any{"scopes": to})
	}
	company := AddCompanyIn{Name: "Fjordline Saunas", Facts: []FactIn{{Field: "configurator", Value: "Kickflip", SourceURL: "https://fjordline.example"}}}
	ctx := context.Background()
	connect := func() *mcp.ClientSession {
		session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
			Connect(ctx, &mcp.StreamableClientTransport{Endpoint: base + "mcp", HTTPClient: bearer}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	tools := func(session *mcp.ClientSession) []string {
		list, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range list.Tools {
			names = append(names, tool.Name)
		}
		return names
	}

	// The Agents page offers Change scopes, to an owner, on each key the list names.
	if _, body := call(t, ada, "GET", base+"api/v1/nav", "", nil); !strings.Contains(body, `"owner":true`) {
		t.Fatalf("an owner isn't offered to change a key's scopes: %s", body)
	}
	if _, body := call(t, ada, "GET", base+"api/v1/keys", "", nil); !strings.Contains(body, `"name":"bot"`) {
		t.Fatalf("the key list doesn't name the key: %s", body)
	}
	if code, _ := call(t, bearer, "POST", base+"api/v1/companies", "application/json", company); code != http.StatusForbidden {
		t.Fatalf("a read key wrote: %d", code)
	}
	open := connect()
	if slices.Contains(tools(open), "add_company") {
		t.Fatal("a read key's MCP session can add companies")
	}
	if code, _ := scopes(asGrace, k.ID, "read", "research:write"); code != http.StatusForbidden {
		t.Fatalf("a member changed a key's scopes: %d", code)
	}

	if code, body := scopes(ada, k.ID, "read", "research:write"); code != http.StatusOK {
		t.Fatalf("change scopes: %d %s", code, body)
	}
	if code, body := call(t, bearer, "POST", base+"api/v1/companies", "application/json", company); code != http.StatusOK {
		t.Fatalf("the same secret can't use its new scope: %d %s", code, body)
	}
	if _, err := open.ListTools(ctx, nil); err == nil {
		t.Fatal("an MCP session opened under the old scopes carried on")
	}
	if !slices.Contains(tools(connect()), "add_company") {
		t.Fatal("a new MCP session doesn't get the new scope's tools")
	}

	stillOpen := connect()
	if code, body := scopes(ada, k.ID, "research:write", "read"); code != http.StatusOK {
		t.Fatalf("resend scopes: %d %s", code, body)
	}
	if _, err := stillOpen.ListTools(ctx, nil); err != nil {
		t.Fatalf("the same scopes in another order ended an open session: %v", err)
	}

	if code, body := scopes(ada, k.ID, "read"); code != http.StatusOK {
		t.Fatalf("narrow scopes: %d %s", code, body)
	}
	if code, _ := call(t, bearer, "POST", base+"api/v1/companies", "application/json", company); code != http.StatusForbidden {
		t.Fatalf("a narrowed key still writes: %d", code)
	}
	if slices.Contains(tools(connect()), "add_company") {
		t.Fatal("a narrowed key's new MCP session still has the old tools")
	}

	for _, bad := range [][]string{{}, {"read", "everything"}} {
		if code, _ := scopes(ada, k.ID, bad...); code != http.StatusBadRequest {
			t.Fatalf("scopes %v accepted: %d", bad, code)
		}
	}
	other := mustOrg(t, s, "beta")
	_, theirs, _ := s.CreateKey(other.ID, "beta-bot", []string{"read"}, "test")
	if code, _ := scopes(ada, theirs.ID, "read", "research:write"); code != http.StatusBadRequest {
		t.Fatalf("another organisation's key was changed: %d", code)
	}
	if s.DB.First(theirs, theirs.ID); theirs.Scopes != "read" {
		t.Fatalf("another organisation's key now has %q", theirs.Scopes)
	}
	builtin := &APIKey{OrgID: o.ID, Name: "agent", Prefix: builtinPrefix, Hash: "builtin:test", Scopes: "read research:write"}
	s.DB.Create(builtin)
	if code, body := scopes(ada, builtin.ID, "read"); code != http.StatusBadRequest || !strings.Contains(body, "own agents") {
		t.Fatalf("a built-in agent's key was changed here: %d %s", code, body)
	}
	if err := s.RevokeKey(o.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := scopes(ada, k.ID, "read", "research:write"); code != http.StatusBadRequest {
		t.Fatalf("a revoked key's scopes were changed: %d", code)
	}
}
