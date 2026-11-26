import Sortable from 'sortablejs';

import type { ServerMessage } from './messages';
import type {
    ItemEntry,
    ParticipantEntry,
    RankedItem,
    ResultEntry,
    SaveRankingsRequest,
    ToggleResponse,
} from './protocol.gen';
import { assertNever, byId, errorMessage, escapeHtml, showError } from './ui';

const minReconnectDelay = 1000;
const maxReconnectDelay = 30000;

// Wires up the ballot page. root carries the ballot and user IDs as data attributes.
export function initBallot(root: HTMLElement): void {
    const ballotId = root.dataset['ballotId'];
    const currentPublicId = root.dataset['currentPublicId'];
    const rankedList = byId('ranked-list', HTMLUListElement);
    const unrankedList = byId('unranked-list', HTMLUListElement);

    if (!ballotId || !currentPublicId || !rankedList || !unrankedList) {
        console.error('Ballot page is missing required elements');
        return;
    }

    new BallotPage(ballotId, currentPublicId, rankedList, unrankedList).init();
}

interface ViewedParticipant {
    id: string;
    name: string;
}

class BallotPage {
    private readonly participantList = byId('participant-list', HTMLUListElement);
    private readonly participantToggle = byId('participant-toggle', HTMLButtonElement);

    private rankingSaveTimeout: ReturnType<typeof setTimeout> | undefined;
    private wsReconnectDelay = minReconnectDelay;
    private viewing: ViewedParticipant | null = null;
    private cachedParticipants: ParticipantEntry[] = [];

    constructor(
        private readonly ballotId: string,
        private readonly currentPublicId: string,
        private readonly rankedList: HTMLUListElement,
        private readonly unrankedList: HTMLUListElement,
    ) {}

    init(): void {
        this.cachedParticipants = this.readInitialParticipants();

        this.initAddItem();
        this.initDeleteItem();
        this.initDisplayName();
        this.initParticipantList();
        this.initSortable();
        this.initToggleOpen();
        this.initCopyAndShare();
        this.connectWebSocket();
    }

    // --- Items ---

    private initAddItem(): void {
        const form = document.querySelector<HTMLFormElement>('.add-item-form');
        const input = form?.querySelector<HTMLInputElement>('input[name="name"]');
        const submitBtn = form?.querySelector<HTMLButtonElement>('button[type="submit"]');
        if (!form || !input || !submitBtn) return;

        form.addEventListener('submit', (e) => {
            e.preventDefault();
            const name = input.value.trim();
            if (!name) return;

            submitBtn.disabled = true;

            fetch(form.action, {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: new URLSearchParams({ name }),
            }).then(async response => {
                if (!response.ok) {
                    throw new Error((await response.text()) || 'Failed to add item');
                }
                // Success: clear input. WebSocket broadcast will add the item to DOM.
                input.value = '';
            }).catch((err: unknown) => {
                showError(errorMessage(err));
            }).finally(() => {
                submitBtn.disabled = false;
                input.focus();
            });
        });
    }

    private initDeleteItem(): void {
        document.addEventListener('submit', (e) => {
            const form = e.target;
            if (!(form instanceof HTMLFormElement) || !form.classList.contains('delete-form')) return;
            e.preventDefault();

            const item = form.closest('.ranked-item');
            const name = item?.querySelector('.item-name')?.textContent?.trim() ?? 'this item';
            if (!confirm(`Are you sure you want to delete "${name}"?`)) {
                return;
            }

            const deleteBtn = form.querySelector<HTMLButtonElement>('.delete-btn');
            if (deleteBtn) deleteBtn.disabled = true;

            fetch(form.action, { method: 'POST' }).then(async response => {
                if (!response.ok) {
                    throw new Error((await response.text()) || 'Failed to delete item');
                }
                // Success: WebSocket broadcast will remove the item from DOM.
            }).catch((err: unknown) => {
                showError(errorMessage(err));
                if (deleteBtn) deleteBtn.disabled = false;
            });
        });
    }

    private itemElements(): HTMLLIElement[] {
        return [
            ...this.rankedList.querySelectorAll<HTMLLIElement>('.ranked-item'),
            ...this.unrankedList.querySelectorAll<HTMLLIElement>('.ranked-item'),
        ];
    }

