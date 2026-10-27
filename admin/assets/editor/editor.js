// The admin's Markdown editor: editors see formatted text, the field stores
// Markdown. It is built on ProseMirror and its Markdown parser and serializer
// (prosemirror.js, built from tools/editor). The "Markdown" button shows the
// source for those who prefer to write it.
//
// The original textarea stays the form field. The editor only writes to it
// when the text is changed, so a field that is not touched is submitted
// byte for byte as it was loaded.
import {
    EditorState,
    EditorView,
    InputRule,
    MarkdownIt,
    MarkdownParser,
    MarkdownSerializer,
    Schema,
    TextSelection,
    baseKeymap,
    chainCommands,
    defaultMarkdownSerializer,
    dropCursor,
    exitCode,
    history,
    inputRules,
    keymap,
    lift,
    liftListItem,
    markdownSchema,
    redo,
    redoDepth,
    setBlockType,
    sinkListItem,
    splitListItem,
    textblockTypeInputRule,
    toggleMark,
    undo,
    undoDepth,
    undoInputRule,
    wrapIn,
    wrapInList,
    wrappingInputRule,
} from "./prosemirror.js";

// The editor's stylesheet is only needed with the editor; the first editor
// waits for it, so the toolbar never shows unstyled.
// import.meta.resolve applies the import map, which versions the address.
const stylesheet = import.meta.resolve("./editor.css");

if (![...document.styleSheets].some(sheet => sheet.href === stylesheet)) {
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = stylesheet;

    await new Promise(resolve => {
        link.addEventListener("load", resolve, {once: true});
        link.addEventListener("error", resolve, {once: true});
        document.head.append(link);
    });
}

// Labels are translated by the admin into its language; see
// admin/markdowneditor.go.
let labels;

function t(message) {
    if (!labels) {
        try {
            labels = JSON.parse(document.getElementById("markdown-editor-labels")?.textContent || "{}");
        } catch {
            labels = {};
        }
    }

    return labels[message] || message;
}

// ---------------------------------------------------------------------------
// Schema: CommonMark as prosemirror-markdown defines it, plus strikethrough
// (GitHub Flavored Markdown, which the site renders). Pasted HTML is read
// through the same rules, so only this structure survives a paste.

const mediaPrefix = "media:";

