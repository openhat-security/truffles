#!/usr/bin/env node
"use strict";

const fs = require("fs");
const path = require("path");
const https = require("https");
const { execFileSync } = require("child_process");

const pkg = require("./package.json");
const VERSION = pkg.version;
const REPO = process.env.TRUFFLES_REPO || "openhat-security/truffles";
const TAG = process.env.TRUFFLES_TAG || `v${VERSION}`;

function platformTriple() {
  const goos =
    process.platform === "darwin"
      ? "darwin"
      : process.platform === "linux"
        ? "linux"
        : process.platform === "win32"
          ? "windows"
          : null;
  const goarch =
    process.arch === "x64" || process.arch === "x86_64"
      ? "amd64"
      : process.arch === "arm64"
        ? "arm64"
        : null;
  if (!goos || !goarch) {
    throw new Error(
      `unsupported platform ${process.platform}/${process.arch} (need darwin|linux|win32 × x64|arm64)`,
    );
  }
  const ext = goos === "windows" ? ".zip" : "";
  const asset =
    goos === "windows"
      ? `truffles_${VERSION}_windows_amd64.zip`
      : `truffles_${VERSION}_${goos}_${goarch}`;
  return { goos, goarch, asset, isZip: goos === "windows" };
}

function get(url) {
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { "User-Agent": `truffles-npm/${VERSION}` } }, (res) => {
        if (
          res.statusCode >= 300 &&
          res.statusCode < 400 &&
          res.headers.location
        ) {
          get(res.headers.location).then(resolve, reject);
          return;
        }
        if (res.statusCode !== 200) {
          reject(new Error(`GET ${url} → HTTP ${res.statusCode}`));
          res.resume();
          return;
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

async function main() {
  if (process.env.TRUFFLES_SKIP_DOWNLOAD === "1") {
    console.log("truffles: TRUFFLES_SKIP_DOWNLOAD=1 — skipping binary download");
    return;
  }

  const { asset, isZip } = platformTriple();
  const url = `https://github.com/${REPO}/releases/download/${TAG}/${asset}`;
  const vendorDir = path.join(__dirname, "vendor");
  const dest = path.join(vendorDir, process.platform === "win32" ? "truffles.exe" : "truffles");

  fs.mkdirSync(vendorDir, { recursive: true });
  console.log(`truffles: downloading ${url}`);
  const buf = await get(url);
  if (isZip) {
    const zipPath = path.join(vendorDir, asset);
    fs.writeFileSync(zipPath, buf);
    execFileSync("powershell", [
      "-NoProfile",
      "-Command",
      `Expand-Archive -Force '${zipPath.replace(/'/g, "''")}' '${vendorDir.replace(/'/g, "''")}'`,
    ]);
    fs.unlinkSync(zipPath);
    const extracted = path.join(vendorDir, "truffles.exe");
    if (!fs.existsSync(extracted)) {
      throw new Error("truffles.exe not found in release zip");
    }
  } else {
    fs.writeFileSync(dest, buf, { mode: 0o755 });
    if (process.platform !== "win32") {
      fs.chmodSync(dest, 0o755);
    }
  }

  if (process.platform === "darwin") {
    try {
      execFileSync("xattr", ["-d", "com.apple.quarantine", dest], {
        stdio: "ignore",
      });
    } catch {
      /* attribute may be absent */
    }
  }

  try {
    const out = execFileSync(dest, ["version"], { encoding: "utf8" }).trim();
    console.log(`truffles: installed ${out || dest}`);
  } catch {
    console.log(`truffles: installed ${dest}`);
  }
}

main().catch((err) => {
  console.error(`truffles: postinstall failed: ${err.message}`);
  console.error(
    `Hint: install via curl|bash instead:\n  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | bash`,
  );
  process.exit(1);
});
