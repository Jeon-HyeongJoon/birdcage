// Contract test: the Go helper writes /protocol/fixtures/*.json; the TS side must accept them.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { decode, PROTOCOL_VERSION } from "../src/protocol";
import { validatePayload } from "../src/validators";

const dir = join(__dirname, "..", "..", "..", "protocol", "fixtures");
const files = readdirSync(dir).filter((f) => f.endsWith(".json"));

describe("protocol fixtures (Go ↔ TS contract)", () => {
  it("has fixtures", () => {
    expect(files.length).toBeGreaterThanOrEqual(9);
  });
  for (const f of files) {
    it(f, () => {
      const env = decode(readFileSync(join(dir, f), "utf8"));
      expect(env.v).toBe(PROTOCOL_VERSION);
      expect(env.type).toBe(f.replace(/\.json$/, ""));
      expect(validatePayload(env.type, env.data)).toEqual([]);
    });
  }
  it("catches a renamed field", () => {
    const env = decode(readFileSync(join(dir, "job.done.json"), "utf8"));
    const bad = JSON.parse(JSON.stringify(env.data));
    bad.job.id = bad.job.jobId;
    delete bad.job.jobId;
    expect(validatePayload("job.done", bad)).toContain("job.done.job.jobId: missing");
  });
});
