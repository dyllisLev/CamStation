import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";

const appRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(appRoot, "..");

export function parseInstallerOptions(args) {
  const values = new Map();
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index];
    const value = args[index + 1];
    if (!key?.startsWith("--") || value === undefined || values.has(key)) {
      throw new Error("invalid installer build arguments");
    }
    values.set(key, value);
  }
  const serverUrl = values.get("--server-url") ?? "";
  const version = values.get("--version") ?? "";
  const displayName = values.get("--display-name") ?? "";
  const signerThumbprint = (values.get("--signer-thumbprint") ?? "").toLowerCase();
  if ([...values.keys()].some((key) => !["--server-url", "--version", "--display-name", "--signer-thumbprint"].includes(key))) {
    throw new Error("unknown installer build argument");
  }
  let parsed;
  try {
    parsed = new URL(serverUrl);
  } catch {
    throw new Error("absolute CamStation server URL is required");
  }
  if (!["http:", "https:"].includes(parsed.protocol) || parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash) {
    throw new Error("server URL must be an http(s) origin without credentials or path");
  }
  if (!/^[A-Za-z0-9._-]{1,64}$/.test(version) || displayName.length > 128 || /[\r\n]/.test(displayName)) {
    throw new Error("invalid version or display name");
  }
  if (signerThumbprint && !/^([a-f0-9]{40}|[a-f0-9]{64})$/.test(signerThumbprint)) {
    throw new Error("invalid signer thumbprint");
  }
  return {
    serverUrl: parsed.origin,
    version,
    displayName,
    signerThumbprint,
    allowDevelopmentUnsigned: signerThumbprint === "",
  };
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: options.cwd ?? repoRoot,
    env: options.env ?? process.env,
    stdio: "inherit",
  });
  if (result.error || result.status !== 0) {
    throw result.error ?? new Error(`${command} exited ${result.status}`);
  }
}

function hashFile(file) {
  const hash = crypto.createHash("sha256");
  const descriptor = fs.openSync(file, "r");
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  try {
    for (;;) {
      const read = fs.readSync(descriptor, buffer, 0, buffer.length, null);
      if (read === 0) break;
      hash.update(buffer.subarray(0, read));
    }
  } finally {
    fs.closeSync(descriptor);
  }
  return hash.digest("hex");
}

function listFiles(root) {
  const files = [];
  function visit(directory) {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) visit(full);
      else if (entry.isFile()) files.push(path.relative(root, full).split(path.sep).join("/"));
      else throw new Error(`payload contains non-regular entry ${full}`);
    }
  }
  visit(root);
  return files.sort();
}

function buildManifest(payloadRoot, version) {
  const files = listFiles(payloadRoot).filter((name) => name !== "manifest.json").map((name) => {
    const file = path.join(payloadRoot, ...name.split("/"));
    return { path: name, size: fs.statSync(file).size, sha256: hashFile(file) };
  });
  const digest = crypto.createHash("sha256");
  for (const file of files) digest.update(`${file.path}\0${file.size}\0${file.sha256}\0`);
  return { schemaVersion: 1, version, digest: digest.digest("hex"), files };
}

const crcTable = Array.from({ length: 256 }, (_, value) => {
  let crc = value;
  for (let bit = 0; bit < 8; bit += 1) crc = (crc & 1) ? (0xedb88320 ^ (crc >>> 1)) : (crc >>> 1);
  return crc >>> 0;
});

