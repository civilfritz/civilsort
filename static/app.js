// Tab switching
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
        btn.classList.add('active');
        const tabId = btn.dataset.tab;
        document.getElementById(tabId).classList.add('active');
        // Save active tab to hash so it survives reload
        location.hash = tabId;
    });
});

// Restore active tab from hash on page load
if (location.hash) {
    const tabId = location.hash.substring(1);
    const tabBtn = document.querySelector(`[data-tab="${tabId}"]`);
    const tabPanel = document.getElementById(tabId);
    if (tabBtn && tabPanel) {
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
        tabBtn.classList.add('active');
        tabPanel.classList.add('active');
    }
}

// Drag-and-drop ranking with two zones
const rankedList = document.getElementById('ranked-list');
const unrankedList = document.getElementById('unranked-list');

if (rankedList && unrankedList && typeof Sortable !== 'undefined') {
    const options = {
        group: 'ranking',
        animation: 150,
        ghostClass: 'sortable-ghost',
        onEnd: function() {
            saveRanking();
        }
    };

    Sortable.create(rankedList, options);
    Sortable.create(unrankedList, options);
}

function saveRanking() {
    // Only save items from the ranked zone
    const items = rankedList.querySelectorAll('.ranked-item');
    const order = Array.from(items).map(li => parseInt(li.dataset.itemId));

    fetch(`/ballot/${ballotId}/rankings`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order: order })
    }).catch(err => {
        console.error('Error saving ranking:', err);
    });
}

// WebSocket for live results
function connectWebSocket() {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${location.host}/ballot/${ballotId}/ws`);

    ws.onmessage = function(event) {
        try {
            const msg = JSON.parse(event.data);
            if (msg.type === 'items_changed') {
                // Items added or deleted - update DOM in-place
                updateItems(msg);
            } else if (msg.type === 'results') {
                // Ranking changed - update results tab in-place
                updateResultsList(msg.results);
            }
        } catch (err) {
            console.error('Error parsing message:', err);
        }
    };

    ws.onclose = function() {
        console.log('WebSocket closed, reconnecting...');
        setTimeout(connectWebSocket, 2000);
    };

    ws.onerror = function(err) {
        console.error('WebSocket error:', err);
    };
}

function updateItems(msg) {
    if (!msg.items) return;

    // Collect existing item IDs from both zones
    const existingIds = new Set();
    const rankedItems = rankedList.querySelectorAll('.ranked-item');
    const unrankedItems = unrankedList.querySelectorAll('.ranked-item');

    rankedItems.forEach(li => existingIds.add(parseInt(li.dataset.itemId)));
    unrankedItems.forEach(li => existingIds.add(parseInt(li.dataset.itemId)));

    // Build map of new items by ID
    const newItemsById = new Map();
    msg.items.forEach(item => newItemsById.set(item.id, item));

    // Remove items that no longer exist
    let rankedRemoved = false;
    [...rankedItems, ...unrankedItems].forEach(li => {
        const itemId = parseInt(li.dataset.itemId);
        if (!newItemsById.has(itemId)) {
            if (li.parentElement === rankedList) {
                rankedRemoved = true;
            }
            li.remove();
        }
    });

    // Add new items to unranked zone
    msg.items.forEach(item => {
        if (!existingIds.has(item.id)) {
            const li = document.createElement('li');
            li.dataset.itemId = item.id;
            li.className = 'ranked-item';

            li.innerHTML = `
                <span class="drag-handle">☰</span>
                <span class="item-name">${escapeHtml(item.name)}</span>
            `;

            // Add "added by you" and delete button if current user owns it
            if (item.addedBy === currentUserId) {
                const actionsSpan = document.createElement('span');
                actionsSpan.className = 'item-actions';
                actionsSpan.innerHTML = `
                    <span class="item-owner-label">added by you</span>
                    <form method="POST" action="/ballot/${ballotId}/items/${item.id}/delete" class="delete-form">
                        <button type="submit" class="delete-btn">Delete</button>
                    </form>
                `;
                li.appendChild(actionsSpan);
            }

            unrankedList.appendChild(li);
        }
    });

    // Update results
    updateResultsList(msg.results);

    // If any ranked items were removed, save the current ranking
    if (rankedRemoved) {
        saveRanking();
    }
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function updateResultsList(results) {
    const list = document.getElementById('results-list');
    if (!list) return;

    if (results.length === 0) {
        list.innerHTML = '<li>No results yet. Add items and rank them!</li>';
        return;
    }

    list.innerHTML = '';
    results.forEach((entry, i) => {
        const li = document.createElement('li');
        li.textContent = `#${entry.rank}: ${entry.name}`;
        list.appendChild(li);
    });
}

// Connect WebSocket when page loads
if (typeof ballotId !== 'undefined') {
    connectWebSocket();
}
