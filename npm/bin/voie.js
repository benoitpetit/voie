#!/usr/bin/env node
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const cacheRoot = process.env.VOIE_CACHE_DIR || path.join(os.homedir(), ".cache", "voie-ai");
const fail = (message) => {
  console.error(`voie: ${message}`);
  process.exit(1);
};

if (!fs.existsSync(cacheRoot)) {
  fail(`no binary installed. Run: npm install voie-ai (cache: ${cacheRoot})`);
}

const versions = fs
  .readdirSync(cacheRoot)
  .filter((name) => /^v?\d+\.\d+\.\d+$/.test(name))
  .sort((a, b) => {
    const left = a.replace(/^v/, "").split(".").map(Number);
    const right = b.replace(/^v/, "").split(".").map(Number);
    for (let i = 0; i < 3; i += 1) {
      if (left[i] !== right[i]) return right[i] - left[i];
    }
    return 0;
  });

if (versions.length === 0) {
  fail(`no binary installed. Run: npm install voie-ai (cache: ${cacheRoot})`);
}

const binary = path.join(
  cacheRoot,
  versions[0],
  process.platform === "win32" ? "voie.exe" : "voie",
);
if (!fs.existsSync(binary)) {
  fail(`binary missing at ${binary}. Reinstall voie-ai.`);
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  fail(result.error.message);
}
process.exit(result.status ?? 1);
