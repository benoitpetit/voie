const test = require("node:test");
const assert = require("node:assert/strict");
const zlib = require("node:zlib");
const {
  platformKey,
  binaryName,
  findAsset,
  verifyChecksum,
  requireChecksumAsset,
  extractTarGz,
  extractZip,
} = require("./lib");

const checksumText =
  "70b2e55ab027f2e8ed4e24cb9312314bdc50aa6da195149b6e07199d8e3eecf3  voie_0.1.2_linux_amd64.tar.gz\n";

const payload = Buffer.from("BINARY-CONTENT");

function tarFixture(name, content) {
  const header = Buffer.alloc(512);
  header.write(name, 0, 100, "utf8");
  header.write("0000755\0", 100, 8, "utf8");
  header.write(content.length.toString(8).padStart(11, "0") + "\0", 124, 12, "utf8");
  header.write("0", 156, 1, "utf8");
  const data = Buffer.alloc(512);
  content.copy(data);
  return zlib.gzipSync(Buffer.concat([header, data, Buffer.alloc(1024)]));
}

function zipFixture(name, content) {
  const nameBuf = Buffer.from(name);
  const local = Buffer.alloc(30);
  local.writeUInt32LE(0x04034b50, 0);
  local.writeUInt16LE(nameBuf.length, 26);
  const central = Buffer.alloc(46);
  central.writeUInt32LE(0x02014b50, 0);
  central.writeUInt16LE(0, 10);
  central.writeUInt32LE(content.length, 20);
  central.writeUInt32LE(content.length, 24);
  central.writeUInt16LE(nameBuf.length, 28);
  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0);
  eocd.writeUInt16LE(1, 8);
  eocd.writeUInt16LE(1, 10);
  eocd.writeUInt32LE(central.length + nameBuf.length, 12);
  eocd.writeUInt32LE(30 + nameBuf.length + content.length, 16);
  return Buffer.concat([
    local,
    nameBuf,
    content,
    central,
    nameBuf,
    eocd,
  ]);
}

test("platformKey maps supported systems to release suffixes", () => {
  assert.equal(platformKey("linux", "x64"), "linux_amd64");
  assert.equal(platformKey("linux", "arm64"), "linux_arm64");
  assert.equal(platformKey("darwin", "x64"), "darwin_amd64");
  assert.equal(platformKey("darwin", "arm64"), "darwin_arm64");
  assert.equal(platformKey("win32", "x64"), "windows_amd64");
  assert.equal(platformKey("win32", "arm64"), null);
  assert.equal(platformKey("freebsd", "x64"), null);
});

test("binaryName depends on the host", () => {
  assert.equal(binaryName("win32"), "voie.exe");
  assert.equal(binaryName("linux"), "voie");
});

test("findAsset matches the suffix without hardcoding a version", () => {
  const assets = [
    { name: "checksums.txt" },
    { name: "voie_9.9.9_linux_amd64.tar.gz", browser_download_url: "https://example/linux" },
    { name: "voie_9.9.9_windows_amd64.zip", browser_download_url: "https://example/windows" },
  ];
  assert.equal(findAsset(assets, "linux_amd64").browser_download_url, "https://example/linux");
  assert.equal(findAsset(assets, "windows_amd64").browser_download_url, "https://example/windows");
  assert.equal(findAsset(assets, "linux_arm64"), null);
});

test("verifyChecksum checks the release checksums file", () => {
  assert.equal(
    verifyChecksum(checksumText, "voie_0.1.2_linux_amd64.tar.gz", Buffer.from("x")),
    false,
  );
  const crypto = require("node:crypto");
  const content = Buffer.from("hello");
  const digest = crypto.createHash("sha256").update(content).digest("hex");
  const text = `${digest}  voie_0.1.2_linux_amd64.tar.gz\n`;
  assert.equal(verifyChecksum(text, "voie_0.1.2_linux_amd64.tar.gz", content), true);
});

test("extractTarGz returns the binary", () => {
  assert.deepEqual(extractTarGz(tarFixture("voie", payload), "voie"), payload);
  assert.equal(extractTarGz(tarFixture("voie", payload), "other"), null);
});

test("extractZip returns the Windows binary", () => {
  assert.deepEqual(extractZip(zipFixture("voie.exe", payload), "voie.exe"), payload);
  assert.equal(extractZip(zipFixture("voie.exe", payload), "voie"), null);
});

test('requireChecksumAsset rejects a release without checksums.txt', () => {
  assert.throws(
    () => requireChecksumAsset({ assets: [{ name: "voie_0.1.2_linux_amd64.tar.gz" }] }),
    /checksums\.txt/,
  );
});

test('requireChecksumAsset returns the checksum asset when present', () => {
  const checksums = { name: "checksums.txt", browser_download_url: "https://example.invalid/checksums.txt" };
  const release = { assets: [{ name: "voie.tar.gz" }, checksums] };
  assert.equal(requireChecksumAsset(release), checksums);
});