function crc32(file) {
  let crc = 0xffffffff;
  const descriptor = fs.openSync(file, "r");
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  try {
    for (;;) {
      const read = fs.readSync(descriptor, buffer, 0, buffer.length, null);
      if (read === 0) break;
      for (let index = 0; index < read; index += 1) crc = crcTable[(crc ^ buffer[index]) & 0xff] ^ (crc >>> 8);
    }
  } finally {
    fs.closeSync(descriptor);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function copyIntoDescriptor(source, output) {
  const input = fs.openSync(source, "r");
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  try {
    for (;;) {
      const read = fs.readSync(input, buffer, 0, buffer.length, null);
      if (read === 0) break;
      fs.writeSync(output, buffer, 0, read);
    }
  } finally {
    fs.closeSync(input);
  }
}

function writeStoredZip(root, destination) {
  const entries = listFiles(root).map((name) => {
    const file = path.join(root, ...name.split("/"));
    const size = fs.statSync(file).size;
    if (size > 0xffffffff) throw new Error(`payload file is too large for bounded ZIP: ${name}`);
    return { name, file, size, crc: crc32(file) };
  });
  const output = fs.openSync(destination, "w", 0o600);
  let offset = 0;
  try {
    for (const entry of entries) {
      const name = Buffer.from(entry.name);
      const header = Buffer.alloc(30);
      header.writeUInt32LE(0x04034b50, 0);
      header.writeUInt16LE(20, 4);
      header.writeUInt16LE(0x0800, 6);
      header.writeUInt16LE(0, 8);
      header.writeUInt32LE(entry.crc, 14);
      header.writeUInt32LE(entry.size, 18);
      header.writeUInt32LE(entry.size, 22);
      header.writeUInt16LE(name.length, 26);
      entry.offset = offset;
      fs.writeSync(output, header);
      fs.writeSync(output, name);
      copyIntoDescriptor(entry.file, output);
      offset += header.length + name.length + entry.size;
    }
    const centralOffset = offset;
    for (const entry of entries) {
      const name = Buffer.from(entry.name);
      const header = Buffer.alloc(46);
      header.writeUInt32LE(0x02014b50, 0);
      header.writeUInt16LE(20, 4);
      header.writeUInt16LE(20, 6);
      header.writeUInt16LE(0x0800, 8);
      header.writeUInt32LE(entry.crc, 16);
      header.writeUInt32LE(entry.size, 20);
      header.writeUInt32LE(entry.size, 24);
      header.writeUInt16LE(name.length, 28);
      header.writeUInt32LE(entry.offset, 42);
      fs.writeSync(output, header);
      fs.writeSync(output, name);
      offset += header.length + name.length;
    }
    const end = Buffer.alloc(22);
    end.writeUInt32LE(0x06054b50, 0);
    end.writeUInt16LE(entries.length, 8);
    end.writeUInt16LE(entries.length, 10);
    end.writeUInt32LE(offset - centralOffset, 12);
    end.writeUInt32LE(centralOffset, 16);
    fs.writeSync(output, end);
    fs.fsyncSync(output);
  } finally {
    fs.closeSync(output);
  }
}

export function buildInstaller(options) {
  const dist = path.join(appRoot, "dist");
  const work = path.join(dist, "installer-work");
  const payloadRoot = path.join(work, "payload");
  const embeddedPayload = path.join(repoRoot, "cmd", "camstation-viewer-installer", "payload", "release.zip");
  const output = path.join(dist, "CamStationViewerSetup.exe");
  fs.rmSync(work, { recursive: true, force: true });
  fs.mkdirSync(path.join(payloadRoot, "stable"), { recursive: true });
  fs.mkdirSync(path.join(payloadRoot, "release"), { recursive: true });
  const windowsEnv = { ...process.env, GOOS: "windows", GOARCH: "amd64", CGO_ENABLED: "0" };
  try {
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", path.join(payloadRoot, "stable", "CamStationViewerHost.exe"), "./cmd/camstation-viewer-host"], { env: windowsEnv });
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", path.join(payloadRoot, "stable", "CamStationViewerBootstrap.exe"), "./cmd/camstation-viewer-bootstrap"], { env: windowsEnv });
    run("go", ["build", "-trimpath", "-ldflags", `-s -w -X main.version=${options.version}`, "-o", path.join(payloadRoot, "release", "camstation-viewer-agent.exe"), "./cmd/camstation-viewer-agent"], { env: windowsEnv });
    run("npm", ["run", "build"], { cwd: appRoot });
    run(process.execPath, ["scripts/package-win.mjs"], { cwd: appRoot });
    fs.cpSync(path.join(dist, "CamStationViewer-win32-x64"), path.join(payloadRoot, "release", "viewer"), { recursive: true });
    fs.writeFileSync(path.join(payloadRoot, "defaults.json"), `${JSON.stringify({
      serverUrl: options.serverUrl,
      displayName: options.displayName,
      allowDevelopmentUnsigned: options.allowDevelopmentUnsigned,
      ...(options.signerThumbprint ? { signerThumbprint: options.signerThumbprint } : {}),
    }, null, 2)}\n`, { mode: 0o600 });
    const manifest = buildManifest(payloadRoot, options.version);
    fs.writeFileSync(path.join(payloadRoot, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o600 });
    fs.mkdirSync(path.dirname(embeddedPayload), { recursive: true });
    writeStoredZip(payloadRoot, embeddedPayload);
    run("go", ["test", "./cmd/camstation-viewer-installer", "-run", "EmbeddedBuildPayload", "-count=1"]);
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", output, "./cmd/camstation-viewer-installer"], { env: windowsEnv });
    const header = fs.readFileSync(output).subarray(0, 2).toString("ascii");
    if (header !== "MZ") throw new Error("installer output is not a Windows PE executable");
    return output;
  } finally {
    fs.rmSync(embeddedPayload, { force: true });
    fs.rmSync(work, { recursive: true, force: true });
  }
}

const invoked = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : "";
if (import.meta.url === invoked) {
  try {
    const options = parseInstallerOptions(process.argv.slice(2));
    const output = buildInstaller(options);
    console.log(output);
  } catch (error) {
    console.error(error);
    process.exitCode = 1;
  }
}
