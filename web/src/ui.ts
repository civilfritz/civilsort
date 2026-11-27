// Returns the element with the given ID if it exists and is an instance of type.
export function byId<T extends HTMLElement>(id: string, type: new () => T): T | null {
    const el = document.getElementById(id);
    return el instanceof type ? el : null;
}

export function escapeHtml(text: string): string {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Shows an error toast that fades out after three seconds.
export function showError(message: string): void {
    const toast = document.createElement('div');
    toast.className = 'toast toast-error';
    toast.textContent = message;
    document.body.appendChild(toast);

    setTimeout(() => {
        toast.classList.add('fade-out');
        setTimeout(() => toast.remove(), 300);
    }, 3000);
}

export function errorMessage(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

// Fails the build if a switch over a union is missing a case.
export function assertNever(value: never): never {
    throw new Error(`Unexpected value: ${JSON.stringify(value)}`);
}