function safeHref(href) {
    const value = (href || "").trim();

    if (/^(https?:|mailto:|tel:|media:|\/|#|\?|\.)/i.test(value)) {
        return value;
    }

    // A relative address such as "contact" has no scheme.
    return /^[^:]*$/.test(value) ? value : null;
}

const headingLevels = {h1: 2, h2: 2, h3: 3, h4: 4, h5: 4, h6: 4};

// A pasted list is written without blank lines between its items, unless
// an item holds more than one paragraph.
function pastedListIsTight(list) {
    return ![...list.children].some(item => item.querySelectorAll(":scope > p").length > 1);
}

const nodes = markdownSchema.spec.nodes
    .update("heading", {
        ...markdownSchema.spec.nodes.get("heading"),
        // Markdown keeps every level; pasted headings become H2 to H4, the
        // levels the toolbar offers.
        parseDOM: Object.entries(headingLevels).map(([tag, level]) => ({tag, attrs: {level}})),
    })
    .update("bullet_list", {
        ...markdownSchema.spec.nodes.get("bullet_list"),
        attrs: {tight: {default: true}, bullet: {default: "-"}},
        parseDOM: [{tag: "ul", getAttrs: dom => ({tight: pastedListIsTight(dom)})}],
    })
    .update("ordered_list", {
        ...markdownSchema.spec.nodes.get("ordered_list"),
        attrs: {order: {default: 1}, tight: {default: true}},
        parseDOM: [{
            tag: "ol",
            getAttrs: dom => ({order: Number(dom.getAttribute("start")) || 1, tight: pastedListIsTight(dom)}),
        }],
    })
    .update("image", {
        ...markdownSchema.spec.nodes.get("image"),
        // Only images copied from an editor keep their Markdown address;
        // images pasted from elsewhere are dropped.
        parseDOM: [{
            tag: "img[data-markdown-src]",
            getAttrs: dom => ({
                src: dom.getAttribute("data-markdown-src"),
                alt: dom.getAttribute("alt"),
                title: dom.getAttribute("title"),
            }),
        }],
        toDOM: node => ["img", {
            src: displayAddress(node.attrs.src),
            alt: node.attrs.alt,
            title: node.attrs.title,
            "data-markdown-src": node.attrs.src,
        }],
    });

const marks = markdownSchema.spec.marks
    .update("link", {
        ...markdownSchema.spec.marks.get("link"),
        parseDOM: [{
            tag: "a[href]",
            getAttrs: dom => {
                const href = safeHref(dom.getAttribute("data-markdown-href") || dom.getAttribute("href"));

                return href === null ? false : {href, title: dom.getAttribute("title")};
            },
        }],
        toDOM: mark => ["a", {
            href: displayAddress(mark.attrs.href),
            title: mark.attrs.title,
            "data-markdown-href": mark.attrs.href,
            rel: "noopener noreferrer",
        }, 0],
    })
    .addToEnd("strike", {
        // Pasted HTML has its line-through as data-strike (see withoutStyles).
        parseDOM: [{tag: "s"}, {tag: "del"}, {tag: "strike"}, {tag: "[data-strike]"}, {style: "text-decoration=line-through"}],
        toDOM: () => ["s", 0],
    });

const schema = new Schema({nodes, marks});

// withoutStyles takes the style attributes out of pasted HTML before the
// editor reads it into the page, where a Content-Security-Policy refuses
// them, and reports each one; the schema keeps no styles anyway. A style
// that strikes the text through becomes data-strike, which the strike
// mark reads.
function withoutStyles(html) {
    return html.replace(/\sstyle\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi,
        (attribute, value) => /line-through/i.test(value) ? " data-strike" : "");
}

// ---------------------------------------------------------------------------
// Markdown in and out.

const tokenizer = MarkdownIt("commonmark", {html: false}).enable(["strikethrough", "table"]);

function listIsTight(tokens, index) {
    while (++index < tokens.length) {
        if (tokens[index].type !== "list_item_open") {
            return tokens[index].hidden;
        }
    }

    return false;
}

// Tables are parsed (and not handled) so that a text with one is refused
// rather than flattened; see parse.
const parser = new MarkdownParser(schema, tokenizer, {
    blockquote: {block: "blockquote"},
    paragraph: {block: "paragraph"},
    list_item: {block: "list_item"},
    bullet_list: {
        block: "bullet_list",
        getAttrs: (token, tokens, index) => ({tight: listIsTight(tokens, index), bullet: token.markup || "-"}),
    },
    ordered_list: {
        block: "ordered_list",
        getAttrs: (token, tokens, index) => ({
            order: Number(token.attrGet("start")) || 1,
            tight: listIsTight(tokens, index),
        }),
    },
    heading: {block: "heading", getAttrs: token => ({level: Number(token.tag.slice(1))})},
    code_block: {block: "code_block", noCloseToken: true},
    fence: {block: "code_block", getAttrs: token => ({params: token.info || ""}), noCloseToken: true},
    hr: {node: "horizontal_rule"},
    image: {
        node: "image",
        getAttrs: token => ({
            src: token.attrGet("src"),
            title: token.attrGet("title") || null,
            alt: token.children?.[0]?.content || null,
        }),
    },
    hardbreak: {node: "hard_break"},
    em: {mark: "em"},
    strong: {mark: "strong"},
    s: {mark: "strike"},
    link: {
        mark: "link",
        getAttrs: token => ({href: token.attrGet("href"), title: token.attrGet("title") || null}),
    },
    code_inline: {mark: "code", noCloseToken: true},
});

const serializer = new MarkdownSerializer({
    ...defaultMarkdownSerializer.nodes,
    bullet_list(state, node) {
        state.renderList(node, "  ", () => (node.attrs.bullet || "-") + " ");
    },
}, {
    ...defaultMarkdownSerializer.marks,
    strike: {open: "~~", close: "~~", mixable: true, expelEnclosingWhitespace: true},
});

// Task list items ("- [ ] item") are GitHub Markdown the editor has no
// element for; like tables they are edited as Markdown.
const taskListItem = /^\s*(?:[-+*]|\d+[.)])\s+\[[ xX]\]\s/m;

// parse returns the document for the Markdown, or null when the text uses
// Markdown the editor cannot show without changing it.
function parse(markdown) {
    if (taskListItem.test(markdown)) {
        return null;
    }

    try {
        return parser.parse(markdown);
    } catch {
        return null;
    }
}

function serialize(doc) {
    return serializer.serialize(doc);
}

// ---------------------------------------------------------------------------
// Media: Markdown refers to media as media:<id>; the editor shows the image
// through its URL, which the media library's picker endpoint resolves.

// The admin names the picker's address on the loader's script tag; it is
// empty without a media library.
const pickerURL = document.querySelector("script[data-media-picker]")?.dataset.mediaPicker || "";
const resolved = new Map();
let pending = new Map();
let pendingTimer;

function mediaID(address) {
    return address?.startsWith(mediaPrefix) ? address.slice(mediaPrefix.length) : "";
}

// displayAddress is what the browser loads for a Markdown address: a media
// reference that is not resolved yet shows nothing until it is.
function displayAddress(address) {
    const id = mediaID(address);

    if (!id) {
        return address;
    }

    const known = resolved.get(id);

    return typeof known === "string" ? known : "";
}

function resolveMedia(id) {
    const known = resolved.get(id);

    if (known instanceof Promise) {
        return known;
    }

    if (typeof known === "string") {
        return Promise.resolve(known);
    }

    const promise = new Promise(resolve => {
        pending.set(id, resolve);
    });

    resolved.set(id, promise);
    clearTimeout(pendingTimer);
    pendingTimer = setTimeout(fetchPending, 10);

    return promise;
}

