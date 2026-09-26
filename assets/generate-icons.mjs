// Run from any directory after installing the frontend dependencies:
// node assets/generate-icons.mjs
import { readFile, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";

const require = createRequire(new URL("../web/package.json", import.meta.url));
const sharp = require("sharp");
const svg = await readFile(new URL("sparkle-transcoder.svg", import.meta.url));
const sizes = [16, 24, 32, 48, 64, 128, 180, 192, 256, 512];
const pngs = new Map();
for (const size of sizes) {
  pngs.set(size, await sharp(svg, { density: 576 }).resize(size, size).png().toBuffer());
}

// Windows 10/11 support PNG-compressed ICO frames. Include native tray sizes
// so the shell can choose the appropriate image at each display scale.
const iconSizes = [16, 24, 32, 48, 64, 128, 256];
const directory = Buffer.alloc(6 + 16 * iconSizes.length);
directory.writeUInt16LE(1, 2);
directory.writeUInt16LE(iconSizes.length, 4);
let offset = directory.length;
for (const [index, size] of iconSizes.entries()) {
  const entry = 6 + 16 * index;
  const png = pngs.get(size);
  directory[entry] = directory[entry + 1] = size === 256 ? 0 : size;
  directory.writeUInt16LE(1, entry + 4);
  directory.writeUInt16LE(32, entry + 6);
  directory.writeUInt32LE(png.length, entry + 8);
  directory.writeUInt32LE(offset, entry + 12);
  offset += png.length;
}
const ico = Buffer.concat([directory, ...iconSizes.map((size) => pngs.get(size))]);
const outputs = [
  ["sparkle-transcoder.png", pngs.get(512)],
  ["sparkle-transcoder.ico", ico],
  ["../web/public/icon.svg", svg],
  ["../web/public/icon-512.png", pngs.get(512)],
  ["../web/public/icon-192.png", pngs.get(192)],
  ["../web/public/apple-touch-icon.png", pngs.get(180)],
  ["../web/public/favicon.ico", ico],
];
for (const [path, content] of outputs) {
  await writeFile(new URL(path, import.meta.url), content);
}
console.log("Generated Windows and web icons from assets/sparkle-transcoder.svg.");
