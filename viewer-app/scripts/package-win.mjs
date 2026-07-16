import { listPackage } from "@electron/asar";
import { packager } from "@electron/packager";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

export const packageIgnorePatterns = [
  /^\/src(?:\/|$)/,
  /^\/tests(?:\/|$)/,
  /^\/scripts(?:\/|$)/,
  /^\/node_modules(?:\/|$)/,
  /^\/tsconfig(?:\.preload)?\.json$/,
  /^\/package-lock\.json$/,
];

export function ignoredPackagePath(value) {
  const normalized = value.replaceAll("\\", "/");
  return packageIgnorePatterns.some((pattern) => pattern.test(normalized));
}

function assertPackageContents(appPath) {
  const archive = path.join(appPath, "resources", "app.asar");
  if (!fs.statSync(archive).isFile()) {
    throw new Error("packaged Viewer app.asar is missing");
  }
  const entries = listPackage(archive, { isPack: false });
  for (const required of ["/build/main.js", "/build/preload.cjs", "/package.json"]) {
    if (!entries.includes(required)) {
      throw new Error(`packaged Viewer is missing ${required}`);
    }
  }
  const leaked = entries.find(ignoredPackagePath);
  if (leaked) {
    throw new Error(`packaged Viewer contains excluded development file ${leaked}`);
  }
}

export async function packageWindows() {
  const appPaths = await packager({
    dir: root,
    name: "CamStationViewer",
    platform: "win32",
    arch: "x64",
    out: path.join(root, "dist"),
    overwrite: true,
    prune: true,
    asar: true,
    ignore: packageIgnorePatterns,
  });
  for (const appPath of appPaths) {
    assertPackageContents(appPath);
  }
}

const invoked = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : "";
if (import.meta.url === invoked) {
  packageWindows().catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
}
