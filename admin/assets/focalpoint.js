// The focal point editor of an image in the media library, on top of its
// number fields, which stay for browsers without JavaScript. Editors click
// or drag in the image to place the marker, or select the marker and move it
// with the arrow keys (Shift moves it further). The crop previews follow at
// once; saving the form stores the point.
//
// The markup wraps everything in [data-focal-editor] with the image's
// data-width and data-height: a [data-focal-frame] as large as the image
// with a [data-focal-marker] inside, whose data-x and data-y are the point
// it shows in percent, previews whose images have
// data-focal-crop="<width>:<height>", and the form with the fields FocalX
// and FocalY in percent. The marker's data-label names it and its
// data-value-text describes its place, with {x} and {y} for the percentages.
//
// The previews place the image as media.FocalPoint.ObjectPosition does, so
// they show what crop variants of those proportions show; until then their
// classes of focal-points.css place them about there. Event delegation
// keeps this working on screens HTMX swaps in later; the module runs once.

const step = 1;
const bigStep = 10;

let drag = null;

document.documentElement.setAttribute("data-focal-ready", "");
document.addEventListener("pointerdown", start);
document.addEventListener("keydown", move);
document.addEventListener("input", typed);

new MutationObserver(prepareAll).observe(document.documentElement, {childList: true, subtree: true});
prepareAll();

// prepareAll places the markers and the previews on the point in the
// number fields, and makes the markers reachable with the keyboard. The
// markup has no style attributes, which a Content-Security-Policy blocks;
// a script may set element.style.
function prepareAll() {
    document.querySelectorAll("[data-focal-marker]:not([tabindex])").forEach(marker => {
        marker.setAttribute("tabindex", "0");
        marker.setAttribute("role", "slider");
        marker.setAttribute("aria-label", marker.dataset.label || "Focal point");
        marker.setAttribute("aria-valuemin", "0");
        marker.setAttribute("aria-valuemax", "100");
        marker.removeAttribute("aria-hidden");

        const editor = marker.closest("[data-focal-editor]");
        const point = shown(marker);

        show(editor, point.x, point.y);
    });
}

// shown is the point the marker's data-x and data-y say it shows, in
// percent: the saved point, also when the fields hold one that was refused.
function shown(marker) {
    const value = name => {
        const number = parseFloat(marker.dataset[name]);

        return Number.isFinite(number) ? clamp(number) : 50;
    };

    return {x: value("x"), y: value("y")};
}

function start(event) {
    if (event.button !== 0 || !event.isPrimary) {
        return;
    }

    const frame = event.target.closest?.("[data-focal-frame]");
    const editor = frame?.closest("[data-focal-editor]");

    if (!editor) {
        return;
    }

    event.preventDefault();

    drag = {frame, editor, pointer: event.pointerId};
    frame.setPointerCapture(event.pointerId);
    frame.addEventListener("pointermove", follow);
    frame.addEventListener("pointerup", stop);
    frame.addEventListener("pointercancel", stop);

    follow(event);
}

function follow(event) {
    if (!drag || event.pointerId !== drag.pointer) {
        return;
    }

    const box = drag.frame.getBoundingClientRect();

    if (box.width === 0 || box.height === 0) {
        return;
    }

    const x = (event.clientX - box.left) / box.width * 100;
    const y = (event.clientY - box.top) / box.height * 100;

    place(drag.editor, x, y);
}

function stop(event) {
    if (!drag || event.pointerId !== drag.pointer) {
        return;
    }

    const {frame, editor} = drag;

    frame.removeEventListener("pointermove", follow);
    frame.removeEventListener("pointerup", stop);
    frame.removeEventListener("pointercancel", stop);
    drag = null;

    editor.querySelector("[data-focal-marker]")?.focus({preventScroll: true});
}

function move(event) {
    const marker = event.target.closest?.("[data-focal-marker]");

    if (!marker) {
        return;
    }

    const distance = event.shiftKey ? bigStep : step;
    const moves = {
        ArrowLeft: [-distance, 0],
        ArrowRight: [distance, 0],
        ArrowUp: [0, -distance],
        ArrowDown: [0, distance],
    };
    const delta = moves[event.key];

    if (!delta) {
        return;
    }

    event.preventDefault();

    const editor = marker.closest("[data-focal-editor]");
    const point = current(editor);

    place(editor, point.x + delta[0], point.y + delta[1]);
}

// typed follows the number fields, so what the editor types shows too.
function typed(event) {
    const field = event.target;
    const editor = field.closest?.("[data-focal-editor]");

    if (!editor || (field.name !== "FocalX" && field.name !== "FocalY")) {
        return;
    }

    const point = current(editor);

    show(editor, clamp(point.x), clamp(point.y));
}

// current is the point in the number fields, in percent.
function current(editor) {
    const value = name => {
        const number = parseFloat(editor?.querySelector(`[name="${name}"]`)?.value);

        return Number.isFinite(number) ? number : 50;
    };

    return {x: value("FocalX"), y: value("FocalY")};
}

// place moves the point and writes it into the number fields.
function place(editor, x, y) {
    x = clamp(x);
    y = clamp(y);

    const fields = {FocalX: x, FocalY: y};

    for (const [name, value] of Object.entries(fields)) {
        const field = editor.querySelector(`[name="${name}"]`);

        if (field) {
            field.value = String(value);
        }
    }

    show(editor, x, y);
}

// show puts the marker and the previews on a point in percent.
function show(editor, x, y) {
    const marker = editor.querySelector("[data-focal-marker]");

    if (marker) {
        marker.style.left = `${x}%`;
        marker.style.top = `${y}%`;
        describe(marker, x, y);
    }

    const width = Number(editor.dataset.width);
    const height = Number(editor.dataset.height);

    editor.querySelectorAll("[data-focal-crop]").forEach(image => {
        const [boxWidth, boxHeight] = image.dataset.focalCrop.split(":").map(Number);

        image.style.objectPosition = objectPosition(x / 100, y / 100, width, height, boxWidth, boxHeight);
    });
}

function describe(marker, x, y) {
    const text = marker.dataset.valueText || "{x}, {y}";

    marker.setAttribute("aria-valuenow", String(x));
    marker.setAttribute("aria-valuetext", text.replace("{x}", `${x}%`).replace("{y}", `${y}%`));
}

// clamp keeps a percentage inside the image, with one decimal.
function clamp(value) {
    return Math.round(Math.min(Math.max(value, 0), 100) * 10) / 10;
}

// objectPosition mirrors media.FocalPoint.ObjectPosition: the crop with the
// box's proportions is placed with the focal point in its middle, kept
// inside the image, and the browser is told how far to move the image.
function objectPosition(focusX, focusY, width, height, boxWidth, boxHeight) {
    if (!(width > 0 && height > 0 && boxWidth > 0 && boxHeight > 0)) {
        return `${percent(focusX)} ${percent(focusY)}`;
    }

    let partWidth = width;
    let partHeight = height;

    if (width * boxHeight > height * boxWidth) {
        partWidth = Math.max(1, Math.round(height * boxWidth / boxHeight));
    } else {
        partHeight = Math.max(1, Math.round(width * boxHeight / boxWidth));
    }

    const position = (length, size, focus) => {
        if (size >= length) {
            return 0.5;
        }

        const start = Math.min(Math.max(Math.round(focus * length - size / 2), 0), length - size);

        return start / (length - size);
    };

    return `${percent(position(width, partWidth, focusX))} ${percent(position(height, partHeight, focusY))}`;
}

function percent(fraction) {
    return `${Math.round(fraction * 1000) / 10}%`;
}
