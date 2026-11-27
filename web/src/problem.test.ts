import assert from "node:assert/strict";
import { test } from "node:test";
import { mustGoFirst, problemText, problemType, retryQuery } from "./problem.ts";

test("a throttled read isn't tried again; another failure is, three times", () => {
  assert.equal(retryQuery(0, { type: "/api/problems/throttled", status: 429 }), false);
  assert.equal(retryQuery(0, { title: "Bad Gateway", status: 502 }), true);
  assert.equal(retryQuery(3, { title: "Bad Gateway", status: 502 }), false);
});

test("an API problem says its detail", () => {
  assert.equal(
    problemText({ title: "Forbidden", status: 403, detail: "this key lacks the agents:configure scope" }, "Forbidden", 403),
    "this key lacks the agents:configure scope",
  );
});

test("plain text from a sign-in check is shown whole", () => {
  assert.equal(
    problemText("Alpha Co requires its single sign-on: reload the page to go through it\n", "Forbidden", 403),
    "Alpha Co requires its single sign-on: reload the page to go through it",
  );
});

test("with nothing to say, the status stands in, even where HTTP/2 gives no status text", () => {
  assert.equal(problemText(undefined, "Bad Gateway", 502), "Bad Gateway");
  assert.equal(problemText({ title: "Bad Gateway", status: 502 }, "", 502), "The server answered 502.");
  assert.equal(problemText("  ", "", 500), "The server answered 500.");
});

test("a problem is told apart by its type", () => {
  assert.equal(problemType({ type: "/api/problems/throttled", title: "Too many tries", status: 429 }), "/api/problems/throttled");
  assert.equal(problemType({ code: "throttled" }), undefined);
  assert.equal(problemType("this request came from another site"), undefined);
  assert.equal(problemType(null), undefined);
});

test("only a refusal the page itself sorts out reloads it", () => {
  assert.equal(mustGoFirst({ type: "/api/problems/must_change_password" }), true);
  assert.equal(mustGoFirst({ type: "/api/problems/sso_required" }), true);
  assert.equal(mustGoFirst({ type: "/api/problems/signed_out" }), false);
  assert.equal(mustGoFirst({ type: "about:blank" }), false);
  assert.equal(mustGoFirst({ code: "sso_required" }), false);
});
