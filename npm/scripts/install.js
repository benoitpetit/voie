const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const {
  platformKey,
  binaryName,
  findAsset,
  verifyChecksum,
  extractTarGz,
  extractZip,
} = require("./lib");

async function get(url, json = false) {
  const headers = {
    Accept: json ? "application/vnd.github+json" : "application/octet-stream",
    "User-Agent": "voie-ai-installer",
  };
  if (process.env.GITHUB_TOKEN) {
    headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
  }
  const response = await fetch(url, { headers });
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText} from ${url}`);
  }
  return json ? response.json() : Buffer.from(await response.arrayBuffer());
}

async function main() {
  const key = platformKey(process.platform, process.arch);
  if (!key) {
    throw new Error(`unsupported platform: ${process.platform} ${process.arch}`);
  }

  const release = await get(
    "https://api.github.com/repos/benoitpetit/voie/releases/latest",
    true,
  );
  const asset = findAsset(release.assets, key);
  if (!asset) throw new Error(`no release asset for ${key}`);

  const binary = binaryName(process.platform);
  const cacheRoot = process.env.VOIE_CACHE_DIR || path.join(os.homedir(), ".cache", "voie-ai");
  const targetDir = path.join(cacheRoot, release.tag_name);
  const target = path.join(targetDir, binary);
  if (fs.existsSync(target)) {
    console.log(`voie ${release.tag_name} already installed at ${target}`);
    return;
  }

  const archive = await get(asset.browser_download_url);
  const checksumAsset = release.assets.find((item) => item.name === "checksums.txt");
  if (checksumAsset) {
    const checksums = (await get(checksumAsset.browser_download_url)).toString("utf8");
    if (!verifyChecksum(checksums, asset.name, archive)) {
      throw new Error(`SHA-256 mismatch for ${asset.name}`);
    }
  }

  const extracted = asset.name.endsWith(".zip")
    ? extractZip(archive, binary)
    : extractTarGz(archive, binary);
  if (!extracted) throw new Error(`${binary} missing from ${asset.name}`);

  fs.mkdirSync(targetDir, { recursive: true });
  const temporary = `${target}.tmp`;
  fs.writeFileSync(temporary, extracted, { mode: 0o755 });
  fs.renameSync(temporary, target);
  fs.chmodSync(target, 0o755);
  console.log(`voie ${release.tag_name} installed to ${target}`);
}

main().catch((error) => {
  console.error(`voie install failed: ${error.message}`);
  process.exitCode = 1;
});