async function fetchPending() {
    const batch = pending;
    pending = new Map();

    const query = new URLSearchParams();

    for (const id of batch.keys()) {
        query.append("id", id);
    }

    let items = [];

    try {
        // Without a media library images show their alternative text.
        const response = pickerURL ? await fetch(`${pickerURL}?${query}`, {headers: {Accept: "application/json"}}) : null;

        if (response?.ok && response.headers.get("Content-Type")?.includes("json")) {
            items = (await response.json()).items || [];
        }
    } catch {
        // Unresolved images show their alternative text.
    }

    const urls = new Map(items.map(item => [item.id, item.preview || item.url]));

    for (const [id, resolve] of batch) {
        const url = urls.get(id) || "";
        resolved.set(id, url);
        resolve(url);
    }
}

class ImageView {
    constructor(node) {
        this.dom = document.createElement("img");
        this.update(node);
    }

    update(node) {
        if (node.type !== schema.nodes.image) {
            return false;
        }

        this.node = node;
        this.dom.alt = node.attrs.alt || "";
        this.dom.title = node.attrs.title || "";
        this.dom.dataset.markdownSrc = node.attrs.src;

        const id = mediaID(node.attrs.src);

        if (!id) {
            this.dom.src = node.attrs.src;

            return true;
        }

        const known = resolved.get(id);

        if (typeof known === "string") {
            this.show(known);
        } else {
            resolveMedia(id).then(url => {
                if (this.node === node) {
                    this.show(url);
                }
            });
        }

        return true;
    }

    show(url) {
        this.dom.classList.toggle("md-missing", !url);

        if (url) {
            this.dom.src = url;
        } else {
            this.dom.removeAttribute("src");
        }
    }

    ignoreMutation() {
        return true;
    }
}

// ---------------------------------------------------------------------------
// Commands, keys and input rules.

function markActive(state, type) {
    const {from, $from, to, empty} = state.selection;

    if (empty) {
        return Boolean(type.isInSet(state.storedMarks || $from.marks()));
    }

    return state.doc.rangeHasMark(from, to, type);
}

function blockActive(state, type, attrs = {}) {
    const {$from, to} = state.selection;

    if (to > $from.end()) {
        return false;
    }

    const parent = $from.parent;

    return parent.type === type && Object.entries(attrs).every(([key, value]) => parent.attrs[key] === value);
}

// toggleHeading makes the block a heading of level, or a paragraph again
// when it already is one, so a toolbar button and its shortcut behave alike.
function toggleHeading(level) {
    return (state, dispatch) => {
        if (blockActive(state, schema.nodes.heading, {level})) {
            return setBlockType(schema.nodes.paragraph)(state, dispatch);
        }

        return setBlockType(schema.nodes.heading, {level})(state, dispatch);
    };
}

function wrapperActive(state, type) {
    const {$from} = state.selection;

    for (let depth = $from.depth; depth > 0; depth--) {
        if ($from.node(depth).type === type) {
            return true;
        }
    }

    return false;
}

// toggleList wraps the selection in a list of type, or lifts it out of the
// list when it is in one of that type already.
function toggleList(type) {
    return (state, dispatch) => {
        if (wrapperActive(state, type)) {
            return liftListItem(schema.nodes.list_item)(state, dispatch);
        }

        return wrapInList(type)(state, dispatch);
    };
}

function toggleBlockquote(state, dispatch) {
    if (wrapperActive(state, schema.nodes.blockquote)) {
        return lift(state, dispatch);
    }

    return wrapIn(schema.nodes.blockquote)(state, dispatch);
}

function toggleCodeBlock(state, dispatch) {
    if (blockActive(state, schema.nodes.code_block)) {
        return setBlockType(schema.nodes.paragraph)(state, dispatch);
    }

    return setBlockType(schema.nodes.code_block)(state, dispatch);
}

function insertRule(state, dispatch) {
    const rule = schema.nodes.horizontal_rule;
    const {$from} = state.selection;

    for (let depth = $from.depth; depth >= 0; depth--) {
        const index = $from.index(depth);

        if ($from.node(depth).canReplaceWith(index, index, rule)) {
            if (dispatch) {
                dispatch(state.tr.replaceSelectionWith(rule.create()).scrollIntoView());
            }

            return true;
        }
    }

    return false;
}

const hardBreak = chainCommands(exitCode, (state, dispatch) => {
    if (dispatch) {
        dispatch(state.tr.replaceSelectionWith(schema.nodes.hard_break.create()).scrollIntoView());
    }

    return true;
});

