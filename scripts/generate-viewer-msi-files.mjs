import { createHash } from "node:crypto";
import { lstat, readdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const namespace = "FA58E97A-3341-5A49-8D67-2132A7E6E99A";
const forbidden = /CamStationViewer(?:Agent|Bootstrap|Host)\.exe|current\.json|release\.zip|schtasks\.exe|CamStationViewerRecovery|--agent-(?:generation|nonce|session)/iu;

export function componentIdentity(relativePath, releaseVersion) {
  const normalized = normalizeRelativePath(relativePath);
  const release = normalizeReleaseVersion(releaseVersion);
  const digest = createHash("sha256").update(normalized).digest("hex").toUpperCase();
  return {
    componentId: `Cmp_${digest.slice(0, 24)}`,
    fileId: `File_${digest.slice(0, 24)}`,
    guid: `{${uuidV5(namespace, `${normalized}@${release}`)}}`,
  };
}

export async function generateViewerMsiFragment(sourceRoot, releaseVersion) {
  const files = (await collectFiles(sourceRoot))
    .filter((relativePath) => relativePath.toLowerCase() !== "camstationviewer.exe")
    .map((relativePath) => {
      const identity = componentIdentity(relativePath, releaseVersion);
      const source = `$(var.ViewerPayloadDir)\\${relativePath.replaceAll("/", "\\")}`;
      return { directory: path.posix.dirname(relativePath), identity, source };
    });
  const directories = [...new Set(files.map(({ directory }) => directory).filter((directory) => directory !== "."))];
  const componentsByDirectory = new Map();
  for (const component of files) {
    const group = componentsByDirectory.get(component.directory) ?? [];
    group.push(component);
    componentsByDirectory.set(component.directory, group);
  }
  const refs = files.map(({ identity }) => identity.componentId);
  return [
    "<?xml version=\"1.0\" encoding=\"utf-8\"?>",
    "<Wix xmlns=\"http://wixtoolset.org/schemas/v4/wxs\">",
    "  <Fragment>",
    "    <DirectoryRef Id=\"INSTALLFOLDER\">",
    ...renderDirectoryTree(directories, "      "),
    "    </DirectoryRef>",
    "  </Fragment>",
    "  <Fragment>",
    ...[...componentsByDirectory.entries()].flatMap(([directory, components]) => [
      `    <DirectoryRef Id="${directory === "." ? "INSTALLFOLDER" : directoryIdentity(directory)}">`,
      ...components.map(renderComponent),
      "    </DirectoryRef>",
    ]),
    "  </Fragment>",
    "  <Fragment>",
    "    <ComponentGroup Id=\"GeneratedViewerPayload\">",
    ...refs.map((id) => `      <ComponentRef Id="${id}" />`),
    "    </ComponentGroup>",
    "  </Fragment>",
    "</Wix>",
    "",
  ].join("\n");
}

function renderDirectoryTree(directories, indentation) {
  const root = { children: new Map() };
  for (const directory of directories) {
    let node = root;
    let accumulated = "";
    for (const segment of directory.split("/")) {
      accumulated = accumulated ? `${accumulated}/${segment}` : segment;
      if (!node.children.has(segment)) node.children.set(segment, { path: accumulated, children: new Map() });
      node = node.children.get(segment);
    }
  }
  const render = (node, depth) => [...node.children.entries()]
    .sort(([left], [right]) => left.localeCompare(right, "en", { sensitivity: "accent" }) || left.localeCompare(right))
    .flatMap(([name, child]) => {
      const indent = `${indentation}${"  ".repeat(depth)}`;
      return [`${indent}<Directory Id="${directoryIdentity(child.path)}" Name="${xml(name)}">`, ...render(child, depth + 1), `${indent}</Directory>`];
    });
  return render(root, 0);
}

function renderComponent({ identity, source }) {
  return `      <Component Id="${identity.componentId}" Guid="${identity.guid}">\n        <File Id="${identity.fileId}" Source="${xml(source)}" KeyPath="yes" />\n      </Component>`;
}

function directoryIdentity(relativePath) {
  const normalized = normalizeRelativePath(relativePath);
  return `Dir_${createHash("sha256").update(`directory:${normalized}`).digest("hex").slice(0, 24).toUpperCase()}`;
}

async function collectFiles(root) {
  const seen = new Set();
  const files = [];
  async function visit(directory, prefix = "") {
    const entries = await readdir(directory, { withFileTypes: true });
    for (const entry of entries) {
      const relativePath = normalizeRelativePath(prefix ? `${prefix}/${entry.name}` : entry.name);
      const fullPath = path.join(directory, entry.name);
      const stat = await lstat(fullPath);
      if (stat.isSymbolicLink()) throw new Error(`symlink is not allowed: ${relativePath}`);
      if (stat.isDirectory()) await visit(fullPath, relativePath);
      else if (stat.isFile()) {
        if (forbidden.test(relativePath)) throw new Error(`rejected runtime artifact: ${relativePath}`);
        const key = relativePath.toLowerCase();
        if (seen.has(key)) throw new Error(`duplicate case-insensitive path: ${relativePath}`);
        seen.add(key);
        files.push(relativePath);
      } else throw new Error(`unsupported payload entry: ${relativePath}`);
    }
  }
  await visit(path.resolve(root));
  return files.sort((left, right) => left.localeCompare(right, "en", { sensitivity: "accent" }) || left.localeCompare(right));
}

function normalizeRelativePath(value) {
  if (typeof value !== "string" || value.length === 0 || /[\x00-\x1F\x7F]/u.test(value)) throw new Error("invalid relative path");
  const normalized = value.replaceAll("\\", "/");
  if (normalized.startsWith("/") || /^[A-Za-z]:/u.test(normalized) || normalized.split("/").some((part) => part === "" || part === "." || part === "..")) {
    throw new Error("invalid relative path");
  }
  return normalized.toLowerCase();
}

function normalizeReleaseVersion(value) {
  if (typeof value !== "string" || !/^\d+\.\d+\.\d+(?:\.\d+)?$/u.test(value)) throw new Error("invalid MSI release version");
  return value;
}

function uuidV5(namespaceValue, name) {
  const namespaceBytes = Buffer.from(namespaceValue.replaceAll("-", ""), "hex");
  const digest = createHash("sha1").update(namespaceBytes).update(name, "utf8").digest();
  digest[6] = (digest[6] & 0x0f) | 0x50;
  digest[8] = (digest[8] & 0x3f) | 0x80;
  const value = digest.subarray(0, 16).toString("hex").toUpperCase();
  return `${value.slice(0, 8)}-${value.slice(8, 12)}-${value.slice(12, 16)}-${value.slice(16, 20)}-${value.slice(20)}`;
}

function xml(value) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll("\"", "&quot;");
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  const [sourceRoot, outputPath, releaseVersion] = process.argv.slice(2);
  if (!sourceRoot || !outputPath || !releaseVersion) throw new Error("usage: generate-viewer-msi-files.mjs <payload-directory> <output.wxs> <msi-version>");
  await writeFile(outputPath, await generateViewerMsiFragment(sourceRoot, releaseVersion), "utf8");
}
