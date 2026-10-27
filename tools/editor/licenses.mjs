// Collects the licence of every package in the bundle into
// admin/assets/editor/LICENSES.txt, next to the bundle.
import { readFileSync, writeFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const bundled = [
  "markdown-it", "linkify-it", "mdurl", "uc.micro", "punycode.js", "entities",
  "prosemirror-commands", "prosemirror-dropcursor", "prosemirror-history", "prosemirror-inputrules",
  "prosemirror-keymap", "prosemirror-markdown", "prosemirror-model", "prosemirror-schema-list",
  "prosemirror-state", "prosemirror-transform", "prosemirror-view",
  "orderedmap", "rope-sequence", "w3c-keyname",
];

let out = "Licences of the packages bundled in prosemirror.js (built from tools/editor).\n";

for (const name of bundled) {
  const dir = join("node_modules", name);
  const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
  const file = readdirSync(dir).find(entry => /^licen[cs]e/i.test(entry));

  if (!file) {
    throw new Error(`${name} has no licence file`);
  }

  out += `\n${"=".repeat(72)}\n${name} ${pkg.version} (${pkg.license})\n${"=".repeat(72)}\n\n`;
  out += readFileSync(join(dir, file), "utf8").trim() + "\n";
}

writeFileSync("../../admin/assets/editor/LICENSES.txt", out);
