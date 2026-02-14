// Hamburger menu
const hamburgerMenu = document.getElementById('hamburger-menu');
const menuDropdown = document.getElementById('menu-dropdown');
const menuWrapper = document.getElementById('menu-wrapper');

if (hamburgerMenu && menuDropdown) {
    hamburgerMenu.addEventListener('click', (e) => {
        e.stopPropagation();
        menuDropdown.classList.toggle('open');
    });

    // Close menu when clicking outside
    document.addEventListener('click', (e) => {
        if (menuDropdown.classList.contains('open') && !menuWrapper.contains(e.target)) {
            menuDropdown.classList.remove('open');
        }
    });

    // Close menu on escape key
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && menuDropdown.classList.contains('open')) {
            menuDropdown.classList.remove('open');
        }
    });

    // Don't close menu when clicking theme toggle, copy, or share buttons
    const menuThemeToggle = menuDropdown.querySelector('.theme-toggle');
    if (menuThemeToggle) {
        menuThemeToggle.addEventListener('click', (e) => {
            e.stopPropagation();
        });
    }

    const menuCopyBtn = menuDropdown.querySelector('#copy-url-btn');
    if (menuCopyBtn) {
        menuCopyBtn.addEventListener('click', (e) => {
            e.stopPropagation();
        });
    }

    const menuShareBtn = menuDropdown.querySelector('#share-url-btn');
    if (menuShareBtn) {
        menuShareBtn.addEventListener('click', (e) => {
            e.stopPropagation();
        });
    }
}

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

// Confirm before deleting items
document.addEventListener('submit', function(e) {
    if (!e.target.classList.contains('delete-form')) return;
    const item = e.target.closest('.ranked-item');
    const name = item ? item.querySelector('.item-name').textContent.trim() : 'this item';
    if (!confirm('Are you sure you want to delete "' + name + '"?')) {
        e.preventDefault();
    }
});

// Name input saving
const displayNameInput = document.getElementById('display-name');
if (displayNameInput) {
    function saveDisplayName() {
        const name = displayNameInput.value.trim();
        fetch(`/ballot/${ballotId}/name`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
            body: `name=${encodeURIComponent(name)}`
        }).catch(err => {
            console.error('Error saving name:', err);
        });
    }

    displayNameInput.addEventListener('blur', saveDisplayName);
    displayNameInput.addEventListener('keypress', function(e) {
        if (e.key === 'Enter') {
            e.preventDefault();
            saveDisplayName();
            displayNameInput.blur();
        }
    });
}

