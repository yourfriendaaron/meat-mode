import { test } from "node:test";
import assert from "node:assert/strict";
import { EXPORT_FALLBACK_NAME } from "../../src/settings/data.ts";

test("the export is saved as a zip when the hub's reply names no file", () => {
  assert.equal(EXPORT_FALLBACK_NAME, "meat-mode-export.zip");
});
