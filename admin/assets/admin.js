import htmx from "htmx";
// go-partial's exp/sse client, window.goPartialSSE: it follows the presence
// stream the header names, applies its patches and reports the page (see
// admin/presence.go).
import "partial-sse";
// go-partial's exp/flash client, window.goPartialFlash: it hides success and
// info toasts after a while, not while they are hovered or focused, keeps
// warnings and errors (the stack's data-flash-autohide-warn="off"), removes
// a toast with its data-flash-dismiss button or Escape, and shows one toast
// for a request that failed, as templates/flash-messages.gohtml configures.
import "partial-flash";
// go-partial's exp/interactions client: a submit button with a question of
// its own (clientAttrs "submitConfirm") asks it before it sends its form,
// and every question, also hx-confirm, is asked in the layout's dialog
// (data-partial-confirm-dialog) rather than with window.confirm.
import "partial-interactions";
window.htmx = htmx;

// Track request contexts because HTMX 4.0.0 retains indicator state when an
// HX-Location response starts its follow-up request.
const pendingRequests = new Set();
function refreshProgress() {
    const progress = document.querySelector("#admin-progress");
    if (progress) progress.hidden = pendingRequests.size === 0;
}
document.addEventListener("htmx:before:request", event => {
    pendingRequests.add(event.detail.ctx);
    refreshProgress();
});

document.addEventListener("htmx:finally:request", event => {
    pendingRequests.delete(event.detail.ctx);
    refreshProgress();
});

// Navigation groups stay collapsed in this browser once the editor collapsed
// them; the group of the current page is always open. The sidebar is
// replaced on every navigation, so the state is applied again as soon as a
// new one arrives, before it is painted.
const collapsedKey = "webthings-admin-collapsed-groups";

function collapsedGroups() {
    try {
        return new Set(JSON.parse(localStorage.getItem(collapsedKey) || "[]"));
    } catch {
        return new Set();
    }
}

function setCollapsed(group, collapsed) {
    group.toggleAttribute("data-collapsed", collapsed);
    group.querySelector(".nav-group-toggle")?.setAttribute("aria-expanded", String(!collapsed));
}

function applyCollapsedGroups() {
    const collapsed = collapsedGroups();
    document.querySelectorAll("[data-nav-group]").forEach(group => {
        setCollapsed(group, collapsed.has(group.dataset.navGroup) && !group.hasAttribute("data-active"));
    });
}

document.addEventListener("click", event => {
    const toggle = event.target.closest(".nav-group-toggle");
    if (!toggle) return;
    const group = toggle.closest("[data-nav-group]");
    const collapse = !group.hasAttribute("data-collapsed");
    setCollapsed(group, collapse);
    const collapsed = collapsedGroups();
    if (collapse) collapsed.add(group.dataset.navGroup);
    else collapsed.delete(group.dataset.navGroup);
    try {
        localStorage.setItem(collapsedKey, JSON.stringify([...collapsed]));
    } catch {
        // Without storage the group stays as it is until the next page.
    }
});

new MutationObserver(records => {
    const sidebar = records.some(record => [...record.addedNodes].some(node =>
        node.nodeType === Node.ELEMENT_NODE && (node.id === "admin-sidebar" || node.querySelector("#admin-sidebar"))));
    if (sidebar) applyCollapsedGroups();
}).observe(document.documentElement, {childList: true, subtree: true});
applyCollapsedGroups();

// An open menu, such as the dashboard's "New…", closes on a click
// elsewhere and on Escape.
document.addEventListener("click", event => {
    document.querySelectorAll("details.action-menu[open]").forEach(menu => {
        if (!menu.contains(event.target)) menu.open = false;
    });
});
document.addEventListener("keydown", event => {
    if (event.key !== "Escape") return;
    document.querySelectorAll("details.action-menu[open]").forEach(menu => {
        menu.open = false;
        menu.querySelector("summary")?.focus();
    });
});

// The rights matrix of a group: select-all boxes for the whole matrix, a
// column (one right for every section), a navigation group and a row (every
// right of a section). They are hidden until this code runs, so without
// JavaScript the plain boxes still work. Every right needs read, so checking
// another right checks read and unchecking read clears the row; the server
// saves the same.
function matrixCells(scope) {
    return [...scope.querySelectorAll("[data-matrix-cell]")];
}

function isRead(cell) {
    return cell.value.endsWith(":read");
}

function columnCells(matrix, index) {
    return [...matrix.querySelectorAll("[data-matrix-row]")]
        .map(row => row.querySelectorAll("[data-matrix-cell]")[index])
        .filter(Boolean);
}

function showState(box, cells) {
    const checked = cells.filter(cell => cell.checked).length;
    box.checked = cells.length > 0 && checked === cells.length;
    box.indeterminate = checked > 0 && checked < cells.length;
}

function refreshMatrix(matrix) {
    const all = matrix.querySelector("[data-matrix-all]");
    if (all) showState(all, matrixCells(matrix));
    matrix.querySelectorAll("[data-matrix-column]").forEach((box, index) => showState(box, columnCells(matrix, index)));
    matrix.querySelectorAll("[data-matrix-group]").forEach(group => {
        const box = group.querySelector("[data-matrix-group-toggle]");
        if (box) showState(box, matrixCells(group));
    });
    matrix.querySelectorAll("[data-matrix-row]").forEach(row => {
        const box = row.querySelector("[data-matrix-row-toggle]");
        if (box) showState(box, matrixCells(row));
    });
}