// Participant list toggle
const participantToggle = document.getElementById('participant-toggle');
const participantList = document.getElementById('participant-list');
if (participantToggle && participantList) {
    participantToggle.addEventListener('click', function(e) {
        e.stopPropagation();
        const isVisible = participantList.style.display !== 'none';
        participantList.style.display = isVisible ? 'none' : 'block';
    });

    // Close participant list when clicking elsewhere
    document.addEventListener('click', function(e) {
        if (!participantList.contains(e.target) && e.target !== participantToggle) {
            participantList.style.display = 'none';
        }
    });
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
                if (msg.participantCount !== undefined) {
                    updateParticipantCount(msg.participantCount);
                }
            } else if (msg.type === 'results') {
                // Ranking changed - update results tab in-place
                updateResultsList(msg.results);
                if (msg.participantCount !== undefined) {
                    updateParticipantCount(msg.participantCount);
                }
            } else if (msg.type === 'state_changed') {
                // Ballot state changed - update toggle UI
                updateToggleUI(msg.isOpen);
            } else if (msg.type === 'participants_changed') {
                // Participants or names changed - update everything
                updateItems(msg);
                updateResultsList(msg.results);
                updateParticipantCount(msg.participantCount);
                updateParticipantList(msg.participants);
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

    // Update attribution for existing items
    [...rankedItems, ...unrankedItems].forEach(li => {
        const itemId = parseInt(li.dataset.itemId);
        const item = newItemsById.get(itemId);
        if (item) {
            const metaSpan = li.querySelector('.item-meta');
            if (metaSpan) {
                // Update attribution span
                let attrSpan = metaSpan.querySelector('.item-attribution');
                if (item.addedByName) {
                    if (!attrSpan) {
                        attrSpan = document.createElement('span');
                        attrSpan.className = 'item-attribution';
                        metaSpan.insertBefore(attrSpan, metaSpan.firstChild);
                    }
                    attrSpan.textContent = item.addedByName;
                } else if (attrSpan) {
                    attrSpan.remove();
                }
            }
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
                <span class="item-meta"></span>
            `;

            const metaSpan = li.querySelector('.item-meta');

            // Add attribution if available
            if (item.addedByName) {
                const attrSpan = document.createElement('span');
                attrSpan.className = 'item-attribution';
                attrSpan.textContent = item.addedByName;
                metaSpan.appendChild(attrSpan);
            }

            // Add delete button if current user owns it
            if (item.addedBy === currentUserId) {
                const deleteForm = document.createElement('form');
                deleteForm.method = 'POST';
                deleteForm.action = `/ballot/${ballotId}/items/${item.id}/delete`;
                deleteForm.className = 'delete-form';
                deleteForm.innerHTML = `<button type="submit" class="delete-btn">Delete</button>`;
                metaSpan.appendChild(deleteForm);
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
    results.forEach((entry) => {
        const li = document.createElement('li');
        li.textContent = `#${entry.rank}: ${entry.name}`;
        list.appendChild(li);
    });
}

function updateParticipantCount(count) {
    const toggle = document.getElementById('participant-toggle');
    if (toggle) {
        toggle.textContent = count === 1 ? '1 participant' : `${count} participants`;
    }
}

function updateParticipantList(participants) {
    const list = document.getElementById('participant-list');
    if (!list) return;

    list.innerHTML = '';
    let anonymousCount = 0;

    participants.forEach(p => {
        if (p.displayName) {
            const li = document.createElement('li');
            li.textContent = p.displayName;
            list.appendChild(li);
        } else {
            anonymousCount++;
        }
    });

    if (anonymousCount > 0) {
        const li = document.createElement('li');
        li.className = 'anonymous-label';
        li.textContent = anonymousCount === 1 ? '1 anonymous' : `${anonymousCount} anonymous`;
        list.appendChild(li);
    }
}

// Connect WebSocket when page loads
if (typeof ballotId !== 'undefined') {
    connectWebSocket();
}

// Theme toggle
const themeToggle = document.getElementById('theme-toggle');
if (themeToggle) {
    // Set initial text
    function updateThemeIcon() {
        const currentTheme = document.documentElement.dataset.theme || 'light';
        themeToggle.textContent = currentTheme === 'dark' ? 'Light mode' : 'Dark mode';
    }
    updateThemeIcon();

    themeToggle.addEventListener('click', () => {
        const currentTheme = document.documentElement.dataset.theme || 'light';
        const newTheme = currentTheme === 'dark' ? 'light' : 'dark';
        document.documentElement.dataset.theme = newTheme;
        localStorage.setItem('theme', newTheme);
        updateThemeIcon();
    });
}

// Ballot access toggle
const toggleBtn = document.getElementById('toggle-open-btn');
if (toggleBtn) {
    toggleBtn.addEventListener('click', function() {
        fetch(`/ballot/${ballotId}/toggle`, {
            method: 'POST',
        }).then(response => {
            if (!response.ok) {
                console.error('Error toggling ballot state');
                return;
            }
            return response.json();
        }).then(data => {
            if (data) {
                updateToggleUI(data.isOpen);
            }
        }).catch(err => {
            console.error('Error toggling ballot state:', err);
        });
    });
}

// Copy URL button
const copyUrlBtn = document.getElementById('copy-url-btn');
const shareUrlBtn = document.getElementById('share-url-btn');
if (copyUrlBtn && typeof ballotId !== 'undefined') {
    // Copy button functionality
    copyUrlBtn.addEventListener('click', function() {
        const ballotUrl = `${window.location.origin}/ballot/${ballotId}`;
        navigator.clipboard.writeText(ballotUrl).then(() => {
            // Visual feedback
            const originalText = copyUrlBtn.textContent;
            copyUrlBtn.textContent = '✓ Copied!';
            setTimeout(() => {
                copyUrlBtn.textContent = originalText;
            }, 1500);
        }).catch(err => {
            console.error('Failed to copy URL:', err);
        });
    });
}

// Share button functionality (Web Share API)
if (shareUrlBtn && typeof ballotId !== 'undefined') {
    // Check if Web Share API is available
    if (!navigator.share) {
        // Hide share button if API not available
        shareUrlBtn.style.display = 'none';
    } else {
        shareUrlBtn.addEventListener('click', async function() {
            try {
                const ballotUrl = `${window.location.origin}/ballot/${ballotId}`;
                const ballotTitle = document.querySelector('.ballot-header h1')?.textContent || 'Ballot';
                await navigator.share({
                    title: ballotTitle,
                    text: `Vote on: ${ballotTitle}`,
                    url: ballotUrl
                });
            } catch (err) {
                // User cancelled or error occurred
                if (err.name !== 'AbortError') {
                    console.error('Failed to share:', err);
                }
            }
        });
    }
}

function updateToggleUI(newIsOpen) {
    isOpen = newIsOpen;
    const btn = document.getElementById('toggle-open-btn');

    if (!btn) return;

    if (newIsOpen) {
        btn.textContent = 'Open';
        btn.classList.remove('toggle-closed');
        btn.classList.add('toggle-open');
    } else {
        btn.textContent = 'Closed';
        btn.classList.remove('toggle-open');
        btn.classList.add('toggle-closed');
    }
}
