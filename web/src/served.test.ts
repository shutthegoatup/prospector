import assert from "node:assert/strict";
import { test } from "node:test";
import { parseServed, stepMeta } from "./served.ts";

test("a served page is read by its name", () => {
  assert.deepEqual(parseServed(`{"page":"problem","code":404,"title":"Nothing here","message":"Gone.","back":"/"}`), {
    page: "problem",
    code: 404,
    title: "Nothing here",
    message: "Gone.",
    back: "/",
  });
  assert.equal(parseServed(`{"page":"customer","state":"odd","steps":[]}`)?.page, "customer");
});

test("anything else routes as usual", () => {
  for (const raw of [undefined, "", "not json", `{"page":"elsewhere"}`, `{"page":"problem","code":"404"}`]) {
    assert.equal(parseServed(raw), undefined, raw);
  }
});

test("a step says when it was done or is due", () => {
  const st = { id: 1, name: "Send files", due: "2026-11-20", done_on: null, mine: false };
  assert.equal(stepMeta(st, "2026-11-26"), "was due 2026-11-20");
  assert.equal(stepMeta(st, "2026-11-01"), "due 2026-11-20");
  assert.equal(stepMeta({ ...st, done_on: "2026-11-21" }, "2026-11-26"), "done 2026-11-21");
  assert.equal(stepMeta({ ...st, due: null }, "2026-11-26"), "");
});
