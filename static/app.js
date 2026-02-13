// Tab switching
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
        btn.classList.add('active');
        document.getElementById(btn.dataset.tab).classList.add('active');
    });
});

// Drag-and-drop ranking
const rankingList = document.getElementById('ranking-list');
if (rankingList && typeof Sortable !== 'undefined') {
    Sortable.create(rankingList, {
        animation: 150,
        ghostClass: 'sortable-ghost',
        filter: '.section-divider',
        onEnd: function() {
            saveRanking();
        }
    });
}

function saveRanking() {
    const items = rankingList.querySelectorAll('.ranked-item');
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
            const results = JSON.parse(event.data);
            updateResultsList(results);
        } catch (err) {
            console.error('Error parsing results:', err);
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
