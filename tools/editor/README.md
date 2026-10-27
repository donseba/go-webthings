# Markdown editor bundle

The admin's Markdown editor (`admin/assets/editor/editor.js`) is built on
[ProseMirror](https://prosemirror.net) and its Markdown module
(`prosemirror-markdown`, with `markdown-it` as parser). They are vendored as
one ES module, `admin/assets/editor/prosemirror.js`, so the admin needs no
build step and no CDN; their licences are in
`admin/assets/editor/LICENSES.txt`.

`src/prosemirror.js` lists what the editor imports. The versions are pinned
in `package.json` and `package-lock.json`. To rebuild the bundle after
changing either, with Node.js installed, run from the repository root:

    task editor

which runs `npm ci`, bundles with esbuild (minified ESM), writes the
licences file and records both files in the admin's lock file,
`admin/assets/vendor/importmap.lock.json` (go-importmap's `importmap vendor
-local`). The bundle is the local package `editor` of
`admin/importmap.json`, so the admin does not start, and `task vendor:check`
fails, when `prosemirror.js` or `LICENSES.txt` differs from the lock.
`editor.js`, `loader.js` and `editor.css` are plain files and need no build.
