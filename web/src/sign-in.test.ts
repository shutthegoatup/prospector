import assert from "node:assert/strict";
import { test } from "node:test";
import { failure, offered, withNext } from "./lib/sign-in.ts";

test("a provider's link keeps where the person was going", () => {
  assert.equal(withNext("/login/with/google", "/o/alpha/research?f=review"), "/login/with/google?next=%2Fo%2Falpha%2Fresearch%3Ff%3Dreview");
  assert.equal(withNext("/login/with/google", undefined), "/login/with/google");
});

test("the organisation that asked is offered alone", () => {
  const found = [{ slug: "alpha" }, { slug: "beta" }];
  assert.deepEqual(offered(found, "beta"), [{ slug: "beta" }]);
  assert.deepEqual(offered(found, "gamma"), found);
  assert.deepEqual(offered(found, undefined), found);
  assert.deepEqual(offered(undefined, "alpha"), []);
});

test("every failure says something", () => {
  const otherwise = "it didn't go through";
  assert.equal(failure(undefined, otherwise), undefined);
  assert.equal(failure(null, otherwise), undefined);
  assert.equal(failure({ detail: "wrong email or password" }, otherwise), "wrong email or password");
  assert.equal(failure("this request came from another site\n", otherwise), "this request came from another site");
  for (const e of [new TypeError("Failed to fetch"), {}, "", { detail: "" }]) {
    assert.equal(failure(e, otherwise), otherwise, JSON.stringify(e));
  }
});
