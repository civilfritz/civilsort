import { initBallot } from './ballot';
import { byId } from './ui';

initMenu();
initTabs();
initThemeToggle();

const ballotRoot = byId('ballot', HTMLElement);
if (ballotRoot) {
    initBallot(ballotRoot);
}

function initMenu(): void {
    const hamburgerMenu = byId('hamburger-menu', HTMLButtonElement);
    const menuDropdown = byId('menu-dropdown', HTMLDivElement);
    const menuWrapper = byId('menu-wrapper', HTMLDivElement);
    if (!hamburgerMenu || !menuDropdown || !menuWrapper) return;

    hamburgerMenu.addEventListener('click', (e) => {
        e.stopPropagation();
        menuDropdown.classList.toggle('open');
    });

    // Close menu when clicking outside
    document.addEventListener('click', (e) => {
        if (menuDropdown.classList.contains('open') && !menuWrapper.contains(e.target as Node | null)) {
            menuDropdown.classList.remove('open');
        }
    });

    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && menuDropdown.classList.contains('open')) {
            menuDropdown.classList.remove('open');
        }
    });

    // Don't close menu when clicking theme toggle, copy, or share buttons
    for (const selector of ['.theme-toggle', '#copy-url-btn', '#share-url-btn', '#device-link-btn']) {
        menuDropdown.querySelector(selector)?.addEventListener('click', (e) => {
            e.stopPropagation();
        });
    }
}

function initTabs(): void {
    const buttons = document.querySelectorAll<HTMLElement>('.tab-btn');
    const panels = document.querySelectorAll<HTMLElement>('.tab-panel');

    function activate(btn: HTMLElement, panel: HTMLElement): void {
        buttons.forEach(b => {
            b.classList.remove('active');
            b.setAttribute('aria-selected', 'false');
        });
        panels.forEach(p => p.classList.remove('active'));
        btn.classList.add('active');
        btn.setAttribute('aria-selected', 'true');
        panel.classList.add('active');
    }

    buttons.forEach(btn => {
        btn.addEventListener('click', () => {
            const tabId = btn.dataset['tab'];
            const panel = tabId ? document.getElementById(tabId) : null;
            if (!tabId || !panel) return;
            activate(btn, panel);
            // Save active tab to hash so it survives reload
            location.hash = tabId;
        });
    });

    // Restore active tab from hash on page load
    if (location.hash) {
        const tabId = location.hash.substring(1);
        const btn = document.querySelector<HTMLElement>(`[data-tab="${CSS.escape(tabId)}"]`);
        const panel = document.getElementById(tabId);
        if (btn && panel) {
            activate(btn, panel);
        }
    }
}

function initThemeToggle(): void {
    const themeToggle = byId('theme-toggle', HTMLButtonElement);
    if (!themeToggle) return;

    const currentTheme = (): string => document.documentElement.dataset['theme'] || 'light';
    const updateLabel = (): void => {
        themeToggle.textContent = currentTheme() === 'dark' ? 'Light mode' : 'Dark mode';
    };
    updateLabel();

    themeToggle.addEventListener('click', () => {
        const newTheme = currentTheme() === 'dark' ? 'light' : 'dark';
        document.documentElement.dataset['theme'] = newTheme;
        try {
            localStorage.setItem('theme', newTheme);
        } catch {
            // Storage unavailable; theme still applies for this page view.
        }
        updateLabel();
    });
}
