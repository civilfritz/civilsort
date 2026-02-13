// Tab switching
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
        btn.classList.add('active');
        document.getElementById(btn.dataset.tab).classList.add('active');
    });
});

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
                // Items added or deleted - reload page to update items list and ranking list
                window.location.reload();
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
