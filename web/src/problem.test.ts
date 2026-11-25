import assert from "node:assert/strict";
import { test } from "node:test";
import { problemText } from "./problem.ts";

test("an API problem says its detail", () => {
  assert.equal(problemText({ title: "Forbidden", status: 403, detail: "this key lacks the agents:configure scope" }, "Forbidden", 403), "this key lacks the agents:configure scope");
});

test("plain text from a sign-in check is shown whole", () => {
  assert.equal(problemText("Alpha Co requires its single sign-on: reload the page to go through it\n", "Forbidden", 403), "Alpha Co requires its single sign-on: reload the page to go through it");
});

test("with nothing to say, the status stands in, even where HTTP/2 gives no status text", () => {
  assert.equal(problemText(undefined, "Bad Gateway", 502), "Bad Gateway");
  assert.equal(problemText({ title: "Bad Gateway", status: 502 }, "", 502), "The server answered 502.");
  assert.equal(problemText("  ", "", 500), "The server answered 500.");
});
