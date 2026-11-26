import assert from "node:assert/strict";
import { test } from "node:test";
import { safeNext } from "./lib/next.ts";

test("a path on this site is kept", () => {
  assert.equal(safeNext("/o/alpha/today?step=call"), "/o/alpha/today?step=call");
});

test("anywhere else goes home", () => {
  for (const n of [undefined, "", "https://elsewhere.example/", "//elsewhere.example", "/\\elsewhere.example", "/\t/elsewhere.example", "/\n/elsewhere.example", "o/alpha"]) {
    assert.equal(safeNext(n), "/", JSON.stringify(n));
  }
});
