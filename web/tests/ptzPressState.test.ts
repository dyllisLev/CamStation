import assert from "node:assert/strict";
import test from "node:test";
import { updatePtzPressSources } from "../src/components/live/ptzPressState.ts";

test("keeps a hold pressed until every active input source releases", () => {
  let active = updatePtzPressSources(new Set(), "pointer", true);
  active = updatePtzPressSources(active, "keyboard", true);
  active = updatePtzPressSources(active, "pointer", false);
  assert.deepEqual([...active], ["keyboard"]);
  active = updatePtzPressSources(active, "keyboard", false);
  assert.equal(active.size, 0);
});
