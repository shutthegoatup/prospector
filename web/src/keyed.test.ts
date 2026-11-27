import assert from "node:assert/strict";
import { test } from "node:test";
import { current, sameKey } from "./lib/keyed.ts";

test("a key is the same when every part is", () => {
  const data = { items: [] };
  assert.ok(sameKey(["review", 3, undefined], ["review", 3, undefined]));
  assert.ok(sameKey([data], [data]));
  assert.ok(!sameKey(["review"], ["rejected"]));
  assert.ok(!sameKey([data], [{ items: [] }]), "new data is a new key, though it looks the same");
  assert.ok(!sameKey(["review"], ["review", 3]));
});

test("what is held for one key isn't given out under another", () => {
  const held = { key: ["review", undefined], value: "cursor-on-page-2" };
  assert.equal(
    current(held, ["review", undefined], () => undefined),
    "cursor-on-page-2",
  );
  assert.equal(
    current(held, ["rejected", undefined], () => undefined),
    undefined,
  );
});