// linkRange is the extent of the link around the cursor, if any.
function linkRange(state) {
    const type = schema.marks.link;
    const {$from, from, to, empty} = state.selection;

    if (!empty) {
        let found = null;

        state.doc.nodesBetween(from, to, node => {
            found ||= type.isInSet(node.marks);
        });

        return found ? {from, to, mark: found} : null;
    }

    const mark = type.isInSet($from.marks());

    if (!mark) {
        return null;
    }

    let start = $from.index();
    let end = $from.indexAfter();
    const parent = $from.parent;

    while (start > 0 && mark.isInSet(parent.child(start - 1).marks)) {
        start--;
    }

    while (end < parent.childCount && mark.isInSet(parent.child(end).marks)) {
        end++;
    }

    let position = $from.start();
    let rangeFrom = position;
    let rangeTo = position;

    for (let index = 0; index < end; index++) {
        if (index === start) {
            rangeFrom = position;
        }

        position += parent.child(index).nodeSize;
    }

    rangeTo = position;

    return {from: rangeFrom, to: rangeTo, mark};
}

// editLink asks for the address of the link at the selection: an empty
// answer removes the link, and without selected text the address itself
// becomes the link's text.
function editLink(state, dispatch, view) {
    if (!dispatch) {
        return !blockActive(state, schema.nodes.code_block);
    }

    const type = schema.marks.link;
    const range = linkRange(state);
    const answer = window.prompt(t("Link address (leave empty to remove the link)"), range?.mark.attrs.href || "https://");

    view?.focus();

    if (answer === null) {
        return true;
    }

    const href = answer.trim();
    const {from, to, empty} = state.selection;
    const target = range || {from, to};

    if (!href || href === "https://") {
        if (range) {
            dispatch(state.tr.removeMark(range.from, range.to, type));
        }

        return true;
    }

    if (safeHref(href) === null) {
        window.alert(t("This link address cannot be used."));

        return true;
    }

    const mark = type.create({href});

    if (empty && !range) {
        dispatch(state.tr.replaceSelectionWith(schema.text(href, [mark]), false).scrollIntoView());

        return true;
    }

    dispatch(state.tr.removeMark(target.from, target.to, type).addMark(target.from, target.to, mark).scrollIntoView());

    return true;
}

