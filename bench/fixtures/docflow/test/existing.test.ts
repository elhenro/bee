// regression suite for the shipped workflow. must keep passing after the
// feature is added.

import { test } from "node:test";
import assert from "node:assert/strict";
import { createService } from "../src/index.ts";
import { IllegalTransitionError } from "../src/errors.ts";

test("createDraft starts in draft and emits created", () => {
  const { service, bus } = createService();
  const seen: string[] = [];
  bus.on((e) => seen.push(e.type));
  const d = service.createDraft("t", "b");
  assert.equal(d.status, "draft");
  assert.deepEqual(seen, ["created"]);
});

test("draft -> review -> published happy path", () => {
  const { service } = createService();
  const d = service.createDraft("t", "b");
  service.submitForReview(d.id);
  const pub = service.publish(d.id);
  assert.equal(pub.status, "published");
  assert.ok(pub.publishedAt && pub.publishedAt > 0);
});

test("cannot publish straight from draft", () => {
  const { service } = createService();
  const d = service.createDraft("t", "b");
  assert.throws(() => service.publish(d.id), IllegalTransitionError);
});
