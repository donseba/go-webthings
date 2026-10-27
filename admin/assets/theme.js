(() => {
    const key = "webthings-admin-theme";
    const system = window.matchMedia("(prefers-color-scheme: dark)");
    const choices = ["light", "dark", "retro", "system"];
    let choice = "light";
    try { choice = localStorage.getItem(key) || "light"; } catch {}
    if (!choices.includes(choice)) choice = "light";
    function refresh() {
        document.documentElement.dataset.theme = choice === "system" ? (system.matches ? "dark" : "light") : choice;
        document.querySelectorAll("[data-theme-choice]").forEach(button => {
            button.setAttribute("aria-pressed", String(button.dataset.themeChoice === choice));
        });
    }
    document.addEventListener("click", event => {
        const button = event.target.closest("[data-theme-choice]");
        if (!button) return;
        choice = button.dataset.themeChoice;
        try { localStorage.setItem(key, choice); } catch {}
        refresh();
    });
    window.addEventListener("storage", event => {
        if (event.key !== key) return;
        choice = choices.includes(event.newValue) ? event.newValue : "light";
        refresh();
    });
    system.addEventListener("change", refresh);
    document.addEventListener("DOMContentLoaded", refresh);
    document.addEventListener("htmx:after:swap", refresh);
    window.webthingsTheme = { refresh };
    refresh();
})();
