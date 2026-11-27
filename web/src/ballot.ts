import qrcode from 'qrcode-generator';
import Sortable from 'sortablejs';

import type { ServerMessage } from './messages';
import type {
    ItemEntry,
    LinkResponse,
    ParticipantEntry,
    RankedItem,
    ResultEntry,
    SaveRankingsRequest,
    ToggleResponse,
} from './protocol.gen';
import { CloseBallotDeleted, CloseNotParticipant } from './protocol.gen';
import { assertNever, byId, errorMessage, escapeHtml, showError } from './ui';

const minReconnectDelay = 1000;
const maxReconnectDelay = 30000;

// Query parameter the page reloads with after losing participant access.
const notParticipantNotice = 'notice=not-participant';

// Wires up the ballot page. root carries the ballot ID, the requester's role
// (viewer, participant, or creator), and their participant ID.
export function initBallot(root: HTMLElement): void {
    const ballotId = root.dataset['ballotId'];
    if (!ballotId) {
        console.error('Ballot page is missing its ballot ID');
        return;
    }

    // The notice is shown once; keep it out of reloads and shared URLs.
    if (location.search.includes(notParticipantNotice)) {
        history.replaceState(null, '', location.pathname + location.hash);
    }

    if (root.dataset['role'] === 'viewer') {
        new ViewerPage(ballotId).init();
        return;
    }

    const participantId = root.dataset['currentParticipantId'];
    const rankedList = byId('ranked-list', HTMLUListElement);
    const unrankedList = byId('unranked-list', HTMLUListElement);
    if (!participantId || !rankedList || !unrankedList) {
        console.error('Ballot page is missing required elements');
        return;
    }
    const isCreator = root.dataset['role'] === 'creator';
    new ParticipantPage(ballotId, participantId, isCreator, rankedList, unrankedList).init();
}

// LiveBallot is what every visitor sees update in real time: results, the
// participant count and list, the open/closed state, and the item list
// (rendered by each subclass).
abstract class LiveBallot {
    protected readonly participantList = byId('participant-list', HTMLUListElement);
    protected readonly participantToggle = byId('participant-toggle', HTMLButtonElement);
    protected cachedParticipants: ParticipantEntry[] = [];
    protected rankingsPrivate = document.getElementById('ballot')?.dataset['rankingsPrivate'] === 'true';
    private wsReconnectDelay = minReconnectDelay;

    constructor(protected readonly ballotId: string) {}

    init(): void {
        this.cachedParticipants = this.readInitialParticipants();
        this.initParticipantList();
        this.updateParticipantList(this.cachedParticipants); // adds the visibility row
        this.initShare();
        this.connectWebSocket();
    }

    protected abstract updateItems(items: ItemEntry[], results: ResultEntry[]): void;
    protected abstract stateChanged(isOpen: boolean, title: string): void;

    // Called when the server ends this browser's participation.
    protected accessLost(): void {
        location.replace(`/ballot/${this.ballotId}?${notParticipantNotice}`);
    }

    private deleted = false;

    // Replaces the page once the creator deletes the ballot.
    private ballotDeleted(): void {
        if (this.deleted) return;
        this.deleted = true;
        const container = document.querySelector('.container');
        if (container) {
            container.innerHTML = `
                <h1>Ballot deleted</h1>
                <p>The creator deleted this ballot.</p>
                <p><a href="/">Create a new ballot</a></p>
            `;
        }
    }