function headingRule() {
    return textblockTypeInputRule(/^(#{1,4})\s$/, schema.nodes.heading, match => ({level: Math.max(2, match[1].length)}));
}

function editorPlugins() {
    const {nodes: types, marks: markTypes} = schema;

    return [
        inputRules({
            rules: [
                wrappingInputRule(/^\s*>\s$/, types.blockquote),
                wrappingInputRule(/^(\d+)\.\s$/, types.ordered_list, match => ({order: Number(match[1])}),
                    (match, node) => node.childCount + node.attrs.order === Number(match[1])),
                wrappingInputRule(/^\s*([-+*])\s$/, types.bullet_list, match => ({bullet: match[1]})),
                textblockTypeInputRule(/^```$/, types.code_block),
                headingRule(),
                new InputRule(/^(?:---|\*\*\*|___)$/, (state, match, start, end) => {
                    const $start = state.doc.resolve(start);

                    if (!$start.node(-1).canReplaceWith($start.index(-1), $start.indexAfter(-1), types.horizontal_rule)) {
                        return null;
                    }

                    return state.tr.delete(start, end).replaceSelectionWith(types.horizontal_rule.create());
                }),
            ],
        }),
        keymap({
            "Mod-z": undo,
            "Shift-Mod-z": redo,
            "Mod-y": redo,
            "Backspace": undoInputRule,
            "Mod-b": toggleMark(markTypes.strong),
            "Mod-B": toggleMark(markTypes.strong),
            "Mod-i": toggleMark(markTypes.em),
            "Mod-I": toggleMark(markTypes.em),
            "Mod-`": toggleMark(markTypes.code),
            "Mod-k": editLink,
            "Mod-K": editLink,
            "Shift-Enter": hardBreak,
            "Mod-Enter": hardBreak,
            "Enter": splitListItem(types.list_item),
            "Mod-[": liftListItem(types.list_item),
            "Mod-]": sinkListItem(types.list_item),
            "Shift-Ctrl-0": setBlockType(types.paragraph),
            "Shift-Ctrl-2": toggleHeading(2),
            "Shift-Ctrl-3": toggleHeading(3),
            "Shift-Ctrl-4": toggleHeading(4),
            "Shift-Ctrl-7": toggleList(types.ordered_list),
            "Shift-Ctrl-8": toggleList(types.bullet_list),
        }),
        keymap(baseKeymap),
        dropCursor(),
        history(),
    ];
}

// ---------------------------------------------------------------------------
// Toolbar.

// The icons are next to this module, wherever the admin serves its assets,
// at the versioned addresses the import map gives (see the stylesheet).
function iconsAt(specifier) {
    const address = new URL(import.meta.resolve(specifier));

    return address.pathname + address.search;
}

const adminIcons = iconsAt("../icons.svg");
const editorIcons = iconsAt("./icons.svg");

const tools = [
    {
        name: "paragraph", label: "Paragraph", icon: `${adminIcons}#paragraph`, keys: "Ctrl+Shift+0",
        run: setBlockType(schema.nodes.paragraph),
        active: state => blockActive(state, schema.nodes.paragraph),
    },
    ...[2, 3, 4].map(level => ({
        name: `heading${level}`, label: `Heading ${level}`, text: `H${level}`, keys: `Ctrl+Shift+${level}`,
        run: toggleHeading(level),
        active: state => blockActive(state, schema.nodes.heading, {level}),
    })),
    {
        name: "bold", label: "Bold", icon: `${adminIcons}#bold`, keys: "Mod+B", group: true,
        run: toggleMark(schema.marks.strong),
        active: state => markActive(state, schema.marks.strong),
    },
    {
        name: "italic", label: "Italic", icon: `${adminIcons}#italic`, keys: "Mod+I",
        run: toggleMark(schema.marks.em),
        active: state => markActive(state, schema.marks.em),
    },
    {
        name: "code", label: "Code", icon: `${editorIcons}#code`, keys: "Mod+`",
        run: toggleMark(schema.marks.code),
        active: state => markActive(state, schema.marks.code),
    },
    {
        name: "link", label: "Link", icon: `${adminIcons}#link`, keys: "Mod+K",
        run: editLink,
        active: state => markActive(state, schema.marks.link),
    },
    {
        name: "bulletList", label: "Bulleted list", icon: `${adminIcons}#list`, keys: "Ctrl+Shift+8", group: true,
        run: toggleList(schema.nodes.bullet_list),
        active: state => wrapperActive(state, schema.nodes.bullet_list),
    },
    {
        name: "orderedList", label: "Numbered list", icon: `${adminIcons}#numbers`, keys: "Ctrl+Shift+7",
        run: toggleList(schema.nodes.ordered_list),
        active: state => wrapperActive(state, schema.nodes.ordered_list),
    },
    {
        name: "quote", label: "Quote", icon: `${adminIcons}#quote`,
        run: toggleBlockquote,
        active: state => wrapperActive(state, schema.nodes.blockquote),
    },
    {
        name: "codeBlock", label: "Code block", icon: `${editorIcons}#code-block`,
        run: toggleCodeBlock,
        active: state => blockActive(state, schema.nodes.code_block),
    },
    {
        name: "rule", label: "Horizontal line", icon: `${editorIcons}#rule`,
        run: insertRule,
    },
    {
        name: "image", label: "Image from the media library", icon: `${adminIcons}#image`, group: true, media: true,
        run: (state, dispatch, view) => {
            if (dispatch && view) {
                openPicker(view);
            }

            return !blockActive(state, schema.nodes.code_block);
        },
    },
    {
        name: "undo", label: "Undo", icon: `${adminIcons}#undo`, keys: "Mod+Z", group: true,
        run: undo,
        enabled: state => undoDepth(state) > 0,
    },
    {
        name: "redo", label: "Redo", icon: `${adminIcons}#redo`, keys: "Mod+Shift+Z",
        run: redo,
        enabled: state => redoDepth(state) > 0,
    },
];

const mac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

function shortcut(keys) {
    return keys.replace("Mod", mac ? "⌘" : "Ctrl").replace("Ctrl", mac ? "⌃" : "Ctrl").replace("Shift", mac ? "⇧" : "Shift");
}

function iconElement(href) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("class", "icon");
    svg.setAttribute("aria-hidden", "true");

    const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
    use.setAttribute("href", href);
    svg.append(use);

    return svg;
}

function toolButton(tool) {
    const button = document.createElement("button");
    button.type = "button";
    button.dataset.tool = tool.name;
    button.tabIndex = -1;

    const label = t(tool.label);
    button.setAttribute("aria-label", label);
    button.title = tool.keys ? `${label} (${shortcut(tool.keys)})` : label;

    if (tool.keys) {
        button.setAttribute("aria-keyshortcuts", tool.keys.replace("Mod", mac ? "Meta" : "Control").replace("Ctrl", "Control"));
    }

    if (tool.active) {
        button.setAttribute("aria-pressed", "false");
    }

    if (tool.group) {
        button.classList.add("md-group-start");
    }

    if (tool.icon) {
        button.append(iconElement(tool.icon));
    } else {
        button.textContent = tool.text;
    }

    return button;
}

// ---------------------------------------------------------------------------
// Editors.

const editors = new Map();
const sourceKey = "webthings-markdown-source";
let mediaAccess;

function prefersSource() {
    try {
        return localStorage.getItem(sourceKey) === "1";
    } catch {
        return false;
    }
}

function rememberSource(source) {
    try {
        if (source) {
            localStorage.setItem(sourceKey, "1");
        } else {
            localStorage.removeItem(sourceKey);
        }
    } catch {
        // Without storage the choice lasts for this page.
    }
}

// canUseMedia asks once whether this editor may use the media library.
function canUseMedia() {
    if (!pickerURL) return Promise.resolve(false);

    mediaAccess ??= fetch(`${pickerURL}?page=1`, {method: "HEAD", headers: {Accept: "application/json"}})
        .then(response => response.ok && Boolean(response.headers.get("Content-Type")?.includes("json")))
        .catch(() => false);

    return mediaAccess;
}

class MarkdownEditor {
    constructor(textarea) {
        this.textarea = textarea;
        this.source = false;

        this.root = document.createElement("div");
        this.root.className = "md-editor";

        this.toolbar = document.createElement("div");
        this.toolbar.className = "md-toolbar";
        this.toolbar.setAttribute("role", "toolbar");
        this.toolbar.setAttribute("aria-label", t("Text formatting"));

        if (textarea.id) {
            this.toolbar.setAttribute("aria-controls", textarea.id);
        }

        this.buttons = tools.map(tool => {
            const button = toolButton(tool);
            this.toolbar.append(button);

            return {tool, button};
        });

        this.toggle = document.createElement("button");
        this.toggle.type = "button";
        this.toggle.className = "md-source-toggle";
        this.toggle.tabIndex = -1;
        this.toggle.textContent = "Markdown";
        this.toggle.title = t("Show the text as Markdown");
        this.toggle.setAttribute("aria-pressed", "false");
        this.toolbar.append(this.toggle);

        this.surface = document.createElement("div");
        this.surface.className = "md-surface";

        this.message = document.createElement("p");
        this.message.className = "md-message";
        this.message.setAttribute("role", "status");
        this.message.hidden = true;

        textarea.before(this.root);
        this.root.append(this.toolbar, this.surface, this.message, textarea);

        const doc = parse(textarea.value);

        this.view = new EditorView(this.surface, {
            state: this.createState(doc || schema.topNodeType.createAndFill()),
            nodeViews: {image: node => new ImageView(node)},
            attributes: this.contentAttributes(),
            transformPastedHTML: withoutStyles,
            dispatchTransaction: transaction => this.dispatch(transaction),
        });

        this.onToolbarClick = event => this.toolbarClick(event);
        this.onToolbarKey = event => this.toolbarKey(event);
        this.onInvalid = event => this.invalid(event);
        this.onLabelClick = event => {
            if (!this.source) {
                event.preventDefault();
                this.view.focus();
            }
        };

        this.toolbar.addEventListener("mousedown", event => {
            // Keep the selection in the text while a button is pressed.
            if (event.target.closest("button")) {
                event.preventDefault();
            }
        });
        this.toolbar.addEventListener("click", this.onToolbarClick);
        this.toolbar.addEventListener("keydown", this.onToolbarKey);
        textarea.addEventListener("invalid", this.onInvalid);
        this.label()?.addEventListener("click", this.onLabelClick);

        if (!doc) {
            this.setSource(true, t("This text contains Markdown the editor cannot show, such as a table. Edit it as Markdown."));
        } else if (prefersSource()) {
            this.setSource(true);
        } else {
            this.setSource(false);
        }

        this.buttons[0].button.tabIndex = 0;
        this.refresh();

        canUseMedia().then(allowed => {
            for (const {tool, button} of this.buttons) {
                if (tool.media) {
                    button.hidden = !allowed;
                }
            }
        });
    }

    label() {
        return this.textarea.id ? document.getElementById(`${this.textarea.id}_label`) : null;
    }

    contentAttributes() {
        const attributes = {
            "class": "md-content",
            "role": "textbox",
            "aria-multiline": "true",
            "spellcheck": "true",
        };

        const labelledBy = this.textarea.getAttribute("aria-labelledby");
        const describedBy = this.textarea.getAttribute("aria-describedby");

        if (labelledBy) {
            attributes["aria-labelledby"] = labelledBy;
        }

        if (describedBy) {
            attributes["aria-describedby"] = describedBy;
        }

        if (this.textarea.required) {
            attributes["aria-required"] = "true";
        }

        if (this.textarea.lang) {
            attributes.lang = this.textarea.lang;
        }

        return attributes;
    }

    createState(doc) {
        return EditorState.create({doc, plugins: editorPlugins()});
    }

    dispatch(transaction) {
        const state = this.view.state.apply(transaction);
        this.view.updateState(state);

        if (transaction.docChanged) {
            // Only real changes are written, so an untouched field keeps its
            // Markdown exactly.
            this.textarea.value = serialize(state.doc);
            this.textarea.dispatchEvent(new Event("input", {bubbles: true}));
            this.message.hidden = true;
        }

        this.refresh();
    }

    refresh() {
        const state = this.view.state;

        for (const {tool, button} of this.buttons) {
            const active = !this.source && Boolean(tool.active?.(state));
            const possible = tool.enabled ? tool.enabled(state) : tool.run(state, null, this.view);

            // A pressed button stays usable: pressing it again undoes it.
            button.disabled = this.source || !(active || possible);

            if (tool.active) {
                button.setAttribute("aria-pressed", String(active));
            }
        }
    }

    focusables() {
        return [...this.toolbar.querySelectorAll("button")].filter(button => !button.hidden && !button.disabled);
    }

    toolbarKey(event) {
        const buttons = this.focusables();
        const index = buttons.indexOf(document.activeElement);

        if (index < 0) {
            return;
        }

        let next;

        switch (event.key) {
        case "ArrowRight":
            next = buttons[(index + 1) % buttons.length];
            break;
        case "ArrowLeft":
            next = buttons[(index - 1 + buttons.length) % buttons.length];
            break;
        case "Home":
            next = buttons[0];
            break;
        case "End":
            next = buttons[buttons.length - 1];
            break;
        default:
            return;
        }

        event.preventDefault();

        for (const button of this.toolbar.querySelectorAll("button")) {
            button.tabIndex = button === next ? 0 : -1;
        }

        next.focus();
    }

    toolbarClick(event) {
        const button = event.target.closest("button");

        if (!button || button.disabled) {
            return;
        }

        for (const other of this.toolbar.querySelectorAll("button")) {
            other.tabIndex = other === button ? 0 : -1;
        }

        if (button === this.toggle) {
            this.setSource(!this.source);
            rememberSource(this.source);

            return;
        }

        const entry = this.buttons.find(({button: candidate}) => candidate === button);

        if (!entry) {
            return;
        }

        if (entry.tool.name !== "image" && entry.tool.name !== "link") {
            this.view.focus();
        }

        entry.tool.run(this.view.state, transaction => this.view.dispatch(transaction), this.view);

        if (entry.tool.name !== "image" && entry.tool.name !== "link") {
            this.view.focus();
        }
    }

    // setSource switches between formatted text and Markdown. Coming back
    // from Markdown, the text is read again; Markdown the editor cannot show
    // keeps the source open.
    setSource(source, notice = "") {
        if (!source && this.source) {
            const doc = parse(this.textarea.value);

            if (!doc) {
                this.showMessage(t("This text contains Markdown the editor cannot show, such as a table. Edit it as Markdown."));

                return;
            }

            this.view.updateState(this.createState(doc));
        }

        this.source = source;
        // The mode shows either the text or the textarea through the
        // stylesheet: htmx restores the attributes of a swapped-in textarea
        // once it settles, so the textarea's own hidden would not last.
        this.root.dataset.mode = source ? "markdown" : "text";
        this.toggle.setAttribute("aria-pressed", String(source));

        if (notice) {
            this.showMessage(notice);
        } else {
            this.message.hidden = true;
        }

        this.refresh();
    }

    showMessage(text) {
        this.message.textContent = text;
        this.message.hidden = false;
    }

    // invalid shows the browser's validation message (such as a required
    // field left empty) next to the editor, because the hidden textarea
    // cannot show it.
    invalid(event) {
        if (this.source) {
            return;
        }

        event.preventDefault();
        this.showMessage(this.textarea.validationMessage);
        this.root.classList.add("md-invalid");
        this.view.focus();

        const clear = () => {
            this.root.classList.remove("md-invalid");
            this.textarea.removeEventListener("input", clear);
        };

        this.textarea.addEventListener("input", clear);
    }

    insertImage(item, alt) {
        resolved.set(item.id, item.preview || item.url);

        const {state} = this.view;
        const {$from} = state.selection;
        const image = schema.nodes.image.create({src: mediaPrefix + item.id, alt: alt || null});
        let transaction;

        if ($from.parent.type === schema.nodes.paragraph && $from.parent.content.size === 0) {
            transaction = state.tr.replaceSelectionWith(image);
        } else if ($from.depth === 0) {
            // A selected block, such as a horizontal line.
            transaction = state.tr.replaceSelectionWith(schema.nodes.paragraph.create(null, image));
        } else {
            // An image gets a paragraph of its own after the text the cursor
            // is in, rather than landing in the middle of a sentence or a
            // heading.
            const after = $from.after();
            transaction = state.tr.insert(after, schema.nodes.paragraph.create(null, image));
            transaction.setSelection(TextSelection.create(transaction.doc, after + 2));
        }

        this.view.dispatch(transaction.scrollIntoView());
        this.view.focus();
    }

    destroy() {
        this.view.destroy();
        this.textarea.removeEventListener("invalid", this.onInvalid);
        this.label()?.removeEventListener("click", this.onLabelClick);
        editors.delete(this.textarea);
    }
}

// ---------------------------------------------------------------------------
// Media picker: a dialog listing the images of the media library.

let picker;

function element(tag, attributes = {}, ...children) {
    const node = document.createElement(tag);

    for (const [key, value] of Object.entries(attributes)) {
        if (key === "text") {
            node.textContent = value;
        } else {
            node.setAttribute(key, value);
        }
    }

    node.append(...children);

    return node;
}

function createPicker() {
    const titleID = "md-picker-title";
    const dialog = element("dialog", {"class": "md-picker", "aria-labelledby": titleID});
    const title = element("h2", {id: titleID, text: t("Choose an image")});
    const close = element("button", {"type": "button", "class": "md-picker-close", "aria-label": t("Close")},
        iconElement(`${adminIcons}#close`));
    const search = element("input", {"type": "search", "class": "control", "aria-label": t("Search images"),
        "placeholder": t("Search images")});
    const grid = element("div", {"class": "md-picker-grid", "role": "group", "aria-label": t("Images")});
    const status = element("p", {"class": "md-picker-status muted", "role": "status"});
    const more = element("button", {"type": "button", "class": "button-secondary", "text": t("Show more")});
    const altID = "md-picker-alt";
    const alt = element("input", {"type": "text", "class": "control", "id": altID, "maxlength": "300"});
    const altHelp = element("p", {"class": "muted md-picker-help",
        "text": t("Optional. When empty, the image's alternative text from the media library is used.")});
    const insert = element("button", {"type": "button", "class": "button", "text": t("Insert")});
    const cancel = element("button", {"type": "button", "class": "button-secondary", "text": t("Cancel")});

    more.hidden = true;
    insert.disabled = true;

    dialog.append(
        element("div", {"class": "md-picker-header"}, title, close),
        search,
        grid,
        status,
        more,
        element("label", {"class": "field-label", "for": altID, "text": t("Alternative text")}),
        alt,
        altHelp,
        element("div", {"class": "md-picker-actions"}, insert, cancel),
    );

    document.body.append(dialog);

    const state = {dialog, grid, search, status, more, alt, insert, page: 1, items: new Map(), selected: null, view: null};
    let searchTimer;

    const select = id => {
        state.selected = id;
        insert.disabled = !id;

        for (const card of grid.querySelectorAll("button")) {
            card.setAttribute("aria-pressed", String(card.dataset.id === id));
        }
    };

    const choose = () => {
        const item = state.items.get(state.selected);
        const editor = [...editors.values()].find(candidate => candidate.view === state.view);

        dialog.close();

        if (item && editor) {
            editor.insertImage(item, alt.value.trim());
        }
    };

    grid.addEventListener("click", event => {
        const card = event.target.closest("button[data-id]");

        if (card) {
            select(card.dataset.id);
        }
    });

    grid.addEventListener("dblclick", event => {
        const card = event.target.closest("button[data-id]");

        if (card) {
            select(card.dataset.id);
            choose();
        }
    });

    search.addEventListener("input", () => {
        clearTimeout(searchTimer);
        searchTimer = setTimeout(() => loadPage(state, 1), 250);
    });

    more.addEventListener("click", () => loadPage(state, state.page + 1));
    insert.addEventListener("click", choose);
    cancel.addEventListener("click", () => dialog.close());
    close.addEventListener("click", () => dialog.close());

    dialog.addEventListener("close", () => {
        if (state.view && document.contains(state.view.dom)) {
            state.view.focus();
        }
    });

    state.select = select;

    return state;
}

async function loadPage(state, page) {
    const query = new URLSearchParams({page: String(page)});
    const term = state.search.value.trim();

    if (term) {
        query.set("q", term);
    }

    if (page === 1) {
        state.grid.replaceChildren();
        state.items.clear();
        state.select(null);
    }

    state.status.textContent = t("Loading");
    state.more.hidden = true;

    let result;

    try {
        const response = await fetch(`${pickerURL}?${query}`, {headers: {Accept: "application/json"}});

        if (!response.ok || !response.headers.get("Content-Type")?.includes("json")) {
            throw new Error(String(response.status));
        }

        result = await response.json();
    } catch {
        state.status.textContent = t("The media library could not be loaded.");

        return;
    }

    state.page = page;

    for (const item of result.items || []) {
        state.items.set(item.id, item);

        const image = element("img", {src: item.thumb || item.url, alt: "", loading: "lazy"});
        const name = element("span", {text: item.filename});
        const card = element("button", {"type": "button", "data-id": item.id, "aria-pressed": "false",
            "title": item.filename}, image, name);

        state.grid.append(card);
    }

    state.more.hidden = !result.next;
    state.status.textContent = state.items.size === 0 ? t("No images found.") : "";
}

function openPicker(view) {
    picker ??= createPicker();
    picker.view = view;
    picker.alt.value = "";
    picker.search.value = "";
    picker.dialog.showModal();
    picker.search.focus();
    loadPage(picker, 1);
}

// ---------------------------------------------------------------------------
// Entry points for loader.js.

export function enhanceAll(root) {
    for (const textarea of root.querySelectorAll("textarea.markdown")) {
        if (editors.has(textarea) || textarea.closest("[data-markdown-editor=off]")) {
            continue;
        }

        // A page restored from a snapshot may hold the markup of an editor
        // without the editor itself; the field leaves it before a new one
        // is made.
        textarea.closest(".md-editor")?.replaceWith(textarea);
        editors.set(textarea, new MarkdownEditor(textarea));
    }
}

// sweep destroys the editors whose field left the page.
export function sweep() {
    for (const [textarea, editor] of editors) {
        if (!textarea.isConnected) {
            editor.destroy();
        }
    }

    if (picker?.view && !picker.view.dom.isConnected && picker.dialog.open) {
        picker.dialog.close();
    }
}

// markdown converts without an editor, for checks in the browser console.
export const markdown = {parse, serialize};
