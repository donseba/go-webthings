// The Markdown editor turns every Markdown field of the admin (a go-form
// textarea with the class "markdown") into a formatted-text editor. This
// loader is on every admin page; the editor itself (editor.js and the
// ProseMirror bundle) is only fetched once a page has such a field.
//
// The textarea stays the form field: the editor writes its Markdown back
// into it, so validation and submitting work as before, and without
// JavaScript the textarea is simply used as it is.
const selector = "textarea.markdown";

let editor;

function load() {
    editor ??= import("./editor.js").catch(error => {
        // Without the editor the plain textarea keeps working.
        console.error("Markdown editor", error);
        editor = undefined;
    });

    return editor;
}

function hasField(node) {
    return node.nodeType === Node.ELEMENT_NODE && (node.matches(selector) || node.querySelector(selector) !== null);
}

function enhance() {
    load().then(module => module?.enhanceAll(document));
}

// htmx replaces the content on navigation and after a failed submit, so new
// fields can appear at any time; removed editors are cleaned up.
new MutationObserver(records => {
    let added = false;
    let removed = false;

    for (const record of records) {
        added ||= [...record.addedNodes].some(hasField);
        removed ||= record.removedNodes.length > 0;
    }

    if (added) {
        enhance();
    }

    if (removed && editor) {
        editor.then(module => module?.sweep());
    }
}).observe(document.documentElement, {childList: true, subtree: true});

if (document.querySelector(selector)) {
    enhance();
}
