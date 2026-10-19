import { test } from "node:test";
import assert from "node:assert/strict";
import { exportFileName } from "../../src/settings/data.ts";

test("the export is saved under the name the hub gives, or a plain one", () => {
  assert.equal(exportFileName('attachment; filename="meat-mode-export-2026-10-14.json"'), "meat-mode-export-2026-10-14.json");
  assert.equal(exportFileName("attachment"), "meat-mode-export.json");
  assert.equal(exportFileName(null), "meat-mode-export.json");
});
