// Drag and drop for admin trees, such as the page tree, on top of their up
// and down buttons, which stay for the keyboard and for browsers without
// JavaScript. Pointer events cover the mouse, pens and touch alike.
//
// The markup marks the tree with data-tree and every row with
// data-tree-item, data-id and data-parent (the ID of the row it is below,
// empty for the root). A row that can be dragged has data-move, the URL its
// new place is posted to as parent and index (0 is the first place among
// the new siblings), and a [data-tree-handle] to drag it by. A row with
// data-leaf takes no rows below it. Rows are in tree order: each row
// followed by the rows below it.
//
// While dragging, a line shows where the row lands between other rows, and
// the row it lands below is highlighted. Escape cancels. The drop posts a
// form like the up and down buttons do, so HTMX swaps in the tree as the
// server has it. Event delegation keeps this working on trees HTMX swaps in
// later; the module itself runs once.

const threshold = 4;
const edge = 48;
const maxScrollStep = 18;

let drag = null;

document.documentElement.setAttribute("data-tree-ready", "");
document.addEventListener("pointerdown", start);

function start(event) {
    if (drag || event.button !== 0 || !event.isPrimary) {
        return;
    }

    const handle = event.target.closest?.("[data-tree-handle]");
    const row = handle?.closest("[data-tree-item][data-move]");
    const tree = row?.closest("[data-tree]");

    if (!tree) {
        return;
    }

    // No text selection, no native image drag.
    event.preventDefault();
    handle.setPointerCapture(event.pointerId);

    drag = {
        pointer: event.pointerId,
        handle,
        row,
        tree,
        startX: event.clientX,
        startY: event.clientY,
        x: event.clientX,
        y: event.clientY,
        active: false,
        drop: null,
        frame: 0,
    };

    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", finish);
    handle.addEventListener("pointercancel", cancel);
    handle.addEventListener("lostpointercapture", cancel);
    document.addEventListener("keydown", escape, true);
}

function move(event) {
    if (!drag || event.pointerId !== drag.pointer) {
        return;
    }

    drag.x = event.clientX;
    drag.y = event.clientY;

    if (!drag.active) {
        if (Math.hypot(drag.x - drag.startX, drag.y - drag.startY) < threshold) {
            return;
        }

        activate();
    }

    drag.ghost.style.transform = `translate(${drag.x + 12}px, ${drag.y + 8}px)`;
    update();
}

// activate turns a press on the handle into a drag once the pointer moved.
function activate() {
    const items = [...drag.tree.querySelectorAll("[data-tree-item]")].map(element => ({
        element,
        id: element.dataset.id,
        parent: element.dataset.parent,
        leaf: element.hasAttribute("data-leaf"),
    }));

    // The dragged row and the rows below it are no place to drop it.
    const excluded = new Set([drag.row.dataset.id]);

    for (const item of items) {
        if (excluded.has(item.parent)) {
            excluded.add(item.id);
        }
    }

    drag.items = items;
    drag.byID = new Map(items.map(item => [item.id, item]));
    drag.excluded = excluded;
    drag.scroller = scrollParent(drag.tree);
    drag.active = true;

    drag.line = document.createElement("div");
    drag.line.className = "tree-drop-line";
    drag.line.hidden = true;
    drag.tree.append(drag.line);

    drag.ghost = document.createElement("div");
    drag.ghost.className = "tree-drag-ghost";
    drag.ghost.textContent = drag.row.querySelector("a")?.firstChild?.textContent.trim() || "";
    document.body.append(drag.ghost);

    drag.row.classList.add("tree-dragging");
    document.documentElement.setAttribute("data-tree-dragging", "");
    drag.frame = requestAnimationFrame(scroll);
}

// update finds the place under the pointer and shows it.
function update() {
    const drop = place(drag.x, drag.y);

    drag.drop = drop;
    drag.tree.querySelectorAll(".tree-drop-into").forEach(element => element.classList.remove("tree-drop-into"));
    drag.line.hidden = !drop || drop.into;

    if (!drop) {
        return;
    }

    if (drop.into) {
        drop.target.classList.add("tree-drop-into");

        return;
    }

    const box = drag.tree.getBoundingClientRect();

    drag.line.style.top = `${drop.lineY - box.top}px`;
    drag.line.style.left = `${drop.lineX - box.left}px`;
    drag.line.style.right = "0";
}

