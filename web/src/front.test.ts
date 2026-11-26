import assert from "node:assert/strict";
import { test } from "node:test";
import { formShows, outcomeOf, seatTotal } from "./front/enquiry.ts";

test("the enquiry form shows only at the origin its organisation takes enquiries from", () => {
  assert.equal(formShows("/o/prospector/intake", "https://crm.alpha.example", "https://crm.alpha.example"), true);
  assert.equal(formShows("/o/prospector/intake", "https://crm.alpha.example", "http://10.0.0.5:8766"), false);
  assert.equal(formShows("", "", "https://crm.alpha.example"), false);
});

test("the hosted price adds up per user, whatever is typed", () => {
  assert.equal(seatTotal("4"), "£20 a month for 4 users");
  assert.equal(seatTotal("1"), "£5 a month for 1 user");
  assert.equal(seatTotal("1000"), "£2,500 a month for 500 users");
  assert.equal(seatTotal(""), "£5 a month for 1 user");
  assert.equal(seatTotal("-3"), "£5 a month for 1 user");
});

test("an answer from the intake says what the visitor is told", () => {
  assert.equal(outcomeOf(200), "sent");
  assert.equal(outcomeOf(400), "invalid");
  assert.equal(outcomeOf(429), "busy");
  assert.equal(outcomeOf(403), "refused");
  assert.equal(outcomeOf(502), undefined);
});
