import assert from "node:assert/strict";
import { test } from "node:test";
import { safeHref } from "./lib/links.ts";

test("a web address is linked as it is", () => {
  assert.equal(safeHref(" https://www.linkedin.com/in/hannah-pryce "), "https://www.linkedin.com/in/hannah-pryce");
  assert.equal(safeHref("HTTP://larkmoor.example/team"), "HTTP://larkmoor.example/team");
});

test("anything else links nowhere", () => {
  for (const u of ["javascript:alert(1)", " JavaScript:alert(1)", "data:text/html,<b>x</b>", "//elsewhere.example", "larkmoor.example", "https://a b", ""]) {
    assert.equal(safeHref(u), "#", u);
  }
});