    private updateItems(items: ItemEntry[], results: ResultEntry[]): void {
        const existing = this.itemElements();
        const existingIds = new Set(existing.map(itemId));
        const newItemsById = new Map(items.map(item => [item.id, item]));

        // Remove items that no longer exist; update attribution on the rest
        let rankedRemoved = false;
        for (const li of existing) {
            const item = newItemsById.get(itemId(li));
            if (!item) {
                if (li.parentElement === this.rankedList) {
                    rankedRemoved = true;
                }
                li.remove();
                continue;
            }

            const metaSpan = li.querySelector('.item-meta');
            if (!metaSpan) continue;
            let attrSpan = metaSpan.querySelector('.item-attribution');
            if (item.addedByName) {
                if (!attrSpan) {
                    attrSpan = document.createElement('span');
                    attrSpan.className = 'item-attribution';
                    metaSpan.insertBefore(attrSpan, metaSpan.firstChild);
                }
                attrSpan.textContent = item.addedByName;
            } else {
                attrSpan?.remove();
            }
        }

        // Add new items to unranked zone
        for (const item of items) {
            if (!existingIds.has(item.id)) {
                this.unrankedList.appendChild(this.renderItem(item));
            }
        }

        this.updateResultsList(results);
        this.updateEmptyStates();

        // If any ranked items were removed, save the current ranking
        if (rankedRemoved) {
            this.saveRanking();
        }
    }

    private renderItem(item: ItemEntry): HTMLLIElement {
        const li = document.createElement('li');
        li.dataset['itemId'] = String(item.id);
        li.className = 'ranked-item';
        li.innerHTML = `
            <span class="drag-handle">☰</span>
            <span class="item-name">${escapeHtml(item.name)}</span>
            <span class="item-meta"></span>
        `;

        const metaSpan = li.querySelector('.item-meta');
        if (!metaSpan) return li;

        if (item.addedByName) {
            const attrSpan = document.createElement('span');
            attrSpan.className = 'item-attribution';
            attrSpan.textContent = item.addedByName;
            metaSpan.appendChild(attrSpan);
        }

        // Add delete button if current user owns it
        if (item.addedByPublicId === this.currentPublicId) {
            const deleteForm = document.createElement('form');
            deleteForm.method = 'POST';
            deleteForm.action = `/ballot/${this.ballotId}/items/${item.id}/delete`;
            deleteForm.className = 'delete-form';
            deleteForm.innerHTML = '<button type="submit" class="delete-btn">Delete</button>';
            metaSpan.appendChild(deleteForm);
        }

        return li;
    }

    private updateEmptyStates(): void {
        setEmptyState(this.rankedList, 'Drag items here to rank them');
        setEmptyState(this.unrankedList, "Add items below - unranked items won't affect results");
    }

    // --- Ranking ---

    private initSortable(): void {
        const options: Sortable.Options = {
            group: 'ranking',
            animation: 150,
            ghostClass: 'sortable-ghost',
            delay: 200,
            delayOnTouchOnly: true,
            onEnd: () => this.saveRanking(),
        };

        Sortable.create(this.rankedList, options);
        Sortable.create(this.unrankedList, options);

        this.updateEmptyStates();
    }

    // Debounced to avoid flooding the server on rapid reorders.
    private saveRanking(): void {
        clearTimeout(this.rankingSaveTimeout);
        this.rankingSaveTimeout = setTimeout(() => {
            // Only save items from the ranked zone
            const body: SaveRankingsRequest = {
                order: [...this.rankedList.querySelectorAll<HTMLLIElement>('.ranked-item')].map(itemId),
            };

            this.updateEmptyStates();

            fetch(`/ballot/${this.ballotId}/rankings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            }).catch((err: unknown) => {
                console.error('Error saving ranking:', err);
                showError('Failed to save ranking. Please try again.');
            });
        }, 300);
    }

    private updateResultsList(results: ResultEntry[]): void {
        const list = byId('results-list', HTMLUListElement);
        if (!list) return;

        if (results.length === 0) {
            list.innerHTML = '<li>No results yet. Add items and rank them!</li>';
            return;
        }

        list.replaceChildren(...results.map(entry => {
            const li = document.createElement('li');
            li.textContent = `#${entry.rank}: ${entry.name}`;
            return li;
        }));
    }

    // --- WebSocket ---

    private connectWebSocket(): void {
        const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const ws = new WebSocket(`${protocol}//${location.host}/ballot/${this.ballotId}/ws`);

        ws.onmessage = (event: MessageEvent<string>) => {
            let msg: ServerMessage;
            try {
                msg = JSON.parse(event.data) as ServerMessage;
            } catch (err) {
                console.error('Error parsing message:', err);
                return;
            }
            this.handleMessage(msg);
        };

        ws.onopen = () => {
            console.log('WebSocket connected');
            this.wsReconnectDelay = minReconnectDelay;
        };

        ws.onclose = () => {
            console.log(`WebSocket closed, reconnecting in ${this.wsReconnectDelay}ms...`);
            setTimeout(() => this.connectWebSocket(), this.wsReconnectDelay);
            this.wsReconnectDelay = Math.min(this.wsReconnectDelay * 2, maxReconnectDelay);
        };

        ws.onerror = (err) => {
            console.error('WebSocket error:', err);
        };
    }