    // Called after any message that may change a participant's rankings.
    protected rankingsMayHaveChanged(): void {}

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
            this.wsReconnectDelay = minReconnectDelay;
        };

        ws.onclose = (event) => {
            if (event.code === CloseBallotDeleted) {
                this.ballotDeleted();
                return;
            }
            if (event.code === CloseNotParticipant) {
                this.accessLost();
                return;
            }
            if (this.deleted) return;
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
                this.rankingsMayHaveChanged();
                break;
            case 'results':
                this.updateItems(msg.items, msg.results);
                this.updateParticipantCount(msg.participantCount);
                this.rankingsMayHaveChanged();
                break;
            case 'state_changed':
                if (msg.rankingsPrivate !== this.rankingsPrivate) {
                    this.rankingsPrivate = msg.rankingsPrivate;
                    this.updateVisibility();
                }
                this.stateChanged(msg.isOpen, msg.title);
                break;
            case 'participants_changed':
                this.updateItems(msg.items, msg.results);
                this.updateParticipantCount(msg.participantCount);
                this.updateParticipantList(msg.participants);
                this.rankingsMayHaveChanged();
                break;
            case 'ballot_deleted':
                this.ballotDeleted();
                break;
            default:
                assertNever(msg);
        }
    }

    protected updateResultsList(results: ResultEntry[]): void {
        const list = byId('results-list', HTMLUListElement);
        if (!list) return;

        if (results.length === 0) {
            list.innerHTML = '<li>No rankings yet. Add items and rank them to see results!</li>';
            return;
        }

        // Reorder the same elements so each can glide from its old spot.
        const existing = [...list.querySelectorAll<HTMLLIElement>('li[data-item-id]')];
        const reuse = new Map(existing.map(li => [li.dataset['itemId'], li]));
        const tops = captureTops(existing);

        const added: HTMLLIElement[] = [];
        list.replaceChildren(...results.map(entry => {
            let li = reuse.get(String(entry.itemId));
            if (!li) {
                li = document.createElement('li');
                li.dataset['itemId'] = String(entry.itemId);
                added.push(li);
            }
            li.textContent = `#${entry.rank}: ${entry.name}`;
            return li;
        }));

        animateFrom(tops);
        if (existing.length > 0 && !prefersReducedMotion()) {
            for (const li of added) li.animate([{ opacity: 0 }, { opacity: 1 }], moveTiming);
        }
    }

    // --- Sharing ---

    // The Share dialog: a link as a QR code to scan, and as text to tap and
    // copy. Share shows the public ballot URL; "Add device" shows
    // the participant's private join link (see showQr).
    private initShare(): void {
        const ballotUrl = `${location.origin}/ballot/${this.ballotId}`;
        byId('share-btn', HTMLButtonElement)?.addEventListener('click', () => {
            this.showQr(ballotUrl, 'Scan to open this ballot');
        });

        const dialog = byId('qr-dialog', HTMLDialogElement);
        const link = byId('qr-url', HTMLAnchorElement);
        const hint = byId('qr-hint', HTMLElement);
        if (!dialog || !link || !hint) return;

        let hintTimeout: ReturnType<typeof setTimeout> | undefined;
        link.addEventListener('click', (e) => {
            e.preventDefault();
            clearTimeout(hintTimeout);
            navigator.clipboard.writeText(link.href)
                .then(() => { hint.textContent = '✓ Copied'; })
                .catch(() => { hint.textContent = "Couldn't copy; press and hold the link instead."; });
            hintTimeout = setTimeout(() => { hint.textContent = qrHint; }, 2000);
        });

        // Clicks on the backdrop target the dialog itself, as do clicks on its
        // padding, so close only when the click is outside its box.
        dialog.addEventListener('click', (e) => {
            const r = dialog.getBoundingClientRect();
            const outside = e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
            if (e.target === dialog && outside) dialog.close();
        });
    }

    // Shows url in the Share dialog with its own heading, and an optional
    // warning note.
    protected showQr(url: string, title: string, note = ''): void {
        const dialog = byId('qr-dialog', HTMLDialogElement);
        const code = byId('qr-code', HTMLElement);
        const link = byId('qr-url', HTMLAnchorElement);
        const heading = byId('qr-title', HTMLElement);
        const hint = byId('qr-hint', HTMLElement);
        const noteEl = byId('qr-note', HTMLElement);
        if (!dialog || !code || !link || !heading || !hint || !noteEl) return;

        if (code.dataset['url'] !== url) {
            const qr = qrcode(0, 'M');
            qr.addData(url);
            qr.make();
            code.innerHTML = qr.createSvgTag({ scalable: true, margin: 4 });
            code.dataset['url'] = url;
        }
        heading.textContent = title;
        link.href = url;
        link.textContent = url;
        hint.textContent = qrHint;
        noteEl.textContent = note;
        noteEl.hidden = !note;
        dialog.showModal();
    }

    // --- Participant list ---

    private initParticipantList(): void {
        const list = this.participantList;
        const toggle = this.participantToggle;
        if (!list || !toggle) return;

        const close = (): void => {
            this.participantListClosed();
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
    }

    protected participantListClosed(): void {}

    // Reads the server-rendered participant list so it can be restored later.
    private readInitialParticipants(): ParticipantEntry[] {
        const list = this.participantList;
        if (!list) return [];

        const participants: ParticipantEntry[] = [];
        list.querySelectorAll<HTMLElement>('.participant-name').forEach(li => {
            participants.push({
                participantId: li.dataset['participantId'] ?? '',
                displayName: li.dataset['name'] ?? li.textContent?.trim() ?? '',
            });
        });

        const anonText = list.querySelector('.anonymous-label')?.textContent;
        if (anonText) {
            const count = parseInt(anonText.match(/(\d+)/)?.[1] ?? '1', 10);
            for (let i = 0; i < count; i++) {
                participants.push({ participantId: '', displayName: '' });
            }
        }

        return participants;
    }

    private updateParticipantCount(count: number): void {
        const text = this.participantToggle?.querySelector('.participant-count-text');
        if (text) {
            text.textContent = count === 1 ? '1 participant' : `${count} participants`;
        }
    }

    // --- Rankings visibility ---

    // Shows the visibility icon on the participant count and updates the
    // list's visibility row.
    private updateVisibility(): void {
        const toggle = this.participantToggle;
        if (toggle) {
            toggle.title = visibilityText(this.rankingsPrivate);
            toggle.querySelector('.vis-icon use')?.setAttribute('href', visibilityIcon(this.rankingsPrivate));
            const sr = toggle.querySelector('.vis-text');
            if (sr) sr.textContent = this.rankingsPrivate ? '(rankings private)' : '(rankings visible)';
        }
        const row = this.participantList?.querySelector<HTMLLIElement>('.visibility-row');
        if (row) this.syncVisibilityRow(row);
    }

    // Brings an existing visibility row up to date.
    protected syncVisibilityRow(row: HTMLLIElement): void {
        row.replaceWith(this.visibilityRow());
    }

    // The first row of the participant list: whether rankings are visible.
    protected visibilityRow(): HTMLLIElement {
        const li = document.createElement('li');
        li.className = 'visibility-row';
        li.title = visibilityText(this.rankingsPrivate);
        li.append(iconSvg(visibilityIcon(this.rankingsPrivate)), this.rankingsPrivate ? 'Rankings are private' : 'Rankings are visible');
        return li;
    }

    protected updateParticipantList(participants: ParticipantEntry[]): void {
        const list = this.participantList;
        if (!list) return;

        this.cachedParticipants = participants;
        if (!this.canRenderParticipantList()) return;

        list.replaceChildren(this.visibilityRow());
        let anonymousCount = 0;

        for (const p of participants) {
            if (p.displayName) {
                const li = document.createElement('li');
                li.className = 'participant-name';
                li.dataset['participantId'] = p.participantId;
                li.dataset['name'] = p.displayName;
                const label = document.createElement('span');
                label.className = 'participant-label';
                label.textContent = p.displayName;
                li.appendChild(label);
                this.decorateParticipant(li, p);
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

    // False while the list is showing something else (a participant's rankings).
    protected canRenderParticipantList(): boolean {
        return true;
    }

    // Adds per-participant controls to a list entry.
    protected decorateParticipant(_li: HTMLLIElement, _p: ParticipantEntry): void {}
}

// ViewerPage is the ballot as seen by someone who hasn't joined: read-only
// items, live results, and a Join form while the ballot is open.
class ViewerPage extends LiveBallot {
    private readonly itemList = byId('viewer-items', HTMLUListElement);

    protected updateItems(items: ItemEntry[], results: ResultEntry[]): void {
        this.updateResultsList(results);
        const list = this.itemList;
        if (!list) return;

        if (items.length === 0) {
            list.innerHTML = '<li class="empty-state">No items yet</li>';
            return;
        }
        list.replaceChildren(...items.map(item => {
            const li = document.createElement('li');
            li.className = 'ranked-item';
            li.innerHTML = `
                <span class="item-name">${escapeHtml(item.name)}</span>
                <span class="item-meta"></span>
            `;
            if (item.addedByName) {
                const attr = document.createElement('span');
                attr.className = 'item-attribution';
                attr.textContent = item.addedByName;
                li.querySelector('.item-meta')?.appendChild(attr);
            }
            return li;
        }));
    }

    // Opening or closing changes whether the Join form is offered.
    protected stateChanged(isOpen: boolean, title: string): void {
        if (isOpen !== isBallotOpen()) {
            location.reload();
            return;
        }
        updateTitle(title);
    }
}

interface ViewedParticipant {
    id: string;
    name: string;
}

// ParticipantPage is the ballot for someone who has joined: they can add and
// delete items, drag to rank, rename themselves, and view others' rankings.
class ParticipantPage extends LiveBallot {
    private rankingSaveTimeout: ReturnType<typeof setTimeout> | undefined;
    private viewing: ViewedParticipant | null = null;

    constructor(
        ballotId: string,
        private readonly participantId: string,
        private readonly isCreator: boolean,
        private readonly rankedList: HTMLUListElement,
        private readonly unrankedList: HTMLUListElement,
    ) {
        super(ballotId);
    }

    override init(): void {
        super.init();
        this.initAddItem();
        this.initDeleteItem();
        this.initDisplayName();
        this.initRankingsView();
        this.initSortable();
        this.initMoveButtons();
        this.initDeviceLink();
        if (this.isCreator) {
            this.initToggleOpen();
            this.initCreatorMenu();
        }
    }

    // fetch for participant-only endpoints. A 401 means this browser is no
    // longer a participant (removed, link reset, or cookie lost), so the
    // page reloads as a viewer with a notice; the returned promise never
    // settles in that case.
    private participantFetch(input: string, init?: RequestInit): Promise<Response> {
        return fetch(input, init).then(response => {
            if (response.status === 401) {
                this.accessLost();
                return new Promise<Response>(() => {});
            }
            return response;
        });
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

            this.participantFetch(form.action, {
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
            if (!confirm(`Are you sure you want to remove "${name}"?`)) {
                return;
            }

            const deleteBtn = form.querySelector<HTMLButtonElement>('.delete-btn');
            if (deleteBtn) deleteBtn.disabled = true;

            this.participantFetch(form.action, { method: 'POST' }).then(async response => {
                if (!response.ok) {
                    throw new Error((await response.text()) || 'Failed to remove item');
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

    protected updateItems(items: ItemEntry[], results: ResultEntry[]): void {
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
            this.syncDeleteForm(li, item);
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
            <span class="item-meta">
                <span class="item-actions"><button type="button" class="move-btn"></button></span>
            </span>
        `;

        const attribution = item.addedByName;
        if (attribution) {
            const attrSpan = document.createElement('span');
            attrSpan.className = 'item-attribution';
            attrSpan.textContent = attribution;
            li.querySelector('.item-meta')?.prepend(attrSpan);
        }
        syncMoveButton(li, false);
        this.syncDeleteForm(li, item);

        return li;
    }

    // The creator can delete anything; others their own items until someone
    // else ranks them. The server enforces the same rule.
    private canDelete(item: ItemEntry): boolean {
        return this.isCreator || (item.addedBy === this.participantId && !item.rankedByOthers);
    }

    // Adds or removes an item's Delete button to match canDelete.
    private syncDeleteForm(li: HTMLLIElement, item: ItemEntry): void {
        const actions = li.querySelector('.item-actions');
        if (!actions) return;
        const existing = actions.querySelector('.delete-form');
        if (!this.canDelete(item)) {
            existing?.remove();
            return;
        }
        if (existing) return;

        const deleteForm = document.createElement('form');
        deleteForm.method = 'POST';
        deleteForm.action = `/ballot/${this.ballotId}/items/${item.id}/delete`;
        deleteForm.className = 'delete-form';
        deleteForm.innerHTML = '<button type="submit" class="delete-btn" aria-label="Remove" title="Remove">✕</button>';
        actions.appendChild(deleteForm);
    }

    private updateEmptyStates(): void {
        setEmptyState(this.rankedList, 'Drag items here or tap + to rank them');
        setEmptyState(this.unrankedList, "Add items below — unranked items don't affect results");
    }

    // --- Ranking ---

    private initSortable(): void {
        const options: Sortable.Options = {
            group: 'ranking',
            animation: 150,
            ghostClass: 'sortable-ghost',
            delay: 200,
            delayOnTouchOnly: true,
            onEnd: (e) => {
                syncMoveButton(e.item, e.to === this.rankedList);
                this.saveRanking();
            },
        };

        Sortable.create(this.rankedList, options);
        Sortable.create(this.unrankedList, options);

        this.updateEmptyStates();
    }

    // + appends an unranked item to the ranked list; − returns a ranked item
    // to the top of the unranked list, so long ballots don't need dragging.
    private initMoveButtons(): void {
        for (const list of [this.rankedList, this.unrankedList]) {
            list.addEventListener('click', (e) => {
                const li = (e.target as Element | null)?.closest('.move-btn')?.closest<HTMLLIElement>('.ranked-item');
                if (!li) return;
                const toRanked = li.parentElement !== this.rankedList;
                const tops = captureTops(this.itemElements());
                if (toRanked) {
                    this.rankedList.appendChild(li);
                } else {
                    this.unrankedList.prepend(li);
                }
                syncMoveButton(li, toRanked);
                this.updateEmptyStates();

                // Lift the moved item above the neighbours it passes.
                const moving = animateFrom(tops).get(li);
                if (moving) {
                    li.classList.add('moving');
                    const done = (): void => li.classList.remove('moving');
                    moving.addEventListener('finish', done);
                    moving.addEventListener('cancel', done);
                }
                this.saveRanking();
            });
        }
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

            this.participantFetch(`/ballot/${this.ballotId}/rankings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            }).then(async response => {
                // A 400 can happen when an item is deleted mid-save; the
                // resulting items_changed message triggers a corrected save.
                if (!response.ok) {
                    console.error('Ranking not saved:', response.status, await response.text());
                }
            }).catch((err: unknown) => {
                console.error('Error saving ranking:', err);
                showError('Failed to save ranking');
            });
        }, 300);
    }

    // --- Self ---

    private initDisplayName(): void {
        const input = byId('display-name', HTMLInputElement);
        if (!input) return;

        // Each save broadcasts to every participant, so skip unchanged names
        // (Enter saves and then blurs, which would otherwise save twice).
        let saved: string | null = input.value.trim();

        const save = (): void => {
            const name = input.value.trim();
            if (name === saved) return;
            saved = name;

            this.participantFetch(`/ballot/${this.ballotId}/name`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: new URLSearchParams({ name }),
            }).then(response => {
                if (!response.ok) throw new Error(`HTTP ${response.status}`);
            }).catch((err: unknown) => {
                saved = null; // retry on next blur
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

    // Personal link: signs another browser in as this participant.
    private initDeviceLink(): void {
        const button = byId('device-link-btn', HTMLButtonElement);
        const link = button?.dataset['link'];
        if (button && link) {
            button.addEventListener('click', () => {
                this.showQr(location.origin + link, 'Scan to use this ballot on another device',
                    "This link signs in as you. Don't share it with others.");
            });
        }
    }

    // --- Other participants' rankings ---

    private initRankingsView(): void {
        this.participantList?.addEventListener('click', (e) => {
            const clicked = e.target as Element | null;
            const target = clicked?.closest<HTMLElement>('.participant-name');
            const id = target?.dataset['participantId'];
            if (!target || !id) return;
            const name = target.dataset['name'] ?? '';

            const action = clicked?.closest<HTMLButtonElement>('.participant-action');
            if (action) {
                e.stopPropagation();
                this.participantAction(action, id, name);
                return;
            }
            if (!this.rankingsPrivate) {
                this.showParticipantRankings({ id, name });
            }
        });
    }

    protected override canRenderParticipantList(): boolean {
        return this.viewing === null;
    }

    protected override participantListClosed(): void {
        this.restoreParticipantList();
    }

    protected override rankingsMayHaveChanged(): void {
        if (this.viewing) {
            this.showParticipantRankings(this.viewing);
        }
    }

    private restoreParticipantList(): void {
        this.viewing = null;
        this.updateParticipantList(this.cachedParticipants);
    }

    private showParticipantRankings(participant: ViewedParticipant): void {
        const list = this.participantList;
        if (!list) return;

        this.viewing = participant;

        this.participantFetch(`/ballot/${this.ballotId}/rankings/${encodeURIComponent(participant.id)}`)
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

    // --- Ballot settings ---

    protected stateChanged(isOpen: boolean, title: string): void {
        updateToggleUI(isOpen);
        updateTitle(title);
        if (this.rankingsPrivate && this.viewing) {
            this.restoreParticipantList();
        }
    }

    // --- Creator controls ---

    // Replaces the title with an input while the pencil is used. Enter or
    // leaving the field saves; Escape cancels.
    private initTitleEdit(): void {
        const button = byId('edit-title-btn', HTMLButtonElement);
        const span = byId('ballot-title', HTMLElement);
        if (!button || !span) return;

        button.addEventListener('click', () => {
            const original = span.dataset['title'] ?? '';
            const input = document.createElement('input');
            input.type = 'text';
            input.className = 'title-input';
            input.maxLength = 200;
            input.placeholder = 'Ballot';
            input.setAttribute('aria-label', 'Ballot title');
            input.value = original;

            let done = false;
            const finish = (save: boolean): void => {
                if (done) return;
                done = true;
                const title = input.value.trim();
                input.remove();
                span.hidden = false;
                button.hidden = false;
                button.focus();
                if (save && title !== original) {
                    updateTitle(title);
                    this.creatorPost('title', 'Failed to update title', new URLSearchParams({ title }));
                }
            };

            input.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') {
                    e.preventDefault();
                    finish(true);
                } else if (e.key === 'Escape') {
                    finish(false);
                }
            });
            input.addEventListener('blur', () => finish(true));

            span.hidden = true;
            button.hidden = true;
            span.after(input);
            input.focus();
            input.select();
        });
    }

    // The creator's visibility row is a switch whose knob slides between the
    // eye (visible) and incognito (private).
    protected override visibilityRow(): HTMLLIElement {
        if (!this.isCreator) return super.visibilityRow();
        const li = document.createElement('li');
        li.className = 'visibility-row';

        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'vis-switch';
        button.setAttribute('role', 'switch');
        button.setAttribute('aria-label', 'Private rankings');

        const track = document.createElement('span');
        track.className = 'vis-switch-track';
        const knob = document.createElement('span');
        knob.className = 'vis-switch-knob';
        const eye = iconSvg(visibilityIcon(false));
        eye.classList.add('knob-visible');
        const incognito = iconSvg(visibilityIcon(true));
        incognito.classList.add('knob-private');
        knob.append(eye, incognito);
        track.appendChild(knob);

        const label = document.createElement('span');
        label.className = 'vis-switch-label';
        label.setAttribute('aria-hidden', 'true'); // the switch's aria-label says it
        label.textContent = 'Private rankings';

        button.append(track, label);
        li.appendChild(button);
        setVisibilitySwitch(button, this.rankingsPrivate);
        return li;
    }

    // Flips the switch in place so its CSS transitions animate.
    protected override syncVisibilityRow(row: HTMLLIElement): void {
        const button = row.querySelector<HTMLButtonElement>('.vis-switch');
        if (button) {
            setVisibilitySwitch(button, this.rankingsPrivate);
        } else {
            super.syncVisibilityRow(row);
        }
    }

    private initCreatorMenu(): void {
        this.initTitleEdit();

        // The switch moves when the server's state_changed message arrives.
        this.participantList?.addEventListener('click', (e) => {
            if (!(e.target as Element | null)?.closest('.vis-switch')) return;
            const makePrivate = !this.rankingsPrivate;
            if (makePrivate && !confirm('Make rankings private? Once anyone else ranks, they can\'t be made visible again.')) {
                return;
            }
            this.creatorPost('visibility', 'Failed to change rankings visibility', new URLSearchParams({ private: makePrivate ? '1' : '0' }));
        });

        byId('delete-ballot-btn', HTMLButtonElement)?.addEventListener('click', () => {
            const title = byId('ballot-title', HTMLElement)?.dataset['title'];
            const ballot = title ? `"${title}"` : 'this ballot';
            if (confirm(`Delete ${ballot} for everyone? This cannot be undone.`)) {
                this.creatorPost('delete', 'Failed to delete ballot');
            }
        });
    }

    protected override decorateParticipant(li: HTMLLIElement, p: ParticipantEntry): void {
        if (!this.isCreator || p.participantId === this.participantId) return;
        for (const [action, label] of [['link', 'Copy link'], ['reset', 'Reset link'], ['remove', 'Remove']] as const) {
            const button = document.createElement('button');
            button.type = 'button';
            button.className = action === 'remove' ? 'participant-action danger' : 'participant-action';
            button.dataset['action'] = action;
            button.textContent = label;
            li.appendChild(button);
        }
    }

    private participantAction(button: HTMLButtonElement, id: string, name: string): void {
        const base = `/ballot/${this.ballotId}/participants/${encodeURIComponent(id)}`;
        switch (button.dataset['action']) {
            case 'link':
                this.fetchLink(base + '/link', { method: 'GET' }, button);
                break;
            case 'reset':
                if (confirm(`Reset ${name}'s link? Their current link and signed-in devices stop working; the new link is copied for you to send them.`)) {
                    this.fetchLink(base + '/reset', { method: 'POST' }, button);
                }
                break;
            case 'remove':
                this.confirmRemove(base + '/remove', name);
                break;
        }
    }

    // Fetches a participant's join link and copies it.
    private fetchLink(url: string, init: RequestInit, button: HTMLButtonElement): void {
        this.participantFetch(url, init)
            .then(async response => {
                if (!response.ok) throw new Error((await response.text()) || 'Failed to get link');
                return response.json() as Promise<LinkResponse>;
            })
            .then(({ link }) => copyWithFeedback(button, location.origin + link))
            .catch((err: unknown) => showError(errorMessage(err)));
    }

    private confirmRemove(url: string, name: string): void {
        const dialog = byId('remove-dialog', HTMLDialogElement);
        const form = dialog?.querySelector('form');
        if (!dialog || !form) return;

        const nameEl = byId('remove-name', HTMLElement);
        if (nameEl) nameEl.textContent = name;
        const note = byId('remove-note', HTMLElement);
        if (note) {
            note.textContent = isBallotOpen()
                ? 'The ballot is open, so they could rejoin as a new participant. Close it first to prevent that.'
                : "The ballot is closed, so they can't rejoin.";
        }
        form.reset();

        dialog.addEventListener('close', () => {
            if (dialog.returnValue !== 'remove') return;
            const body = new URLSearchParams();
            for (const box of form.querySelectorAll<HTMLInputElement>('input[type=checkbox]:checked')) {
                body.set(box.name, box.value);
            }
            this.creatorPost('remove', 'Failed to remove participant', body, url);
        }, { once: true });
        dialog.showModal();
    }

    // POSTs a creator action; results arrive over the WebSocket.
    private creatorPost(action: string, failure: string, body?: URLSearchParams, url = `/ballot/${this.ballotId}/${action}`): void {
        this.participantFetch(url, { method: 'POST', ...(body ? { body } : {}) })
            .then(async response => {
                if (!response.ok) throw new Error((await response.text()) || failure);
            })
            .catch((err: unknown) => showError(errorMessage(err)));
    }

    // The creator's padlock opens or closes the ballot to new participants.
    private initToggleOpen(): void {
        const toggleBtn = byId('ballot-status', HTMLButtonElement);
        if (!toggleBtn) return;

        toggleBtn.addEventListener('click', () => {
            this.participantFetch(`/ballot/${this.ballotId}/toggle`, { method: 'POST' })
                .then(async response => {
                    if (!response.ok) {
                        throw new Error((await response.text()) || 'Failed to open or close ballot');
                    }
                    return response.json() as Promise<ToggleResponse>;
                })
                .then(data => updateToggleUI(data.isOpen))
                .catch((err: unknown) => {
                    showError(errorMessage(err));
                });
        });
    }
}

function itemId(li: HTMLElement): number {
    return parseInt(li.dataset['itemId'] ?? '', 10);
}

const qrHint = 'Tap the link to copy it.';

// FLIP animation: capture where elements are, change the DOM, then let
// animateFrom glide each one from its old position to its new one.
const moveTiming: KeyframeAnimationOptions = { duration: 600, easing: 'cubic-bezier(0.65, 0, 0.35, 1)' };

function prefersReducedMotion(): boolean {
    return matchMedia('(prefers-reduced-motion: reduce)').matches;
}

// Records each element's on-screen top, including any in-flight
// animation, then cancels those animations.
function captureTops(els: Iterable<HTMLElement>): Map<HTMLElement, number> {
    const tops = new Map<HTMLElement, number>();
    for (const el of els) tops.set(el, el.getBoundingClientRect().top);
    for (const el of tops.keys()) el.getAnimations().forEach(a => a.cancel());
    return tops;
}

// Animates each still-present element from its captured top to where it is
// now, returning the animations started.
function animateFrom(tops: Map<HTMLElement, number>): Map<HTMLElement, Animation> {
    const started = new Map<HTMLElement, Animation>();
    if (prefersReducedMotion()) return started;
    for (const [el, top] of tops) {
        const delta = el.isConnected ? top - el.getBoundingClientRect().top : 0;
        if (delta !== 0) {
            started.set(el, el.animate([{ transform: `translateY(${delta}px)` }, { transform: 'none' }], moveTiming));
        }
    }
    return started;
}

// Labels an item's move button for the zone it's in.
function syncMoveButton(li: HTMLElement, ranked: boolean): void {
    const button = li.querySelector<HTMLButtonElement>('.move-btn');
    if (!button) return;
    const label = ranked ? 'Unrank' : 'Add to ranked';
    button.textContent = ranked ? '−' : '+';
    button.setAttribute('aria-label', label);
    button.title = label;
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

// Locks or unlocks the padlock; its CSS transition animates the shackle.
function updateToggleUI(isOpen: boolean): void {
    const lock = byId('ballot-status', HTMLElement);
    if (!lock) return;
    lock.classList.toggle('unlocked', isOpen);
    lock.classList.toggle('locked', !isOpen);
    let label = isOpen ? 'Open to new participants' : 'Closed to new participants';
    if (lock instanceof HTMLButtonElement) {
        label += isOpen ? '. Click to close.' : '. Click to reopen.';
    }
    lock.setAttribute('aria-label', label);
    lock.title = label;
}

function visibilityText(rankingsPrivate: boolean): string {
    return rankingsPrivate
        ? 'Rankings are private; only the combined results are shown'
        : 'Rankings are visible to all participants';
}

// Eye for visible rankings, incognito (hat and glasses) for private.
function visibilityIcon(rankingsPrivate: boolean): string {
    return rankingsPrivate ? '#icon-incognito' : '#icon-eye';
}

function setVisibilitySwitch(button: HTMLButtonElement, rankingsPrivate: boolean): void {
    button.setAttribute('aria-checked', String(rankingsPrivate));
    button.title = visibilityText(rankingsPrivate);
}

// An inline icon referencing a symbol in the page's SVG sprite.
function iconSvg(href: string): SVGSVGElement {
    const ns = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('class', 'vis-icon');
    svg.setAttribute('aria-hidden', 'true');
    const use = document.createElementNS(ns, 'use');
    use.setAttribute('href', href);
    svg.appendChild(use);
    return svg;
}

function isBallotOpen(): boolean {
    return byId('ballot-status', HTMLElement)?.classList.contains('unlocked') ?? false;
}

function updateTitle(title: string): void {
    const shown = title || 'Ballot';
    const span = byId('ballot-title', HTMLElement);
    if (span) {
        span.textContent = shown;
        span.dataset['title'] = title;
    }
    document.title = `${shown} — civilsort`;
}

// Copies text to the clipboard and briefly confirms on button.
function copyWithFeedback(button: HTMLButtonElement, text: string): void {
    navigator.clipboard.writeText(text).then(() => {
        const originalText = button.textContent;
        button.textContent = '✓ Copied!';
        setTimeout(() => {
            button.textContent = originalText;
        }, 1500);
    }).catch((err: unknown) => {
        console.error('Failed to copy:', err);
        showError('Failed to copy');
    });
}
