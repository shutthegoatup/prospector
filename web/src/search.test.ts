import assert from "node:assert/strict";
import { test } from "node:test";
import { addSearch, companiesSearch, enquiriesSearch, parseSearch, researchSearch, stringifySearch, tasksSearch, todaySearch } from "./search.ts";

const read = <T>(schema: { parse: (v: unknown) => T }, address: string) => schema.parse(parseSearch(address));

test("numbers are numbers, and anything else is no number", () => {
  assert.deepEqual(read(researchSearch, "?f=contact&task=3"), { f: "contact", task: 3 });
  assert.equal(read(researchSearch, "?task=abc").task, undefined);
  assert.equal(read(researchSearch, "?task=-2").task, undefined);
  assert.equal(read(researchSearch, "?task=1.5").task, undefined);
});

test("a filter that isn't one falls back to the page's default", () => {
  assert.equal(read(researchSearch, "?f=nonsense").f, undefined);
  assert.equal(read(todaySearch, "?step=linkedin").step, "linkedin");
  assert.equal(read(todaySearch, "?step=fax").step, undefined);
  assert.equal(read(enquiriesSearch, "?state=open&kind=nope").state, "open");
  assert.equal(read(enquiriesSearch, "?state=open&kind=nope").kind, undefined);
  assert.equal(read(companiesSearch, "?stage=bogus").stage, undefined);
});

test("text stays text, however it looks", () => {
  for (const q of ["2024", "1e5", "1.50", "null", "true", "007", "Larkmoor Yachts", '"quoted"']) {
    assert.equal(read(companiesSearch, "?q=" + encodeURIComponent(q)).q, q, q);
  }
});

test("text goes into the address as written, and reads back the same", () => {
  for (const q of ["2024", "1e5", "true", "Larkmoor Yachts"]) {
    const address = stringifySearch({ q });
    assert.equal(address, "?q=" + encodeURIComponent(q).replace(/%20/g, "+"), q);
    assert.equal(read(companiesSearch, address).q, q, q);
  }
  assert.equal(stringifySearch({ task: 3, closed: true }), "?task=3&closed=true");
});

test("a flag is on with a value, as the bookmarklet and old links give it", () => {
  assert.equal(read(addSearch, "?popup=1&name=Hannah").popup, true);
  assert.equal(read(tasksSearch, "?closed=true").closed, true);
  assert.equal(read(tasksSearch, "?closed=0").closed, false);
  assert.equal(read(tasksSearch, "").closed, undefined);
  assert.equal(read(enquiriesSearch, "?spam=1").spam, true);
});
