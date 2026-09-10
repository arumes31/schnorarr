// Apply the saved operational theme before paint; storage can be unavailable.
(() => {
    const themes = ['green', 'purple', 'orange', 'red', 'blue'];
    window.applyTheme = (name, save = true) => {
        if (!themes.includes(name)) name = 'green';
        document.documentElement.dataset.theme = name;
        const select = document.getElementById('theme-select');
        if (select) select.value = name;
        if (save) {
            try { localStorage.setItem('schnorarr-theme', name); } catch { /* The selection still works for this page. */ }
        }
    };
    let saved = 'green';
    try { saved = localStorage.getItem('schnorarr-theme') || saved; } catch { /* Use the default theme. */ }
    window.applyTheme(saved, false);
    document.addEventListener('DOMContentLoaded', () => {
        window.applyTheme(document.documentElement.dataset.theme, false);
        const preferences = document.getElementById('sync-preferences');
        if (preferences && matchMedia('(min-width: 1025px)').matches) preferences.open = true;
    });
    window.addEventListener('storage', event => {
        if (event.key === 'schnorarr-theme') window.applyTheme(event.newValue, false);
    });
})();
