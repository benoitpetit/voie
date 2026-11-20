const crypto = require("node:crypto");
const zlib = require("node:zlib");

/** Map Node's platform/arch to the release archive suffix. */
function platformKey(platform, arch) {
  if (platform === "win32") return arch === "x64" ? "windows_amd64" : null;
  if (platform === "linux" || platform === "darwin") {
    if (arch === "x64") return `${platform}_amd64`;
    if (arch === "arm64") return `${platform}_arm64`;
  }
  return null;
}

function binaryName(platform) {
  return platform === "win32" ? "voie.exe" : "voie";
}

/** Find the asset by suffix so the package never hardcodes a release version. */
function findAsset(assets, key) {
  if (!key) return null;
  return (
    assets.find(
      (asset) =>
        asset.name.includes(`_${key}.`) &&
        (asset.name.endsWith(".tar.gz") || asset.name.endsWith(".zip")),
    ) || null
  );
}

function verifyChecksum(text, assetName, buffer) {
  const line = text
    .split(/\r?\n/)
    .find((row) => row.trim().endsWith(`  ${assetName}`));
  if (!line) return false;
  const expected = line.trim().split(/\s+/)[0];
  const actual = crypto.createHash("sha256").update(buffer).digest("hex");
  return expected === actual;
}

/** Minimal tar reader: the archives contain one binary and no long paths. */
function extractTarGz(buffer, wanted) {
  const raw = zlib.gunzipSync(buffer);
  let offset = 0;
  while (offset + 512 <= raw.length) {
    const header = raw.subarray(offset, offset + 512);
    if (header.every((byte) => byte === 0)) break;
    const name = header.subarray(0, 100).toString("utf8").split("\0")[0];
    const sizeText = header.subarray(124, 136).toString("utf8").split("\0")[0].trim();
    const size = Number.parseInt(sizeText, 8) || 0;
    const type = String.fromCharCode(header[156]);
    offset += 512;
    if (type === "0" || type === "\0") {
      const data = raw.subarray(offset, offset + size);
      if (name === wanted || name.endsWith(`/${wanted}`)) {
        return Buffer.from(data);
      }
    }
    offset += Math.ceil(size / 512) * 512;
  }
  return null;
}

/** Minimal stored/deflate ZIP reader for the single Windows binary. */
function extractZip(buffer, wanted) {
  let eocd = -1;
  for (let i = buffer.length - 22; i >= 0; i -= 1) {
    if (buffer.readUInt32LE(i) === 0x06054b50) {
      eocd = i;
      break;
    }
  }
  if (eocd < 0) return null;
  const count = buffer.readUInt16LE(eocd + 10);
  let cursor = buffer.readUInt32LE(eocd + 16);
  for (let i = 0; i < count; i += 1) {
    if (buffer.readUInt32LE(cursor) !== 0x02014b50) return null;
    const method = buffer.readUInt16LE(cursor + 10);
    const compressedSize = buffer.readUInt32LE(cursor + 20);
    const nameLength = buffer.readUInt16LE(cursor + 28);
    const extraLength = buffer.readUInt16LE(cursor + 30);
    const commentLength = buffer.readUInt16LE(cursor + 32);
    const localOffset = buffer.readUInt32LE(cursor + 42);
    const name = buffer.subarray(cursor + 46, cursor + 46 + nameLength).toString("utf8");
    const localNameLength = buffer.readUInt16LE(localOffset + 26);
    const localExtraLength = buffer.readUInt16LE(localOffset + 28);
    const dataStart = localOffset + 30 + localNameLength + localExtraLength;
    const compressed = buffer.subarray(dataStart, dataStart + compressedSize);
    if (name === wanted) {
      const out = method === 8 ? zlib.inflateRawSync(compressed) : Buffer.from(compressed);
      return out;
    }
    cursor += 46 + nameLength + extraLength + commentLength;
  }
  return null;
}

module.exports = {
  platformKey,
  binaryName,
  findAsset,
  verifyChecksum,
  extractTarGz,
  extractZip,
};