    private handleMessage(msg: ServerMessage): void {
        switch (msg.type) {
            case 'items_changed':
                this.updateItems(msg.items, msg.results);
                this.updateParticipantCount(msg.participantCount);
                this.refreshViewedParticipant();
                break;
            case 'results':
                this.updateResultsList(msg.results);
                this.updateParticipantCount(msg.participantCount);
                this.refreshViewedParticipant();
                break;
            case 'state_changed':
                updateToggleUI(msg.isOpen);
                break;
            case 'participants_changed':
                this.updateItems(msg.items, msg.results);
                this.updateParticipantCount(msg.participantCount);
                this.updateParticipantList(msg.participants);
                this.refreshViewedParticipant();
                break;
            default:
                assertNever(msg);
        }
    }

    // --- Participants ---

    private initDisplayName(): void {
        const input = byId('display-name', HTMLInputElement);
        if (!input) return;

        const save = (): void => {
            fetch(`/ballot/${this.ballotId}/name`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: new URLSearchParams({ name: input.value.trim() }),
            }).catch((err: unknown) => {
                console.error('Error saving name:', err);
            });
        };

        input.addEventListener('blur', save);
        input.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
                save();
                input.blur();
            }
        });
    }

    private initParticipantList(): void {
        const list = this.participantList;
        const toggle = this.participantToggle;
        if (!list || !toggle) return;

        const close = (): void => {
            this.restoreParticipantList();
            list.style.display = 'none';
            toggle.setAttribute('aria-expanded', 'false');
        };

        toggle.addEventListener('click', (e) => {
            e.stopPropagation();
            if (list.style.display !== 'none') {
                close();
            } else {
                list.style.display = 'block';
                toggle.setAttribute('aria-expanded', 'true');
            }
        });

        // Close participant list when clicking elsewhere
        document.addEventListener('click', (e) => {
            if (!list.contains(e.target as Node | null) && e.target !== toggle) {
                close();
            }
        });

        // Click a participant to view their rankings
        list.addEventListener('click', (e) => {
            const target = (e.target as Element | null)?.closest<HTMLElement>('.participant-name');
            const id = target?.dataset['publicId'];
            if (!target || !id) return;
            this.showParticipantRankings({ id, name: target.textContent ?? '' });
        });
    }

    // Reads the server-rendered participant list so it can be restored later.
    private readInitialParticipants(): ParticipantEntry[] {
        const list = this.participantList;
        if (!list) return [];

        const participants: ParticipantEntry[] = [];
        list.querySelectorAll<HTMLElement>('.participant-name').forEach(li => {
            participants.push({
                publicId: li.dataset['publicId'] ?? '',
                displayName: li.textContent?.trim() ?? '',
            });
        });

        const anonText = list.querySelector('.anonymous-label')?.textContent;
        if (anonText) {
            const count = parseInt(anonText.match(/(\d+)/)?.[1] ?? '1', 10);
            for (let i = 0; i < count; i++) {
                participants.push({ publicId: '', displayName: '' });
            }
        }

        return participants;
    }

    private updateParticipantCount(count: number): void {
        if (this.participantToggle) {
            this.participantToggle.textContent = count === 1 ? '1 participant' : `${count} participants`;
        }
    }

    private updateParticipantList(participants: ParticipantEntry[]): void {
        const list = this.participantList;
        if (!list) return;

        this.cachedParticipants = participants;

        // If currently viewing a specific participant's rankings, keep that view
        if (this.viewing) return;

        list.innerHTML = '';
        let anonymousCount = 0;

        for (const p of participants) {
            if (p.displayName) {
                const li = document.createElement('li');
                li.className = 'participant-name';
                li.dataset['publicId'] = p.publicId;
                li.textContent = p.displayName;
                list.appendChild(li);
            } else {
                anonymousCount++;
            }
        }

        if (anonymousCount > 0) {
            const li = document.createElement('li');
            li.className = 'anonymous-label';
            li.textContent = anonymousCount === 1 ? '1 anonymous' : `${anonymousCount} anonymous`;
            list.appendChild(li);
        }
    }

    private restoreParticipantList(): void {
        this.viewing = null;
        this.updateParticipantList(this.cachedParticipants);
    }

    private refreshViewedParticipant(): void {
        if (this.viewing) {
            this.showParticipantRankings(this.viewing);
        }
    }

    private showParticipantRankings(participant: ViewedParticipant): void {
        const list = this.participantList;
        if (!list) return;

        this.viewing = participant;

        fetch(`/ballot/${this.ballotId}/rankings/${encodeURIComponent(participant.id)}`)
            .then(response => {
                if (!response.ok) throw new Error('Failed to fetch rankings');
                return response.json() as Promise<RankedItem[]>;
            })
            .then(rankings => {
                list.innerHTML = '';

                // Header with back arrow button on left
                const header = document.createElement('div');
                header.className = 'participant-rankings-header';

                const backBtn = document.createElement('button');
                backBtn.className = 'participant-rankings-back';
                backBtn.textContent = '←';
                backBtn.addEventListener('click', (e) => {
                    e.stopPropagation();
                    this.restoreParticipantList();
                });

                const nameSpan = document.createElement('span');
                nameSpan.className = 'participant-rankings-name';
                nameSpan.textContent = participant.name;

                header.append(backBtn, nameSpan);
                list.appendChild(header);

                if (rankings.length === 0) {
                    const empty = document.createElement('div');
                    empty.className = 'participant-rankings-empty';
                    empty.textContent = 'No rankings yet';
                    list.appendChild(empty);
                    return;
                }

                rankings.forEach((item, index) => {
                    const li = document.createElement('li');
                    li.className = 'participant-rankings-item';
                    li.textContent = `${index + 1}. ${item.name}`;
                    list.appendChild(li);
                });
            })
            .catch(() => {
                showError('Failed to load rankings');
            });
    }

    // --- Ballot access and sharing ---

    private initToggleOpen(): void {
        const toggleBtn = byId('toggle-open-btn', HTMLButtonElement);
        if (!toggleBtn) return;

        toggleBtn.addEventListener('click', () => {
            fetch(`/ballot/${this.ballotId}/toggle`, { method: 'POST' })
                .then(response => {
                    if (!response.ok) {
                        console.error('Error toggling ballot state');
                        return null;
                    }
                    return response.json() as Promise<ToggleResponse>;
                })
                .then(data => {
                    if (data) {
                        updateToggleUI(data.isOpen);
                    }
                })
                .catch((err: unknown) => {
                    console.error('Error toggling ballot state:', err);
                    showError('Failed to toggle ballot state. Please try again.');
                });
        });
    }

    private initCopyAndShare(): void {
        const ballotUrl = `${location.origin}/ballot/${this.ballotId}`;

        const copyUrlBtn = byId('copy-url-btn', HTMLButtonElement);
        copyUrlBtn?.addEventListener('click', () => {
            navigator.clipboard.writeText(ballotUrl).then(() => {
                const originalText = copyUrlBtn.textContent;
                copyUrlBtn.textContent = '✓ Copied!';
                setTimeout(() => {
                    copyUrlBtn.textContent = originalText;
                }, 1500);
            }).catch((err: unknown) => {
                console.error('Failed to copy URL:', err);
                showError('Failed to copy URL. Please try again.');
            });
        });

        const shareUrlBtn = byId('share-url-btn', HTMLButtonElement);
        if (!shareUrlBtn) return;

        // Hide share button if the Web Share API isn't available
        if (!('share' in navigator)) {
            shareUrlBtn.style.display = 'none';
            return;
        }

        shareUrlBtn.addEventListener('click', async () => {
            const ballotTitle = document.querySelector('.ballot-header h1')?.textContent || 'Ballot';
            try {
                await navigator.share({
                    title: ballotTitle,
                    text: `Vote on: ${ballotTitle}`,
                    url: ballotUrl,
                });
            } catch (err) {
                // User cancelled or error occurred
                if (!(err instanceof DOMException && err.name === 'AbortError')) {
                    console.error('Failed to share:', err);
                }
            }
        });
    }
}

function itemId(li: HTMLElement): number {
    return parseInt(li.dataset['itemId'] ?? '', 10);
}

// Shows a placeholder in list when it has no items.
function setEmptyState(list: HTMLUListElement, text: string): void {
    list.querySelectorAll('.empty-state').forEach(el => el.remove());
    if (list.querySelector('.ranked-item')) return;

    const emptyLi = document.createElement('li');
    emptyLi.className = 'empty-state';
    emptyLi.textContent = text;
    list.appendChild(emptyLi);
}

function updateToggleUI(isOpen: boolean): void {
    const btn = byId('toggle-open-btn', HTMLButtonElement);
    if (!btn) return;

    btn.textContent = isOpen ? 'Open' : 'Closed';
    btn.classList.toggle('toggle-open', isOpen);
    btn.classList.toggle('toggle-closed', !isOpen);
}
