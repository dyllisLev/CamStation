import { mkdir, rename, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const viewerRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const buildRoot = path.join(viewerRoot, "build");
const preloadRoot = path.join(viewerRoot, "build-preload");

switch (process.argv[2]) {
  case "prepare":
    await rm(buildRoot, { recursive: true, force: true });
    await rm(preloadRoot, { recursive: true, force: true });
    break;
  case "finalize":
    await mkdir(buildRoot, { recursive: true });
    await rename(path.join(preloadRoot, "preload.js"), path.join(buildRoot, "preload.cjs"));
    await rm(preloadRoot, { recursive: true, force: true });
    break;
  default:
    throw new Error("usage: node scripts/build-files.mjs <prepare|finalize>");
}