// setCells checks or clears cells and keeps read consistent in their rows.
function setCells(matrix, cells, checked) {
    cells.forEach(cell => { cell.checked = checked; });
    const rows = new Set(cells.map(cell => cell.closest("[data-matrix-row]")));
    rows.forEach(row => {
        const rowCells = matrixCells(row);
        const read = rowCells.find(isRead);
        if (!read) return;
        if (!checked && cells.includes(read)) {
            rowCells.forEach(cell => { cell.checked = false; });
        } else if (rowCells.some(cell => cell.checked && !isRead(cell))) {
            read.checked = true;
        }
    });
    refreshMatrix(matrix);
}

function initMatrices() {
    document.querySelectorAll("[data-rights-matrix]:not([data-matrix-ready])").forEach(matrix => {
        matrix.setAttribute("data-matrix-ready", "");
        matrix.querySelectorAll("[data-matrix-all], [data-matrix-column], [data-matrix-group-toggle], [data-matrix-row-toggle]")
            .forEach(box => { box.hidden = false; });
        refreshMatrix(matrix);
    });
}

document.addEventListener("change", event => {
    const box = event.target;
    const matrix = box.closest?.("[data-rights-matrix]");
    if (!matrix) return;
    if (box.matches("[data-matrix-cell]")) {
        setCells(matrix, [box], box.checked);
    } else if (box.matches("[data-matrix-all]")) {
        setCells(matrix, matrixCells(matrix), box.checked);
    } else if (box.matches("[data-matrix-column]")) {
        const index = [...matrix.querySelectorAll("[data-matrix-column]")].indexOf(box);
        setCells(matrix, columnCells(matrix, index), box.checked);
    } else if (box.matches("[data-matrix-group-toggle]")) {
        setCells(matrix, matrixCells(box.closest("[data-matrix-group]")), box.checked);
    } else if (box.matches("[data-matrix-row-toggle]")) {
        setCells(matrix, matrixCells(box.closest("[data-matrix-row]")), box.checked);
    }
});

new MutationObserver(records => {
    const added = records.some(record => [...record.addedNodes].some(node =>
        node.nodeType === Node.ELEMENT_NODE && (node.matches("[data-rights-matrix]") || node.querySelector("[data-rights-matrix]"))));
    if (added) initMatrices();
}).observe(document.documentElement, {childList: true, subtree: true});
initMatrices();

// The trash: its buttons work on the ticked items, so they are disabled
// while none is ticked, and the box in the head of a section's table ticks
// all of its items. The boxes are hidden until this code runs, so without
// JavaScript the buttons stay enabled and the server refuses an empty
// selection.
function trashItems(scope) {
    return [...scope.querySelectorAll('input[name="item"]')];
}

function refreshTrash(form) {
    const none = !trashItems(form).some(item => item.checked);
    form.querySelectorAll("[data-trash-action]").forEach(button => { button.disabled = none; });
    form.querySelectorAll("[data-trash-all]").forEach(box => showState(box, trashItems(box.closest("table"))));
}

function initTrash() {
    document.querySelectorAll("[data-trash]:not([data-trash-ready])").forEach(form => {
        form.setAttribute("data-trash-ready", "");
        form.querySelectorAll("[data-trash-all]").forEach(box => {
            box.hidden = trashItems(box.closest("table")).length === 0;
        });
        refreshTrash(form);
    });
}

document.addEventListener("change", event => {
    const box = event.target;
    const form = box.closest?.("[data-trash]");
    if (!form) return;
    if (box.matches("[data-trash-all]")) {
        trashItems(box.closest("table")).forEach(item => { item.checked = box.checked; });
    }
    refreshTrash(form);
});

new MutationObserver(records => {
    const added = records.some(record => [...record.addedNodes].some(node =>
        node.nodeType === Node.ELEMENT_NODE && (node.matches("[data-trash]") || node.querySelector("[data-trash]"))));
    if (added) initTrash();
}).observe(document.documentElement, {childList: true, subtree: true});
initTrash();

// Escape in a list's search field clears it, which searches again.
document.addEventListener("keydown", event => {
    const input = event.target.closest?.("[data-admin-search]");
    if (!input || event.key !== "Escape" || input.value === "") return;
    event.preventDefault();
    input.value = "";
    input.dispatchEvent(new Event("input", {bubbles: true}));
});

// A read-only field with data-select-all selects its whole text when focused
// or clicked, for text to copy, such as a file's Markdown link.
for (const type of ["focusin", "click"]) {
    document.addEventListener(type, event => {
        const field = event.target.closest?.("[data-select-all]");
        if (field) field.select();
    });
}

// Signing out ends this tab's presence with the session: the form carries
// the tab's id, which go-partial's stream client chose. The layout names the
// address (data-logout), so the script works wherever the admin is.
const logoutURL = document.documentElement.dataset.logout;

document.addEventListener("submit", event => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || new URL(form.action, location.href).pathname !== logoutURL) return;
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = "tab";
    input.value = window.goPartialSSE.clientId;
    form.append(input);
}, true);