// place is where the dragged row lands at x, y: its new parent and index,
// with what to show; null where it cannot land. The upper and lower quarter
// of a row place it before or after that row, the middle below it.
function place(x, y) {
    const element = document.elementFromPoint(x, y)?.closest("[data-tree-item]");

    if (!element || !drag.tree.contains(element)) {
        return null;
    }

    const item = drag.byID.get(element.dataset.id);

    if (!item || drag.excluded.has(item.id)) {
        return null;
    }

    const box = element.getBoundingClientRect();
    const ratio = (y - box.top) / box.height;
    // The root and rows whose parent is gone have no siblings to join.
    const beside = item.parent !== "" && drag.byID.has(item.parent);
    const into = !item.leaf;

    let zone = "into";

    if (beside && into) {
        zone = ratio < 0.25 ? "before" : ratio > 0.75 ? "after" : "into";
    } else if (beside) {
        zone = ratio < 0.5 ? "before" : "after";
    } else if (!into) {
        return null;
    }

    if (zone === "into") {
        return {parent: item.id, index: siblings(item.id).length, into: true, target: element};
    }

    // Right after a row with rows below it is the first place below it.
    const children = siblings(item.id);

    if (zone === "after" && children.length > 0) {
        const first = children[0].element.getBoundingClientRect();

        return {parent: item.id, index: 0, lineX: first.left, lineY: first.top};
    }

    const index = siblings(item.parent).indexOf(item);

    if (zone === "before") {
        return {parent: item.parent, index, lineX: box.left, lineY: box.top};
    }

    return {parent: item.parent, index: index + 1, lineX: box.left, lineY: box.bottom};
}

// siblings are the rows below parent in order, without the dragged row.
function siblings(parent) {
    return drag.items.filter(item => item.parent === parent && item.id !== drag.row.dataset.id);
}

// scroll moves the tree's scroll container while the pointer is near its
// top or bottom edge, faster closer to the edge.
function scroll() {
    if (!drag?.active) {
        return;
    }

    const scroller = drag.scroller;
    const page = scroller === document.scrollingElement;
    const top = page ? 0 : scroller.getBoundingClientRect().top;
    const bottom = page ? window.innerHeight : scroller.getBoundingClientRect().bottom;

    let step = 0;

    if (drag.y < top + edge) {
        step = -Math.ceil(maxScrollStep * (top + edge - drag.y) / edge);
    } else if (drag.y > bottom - edge) {
        step = Math.ceil(maxScrollStep * (drag.y - bottom + edge) / edge);
    }

    if (step !== 0) {
        const before = scroller.scrollTop;

        scroller.scrollTop += step;

        if (scroller.scrollTop !== before) {
            update();
        }
    }

    drag.frame = requestAnimationFrame(scroll);
}

function scrollParent(element) {
    for (let parent = element.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
        const overflow = getComputedStyle(parent).overflowY;

        if ((overflow === "auto" || overflow === "scroll") && parent.scrollHeight > parent.clientHeight) {
            return parent;
        }
    }

    return document.scrollingElement;
}

function finish(event) {
    if (!drag || event.pointerId !== drag.pointer) {
        return;
    }

    const {active, drop, row, tree} = drag;

    stop();

    if (!active) {
        return;
    }

    // The click that ends a drag is no click on the row.
    window.addEventListener("click", swallow, {capture: true, once: true});
    setTimeout(() => window.removeEventListener("click", swallow, {capture: true}), 0);

    if (!drop || unchanged(drop, row, tree)) {
        return;
    }

    row.classList.add("tree-moving");
    submit(row.dataset.move, drop, tree);
}

// unchanged reports whether drop is the row's current place.
function unchanged(drop, row, tree) {
    if (drop.parent !== row.dataset.parent) {
        return false;
    }

    const current = [...tree.querySelectorAll("[data-tree-item]")]
        .filter(element => element.dataset.parent === drop.parent)
        .indexOf(row);

    return current === drop.index;
}

// submit posts the new place as a form of the tree, which HTMX sends like
// the up and down buttons; without HTMX the browser posts it.
function submit(url, drop, tree) {
    const form = document.createElement("form");

    form.method = "post";
    form.action = url;
    form.hidden = true;

    for (const [name, value] of Object.entries({parent: drop.parent, index: drop.index})) {
        const input = document.createElement("input");

        input.type = "hidden";
        input.name = name;
        input.value = String(value);
        form.append(input);
    }

    tree.append(form);
    window.htmx?.process?.(form);
    form.requestSubmit();
}

function cancel(event) {
    if (!drag || (event?.pointerId !== undefined && event.pointerId !== drag.pointer)) {
        return;
    }

    stop();
}

function escape(event) {
    if (event.key !== "Escape" || !drag) {
        return;
    }

    event.preventDefault();
    event.stopPropagation();
    stop();
}

// stop ends the drag and removes what it showed; it keeps the drop.
function stop() {
    const current = drag;

    drag = null;

    cancelAnimationFrame(current.frame);
    document.removeEventListener("keydown", escape, true);

    const handle = current.handle;

    handle.removeEventListener("pointermove", move);
    handle.removeEventListener("pointerup", finish);
    handle.removeEventListener("pointercancel", cancel);
    handle.removeEventListener("lostpointercapture", cancel);

    if (handle.hasPointerCapture?.(current.pointer)) {
        handle.releasePointerCapture(current.pointer);
    }

    current.line?.remove();
    current.ghost?.remove();
    current.row.classList.remove("tree-dragging");
    current.tree.querySelectorAll(".tree-drop-into").forEach(element => element.classList.remove("tree-drop-into"));
    document.documentElement.removeAttribute("data-tree-dragging");
}

function swallow(event) {
    event.preventDefault();
    event.stopPropagation();
}
