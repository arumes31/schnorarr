// --- 1. Global State & Constants ---
let currentLogLevel = 'all';
let logScrollLocked = false;
let currentPreviewId = null;
let lastTrafficTotal = 0;

// --- 2. Core Logic Functions ---

function updateTopFiles(files) {
    const list = document.getElementById('top-files-list');
    if (!list) return;
    if (!files || files.length === 0) {
        list.innerHTML = '<li style="color: var(--text-muted); text-align: center; padding: 10px;">Monitoring for large files...</li>';
        return;
    }
    list.innerHTML = files.map(f => `<li class="activity-item"><span class="action-badge badge-added">LARGE</span><div style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${f.Path} <span style="color: var(--text-muted); font-size: 11px;">(${f.Size})</span></div></li>`).join('');
}

function addLogLine(data) {
    const logContainer = document.getElementById('log-container');
    if (!logContainer) return;

    // Clear initial placeholder
    if (logContainer.innerText.includes("Awaiting logs...")) {
        logContainer.innerHTML = '';
    }

    const line = document.createElement('div');
    let color = '#00FFAD';
    let msg = typeof data === 'string' ? data : (data.msg || '');
    let level = (data.level || 'info').toLowerCase();
    if (level === 'error') color = '#ff3d00';
    if (level === 'warn') color = '#ffb300';
    if (!/^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}/.test(msg)) {
        msg = `[${new Date().toLocaleTimeString()}] ${msg}`;
    }
    msg = msg.replace(/\[(.*?)\]/g, (match, content) => {
        let c = '#a0aec0';
        if (content.includes('Scanner')) c = '#d6bcfa';
        else if (content.includes('Transferer')) c = '#63b3ed';
        else if (content.includes('Database')) c = '#f6e05e';
        else if (content.includes('Health')) c = '#f687b3';
        else if (content.match(/^\d+$/)) c = '#00ffad';
        else if (content.includes('ERROR')) c = '#ff3d00';
        return `[<span style="color: ${c}; font-weight: bold;">${content}</span>]`;
    });
    line.innerHTML = `<span style="color: ${color};">${msg}</span>`;
    const filter = document.getElementById('log-filter')?.value.toLowerCase() || '';
    if ((filter && !msg.toLowerCase().includes(filter)) || (currentLogLevel !== 'all' && level !== currentLogLevel)) line.style.display = 'none';
    line.style.opacity = '0'; line.style.animation = 'fadeIn 0.3s forwards';
    logContainer.appendChild(line);
    if (!logScrollLocked) logContainer.scrollTop = logContainer.scrollHeight;
    if (logContainer.childNodes.length > 300) logContainer.removeChild(logContainer.firstChild);
}

function setLogLevel(level) {
    currentLogLevel = level;
    toast(`Log Level: ${level.toUpperCase()}`, 'info');
    filterLogs();
}

function filterLogs() {
    const filter = document.getElementById('log-filter')?.value.toLowerCase() || '';
    const container = document.getElementById('log-container');
    if (!container) return;
    const lines = container.getElementsByTagName('div');
    for (let line of lines) {
        const text = line.innerText.toLowerCase();
        // This is a bit simplified, but checks if level matches and filter matches
        const matchesLevel = currentLogLevel === 'all' || text.includes(`[${currentLogLevel.toUpperCase()}]`) || line.innerHTML.includes(currentLogLevel);
        if (text.includes(filter) && matchesLevel) {
            line.style.display = 'block';
        } else {
            line.style.display = 'none';
        }
    }
}

function updateFavicon(status) {
    let color = '#00ffad'; if (status === 'critical') color = '#ff3d00'; if (status === 'paused') color = '#ffb300';
    const svg = `<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none'><path d='M12 2L2 7L12 12L22 7L12 2Z' fill='${encodeURIComponent(color)}'/><path d='M2 17L12 22L22 17' stroke='${encodeURIComponent(color)}' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/><path d='M2 12L12 17L22 12' stroke='${encodeURIComponent(color)}' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/></svg>`;
    const link = document.querySelector("link[rel*='icon']") || document.createElement('link');
    link.type = 'image/svg+xml'; link.rel = 'shortcut icon'; link.href = 'data:image/svg+xml,' + svg;
    document.getElementsByTagName('head')[0].appendChild(link);
}

function drawSparkline(canvasId, data, color, minMax = 1024) {
    const sl = document.getElementById(canvasId);
    if (!sl || data.length < 2) return;
    const path = sl.querySelector('path');
    if (!path) return;
    const width = sl.offsetWidth; const height = sl.offsetHeight;
    
    // Scale max relative to data but ensure visibility
    const max = Math.max(...data, minMax);
    let d = `M 0 ${height}`;
    data.forEach((val, i) => {
        const x = (i / (data.length - 1)) * width;
        const y = height - (val / max) * height;
        d += ` L ${x} ${y}`;
    });
    path.setAttribute('d', d);
}

function updateLatencySparkline(val) {
    const sl = document.getElementById('latency-sparkline');
    if (!sl) return;
    const valEl = document.getElementById('latency-val');
    if (valEl) valEl.innerText = val + 'ms';
    let history = sl.getAttribute('data-history') ? sl.getAttribute('data-history').split(',').map(Number) : [];
    history.push(val); if (history.length > 30) history.shift();
    sl.setAttribute('data-history', history.join(','));
    // Use 50ms as minMax for latency to make graphs visible
    drawSparkline('latency-sparkline', history, '#ffb300', 50);
}

function updateProgress(data) {
    if (data.state) {
        const badge = document.getElementById('main-status-badge');
        if (badge) {
            badge.innerText = data.state;
            badge.className = `status-pill pill-${data.state.toLowerCase().replace('_', '-')}`;
        }
        if (data.state === 'SYNCING') {
            document.title = "Syncing... | schnorarr"; updateFavicon('syncing');
            if (window.nodeMap) window.nodeMap.setSpeed(2);
        } else if (data.state === 'PAUSED') {
            document.title = 'Paused | schnorarr'; updateFavicon('paused');
            if (window.nodeMap) window.nodeMap.setSpeed(0.2);
        } else {
            document.title = 'schnorarr | Dashboard'; updateFavicon('normal');
            if (window.nodeMap) window.nodeMap.setSpeed(1);
        }
    }
    if (data.eta) { const el = document.getElementById('stat-eta'); if (el) el.innerText = data.eta; }
    if (data.speed) {
        const el = document.getElementById('stat-speed'); if (el) el.innerText = data.speed;
        const val = parseBytes(data.speed);
        const bar = document.getElementById('speed-bar'); if (bar) bar.style.width = Math.min((val / (100 * 1024 * 1024)) * 100, 100) + '%';
    }
    if (data.traffic_today) {
        const todayEl = document.getElementById('stat-today');
        if (todayEl) todayEl.innerText = data.traffic_today;
    }
    if (data.traffic_total) {
        const totalEl = document.getElementById('stat-total');
        if (totalEl) totalEl.innerText = data.traffic_total;
    }
    if (data.latency) { updateLatencySparkline(data.latency); }
    if (data.hasOwnProperty('receiver_healthy')) {
        const receiverBadge = document.getElementById('receiver-badge');
        if (receiverBadge) {
            receiverBadge.className = `status-pill ${data.receiver_healthy ? 'pill-active' : 'pill-critical'}`;
            receiverBadge.innerText = data.receiver_healthy ? 'ONLINE' : 'OFFLINE';
            let title = `Ver: ${data.receiver_version || 'N/A'} | Up: ${data.receiver_uptime || 'N/A'}`;
            if (data.receiver_msg) title += `\nStatus: ${data.receiver_msg}`;
            receiverBadge.title = title;
        }
    }
    if (data.engines) {
        data.engines.forEach(eng => {
            const container = document.getElementById(`engine-progress-container-${eng.id}`);
            const bar = document.getElementById(`engine-progress-bar-${eng.id}`);
            const fileText = document.getElementById(`engine-current-file-${eng.id}`);
            const speedText = document.getElementById(`engine-current-speed-${eng.id}`);
            const statusPill = document.getElementById(`engine-status-${eng.id}`);
            const radar = document.getElementById(`engine-radar-${eng.id}`);
            const todayText = document.getElementById(`engine-today-${eng.id}`);
            const totalText = document.getElementById(`engine-total-${eng.id}`);
            const elapsedEl = document.getElementById(`engine-elapsed-${eng.id}`);
            const avgEl = document.getElementById(`engine-avg-${eng.id}`);
            const lastSyncEl = document.getElementById(`engine-lastsync-${eng.id}`);

            if (lastSyncEl && eng.last_sync) {
                lastSyncEl.setAttribute('data-time', eng.last_sync);
                lastSyncEl.innerText = timeAgo(eng.last_sync);
            }
            if (todayText) todayText.innerText = eng.today;
            if (totalText) totalText.innerText = eng.total;
            if (radar) radar.style.display = eng.is_scanning ? 'flex' : 'none';
            if (statusPill) {
                if (eng.is_active) { statusPill.innerText = 'SYNCING'; statusPill.className = 'status-pill pill-syncing'; }
                else { const st = eng.is_paused ? 'PAUSED' : 'ACTIVE'; statusPill.innerText = st; statusPill.className = `status-pill pill-${st.toLowerCase()}`; }
            }
            if (container && eng.is_active) {
                container.style.display = 'block';
                if (bar) bar.style.width = eng.percent + '%';
                if (fileText) fileText.innerText = eng.file || '...';
                if (speedText) speedText.innerText = `${eng.speed} (${eng.eta})`;
                if (elapsedEl) elapsedEl.innerText = `Elapsed: ${eng.elapsed}`;
                if (avgEl) avgEl.innerText = `Avg: ${eng.avg_speed}`;
                const sl = document.getElementById(`sparkline-${eng.id}`);
                if (sl && eng.speed_history) { sl.setAttribute('data-history', eng.speed_history.join(',')); drawSparkline(`sparkline-${eng.id}`, eng.speed_history, '#00ffad', 1024); }
            } else if (container) container.style.display = 'none';
        });
    }
}

function timeAgo(date) {
    if (!date || date.startsWith("0001")) return "Never";
    const seconds = Math.floor((new Date() - new Date(date)) / 1000);
    if (seconds < 5) return "just now";
    let interval = seconds / 31536000;
    if (interval > 1) return Math.floor(interval) + "y";
    interval = seconds / 2592000;
    if (interval > 1) return Math.floor(interval) + "mo";
    interval = seconds / 86400;
    if (interval > 1) return Math.floor(interval) + "d";
    interval = seconds / 3600;
    if (interval > 1) return Math.floor(interval) + "h";
    interval = seconds / 60;
    if (interval > 1) return Math.floor(interval) + "m";
    return Math.floor(seconds) + "s";
}

function updateRelativeTimes() {
    document.querySelectorAll('.relative-time').forEach(el => {
        const time = el.getAttribute('data-time');
        if (time) el.innerText = timeAgo(time);
    });
}

// --- 3. WebSocket Setup ---
const socket = new WebSocket((window.location.protocol === 'https:' ? 'wss://' : 'ws://') + window.location.host + '/ws');
socket.onmessage = function (event) {
    try {
        const msg = JSON.parse(event.data);
        if (msg.type === 'progress') {
            updateProgress(msg.data);
            if (msg.data.top_files) updateTopFiles(msg.data.top_files);
        }
        else if (msg.type === 'history') addHistoryItem(msg.data);
        else if (msg.type === 'log') addLogLine(msg.data);
    } catch (e) { console.error("WS Message Error:", e); }
};

// --- 4. Sidebar & Settings ---
function setTheme(name) {
    document.documentElement.setAttribute('data-theme', name);
    localStorage.setItem('schnorarr-theme', name);
    toast(`Theme: ${name.toUpperCase()}`, 'success');
}

function toggleWebhookVisibility() {
    const input = document.getElementById('webhook-input');
    if (input) {
        input.type = input.type === 'password' ? 'text' : 'password';
    }
}

function filterEngines() {
    const query = document.getElementById('engine-search')?.value.toLowerCase() || '';
    document.querySelectorAll('.engine-card').forEach(card => {
        const id = card.id.replace('engine-card-', '');
        const alias = document.getElementById(`alias-${id}`)?.innerText.toLowerCase() || '';
        const source = card.querySelector('.path-value')?.innerText.toLowerCase() || '';
        if (id.includes(query) || alias.includes(query) || source.includes(query)) {
            card.style.display = 'block';
        } else {
            card.style.display = 'none';
        }
    });
}

function cycleSyncMode() {
    const el = document.getElementById('sync-mode-switch'); if (!el) return;
    const current = el.getAttribute('data-val');
    let next = current === 'dry' ? 'manual' : (current === 'manual' ? 'auto' : 'dry');
    el.setAttribute('data-val', next);
    const formData = new FormData(); formData.append('mode', next);
    fetch('/settings/sync-mode', { method: 'POST', body: formData }).then(() => toast(`Mode: ${next.toUpperCase()}`, 'info'));
}

function updateAutoApprove(checkbox) {
    const val = checkbox.checked ? 'on' : 'off';
    const formData = new FormData(); formData.append('auto_approve', val);
    fetch('/settings/auto-approve', { method: 'POST', body: formData }).then(() => toast(`Auto-Approve: ${val.toUpperCase()}`, 'info'));
}

function cycleOverrideMode() {
    const el = document.getElementById('override-switch'); if (!el) return;
    const current = el.getAttribute('data-val');
    const next = current === 'override' ? 'ask' : 'override';
    el.setAttribute('data-val', next);
    const formData = new FormData(); formData.append('enabled', next === 'override');
    fetch('/settings/sender-override', { method: 'POST', body: formData }).then(() => toast(`Conflicts: ${next.toUpperCase()}`, 'info'));
}

// --- 5. Engine Actions ---
function onEngineSelect() {
    const selected = document.querySelectorAll('.engine-select:checked');
    const bar = document.getElementById('group-actions-bar');
    const count = document.getElementById('group-count');
    if (selected.length > 0) { if (count) count.innerText = selected.length; if (bar) bar.classList.add('active'); }
    else { if (bar) bar.classList.remove('active'); const master = document.getElementById('select-all-engines'); if (master) master.checked = false; }
}

function toggleAllEngines(master) { document.querySelectorAll('.engine-select').forEach(cb => cb.checked = master.checked); onEngineSelect(); }
function deselectAll() { const master = document.getElementById('select-all-engines'); if (master) master.checked = false; toggleAllEngines({checked: false}); }

async function executeBulkAction(action) {
    const selected = Array.from(document.querySelectorAll('.engine-select:checked')).map(cb => cb.value);
    if (selected.length === 0) return;
    try {
        const resp = await fetch('/api/engines/bulk', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: selected, action: action }) });
        if (resp.ok) { toast(`Bulk ${action.toUpperCase()} Success`, 'success'); if (action !== 'sync') setTimeout(() => window.location.reload(), 800); else deselectAll(); }
    } catch (e) { toast('Bulk action failed', 'error'); }
}

function engineAction(id, action) {
    fetch(`/api/engine/${id}/${action}`, { method: 'POST' }).then(resp => {
        if (resp.ok) { toast(`${action.toUpperCase()} Signal Sent`, 'success'); if (action !== 'sync') setTimeout(() => window.location.reload(), 500); }
    });
}

function editAlias(id) {
    const el = document.getElementById(`alias-${id}`);
    const current = el ? el.innerText : '';
    const next = prompt("Enter new alias:", current);
    if (next !== null && next.trim() !== "" && next !== current) {
        const formData = new FormData(); formData.append('alias', next.trim());
        fetch(`/api/engine/${id}/alias`, { method: 'POST', body: formData }).then(r => { if (r.ok) { if (el) el.innerText = next.trim(); toast("Alias Updated", "success"); } });
    }
}

// --- 6. Modal & Preview ---
async function showPreview(id, mode = 'preview') {
    currentPreviewId = id;
    const modal = document.getElementById('modal-container');
    const body = document.getElementById('preview-body');
    const loading = document.getElementById('preview-loading');
    const stats = document.getElementById('preview-stats');
    const details = document.getElementById('preview-details');
    const confirmBtn = document.getElementById('preview-confirm-btn');
    document.getElementById('preview-id').innerText = id;
    if (modal) modal.style.display = 'flex'; if (loading) loading.style.display = 'block'; if (body) body.style.display = 'none';
    try {
        const resp = await fetch(`/api/engine/${id}/preview`);
        const plan = await resp.json();
        if (loading) loading.style.display = 'none'; if (body) body.style.display = 'block';
        if (stats) stats.innerHTML = `<div class="stat-card" style="padding:15px;"><div class="stat-label">Sync</div><div class="stat-value" style="font-size:20px;">${plan.FilesToSync.length}</div></div><div class="stat-card" style="padding:15px;"><div class="stat-label">Delete</div><div class="stat-value" style="font-size:20px; color:var(--accent-error);">${plan.FilesToDelete.length}</div></div><div class="stat-card" style="padding:15px;"><div class="stat-label">Conflicts</div><div class="stat-value" style="font-size:20px; color:var(--accent-warning);">${plan.Conflicts.length}</div></div>`;
        let html = '<table style="width:100%; border-collapse: collapse; font-size:12px;"><tr style="text-align:left; color:var(--text-muted); border-bottom:1px solid var(--border-glass);"><th style="padding:10px;">Action</th><th>File</th><th>Details</th></tr>';
        plan.Conflicts.forEach(c => {
            const isSourceNewer = new Date(c.source_time) > new Date(c.receiver_time);
            html += `<tr style="border-bottom:1px solid rgba(255,255,255,0.05);"><td style="padding:10px;"><span class="action-badge badge-renamed">DIFF</span></td><td>${c.path}</td><td><div style="font-size:10px; color:var(--accent-warning);">${isSourceNewer ? 'Sender is NEWER' : 'Sender is OLDER'}</div><div style="font-size:9px; opacity:0.6;">Size diff: ${formatBytes(Math.abs(c.source_size - c.receiver_size))}</div></td></tr>`;
        });
        plan.FilesToSync.forEach(f => { if (!plan.Conflicts.some(c => c.path === f.Path)) html += `<tr style="border-bottom:1px solid rgba(255,255,255,0.05);"><td style="padding:10px;"><span class="action-badge badge-added">ADD</span></td><td>${f.Path}</td><td>${formatBytes(f.Size)}</td></tr>`; });
        html += '</table>'; if (details) details.innerHTML = html;
    } catch (e) { if (details) details.innerHTML = "Error loading preview."; }
}

function closeModal() { const el = document.getElementById('modal-container'); if (el) el.style.display = 'none'; }
async function confirmSyncFromPreview() { if (!currentPreviewId) return; const btn = document.getElementById('preview-confirm-btn'); if (btn) btn.disabled = true; try { const resp = await fetch(`/api/engine/${currentPreviewId}/sync`, { method: 'POST' }); if (resp.ok) { toast("Sync started", "success"); closeModal(); } } finally { if (btn) btn.disabled = false; } }

// --- 7. UI Helpers ---
function formatBytes(b) { b = Math.abs(b); if (b === 0) return '0 B'; const k = 1024, s = ['B', 'KB', 'MB', 'GB', 'TB'], i = Math.floor(Math.log(b) / Math.log(k)); return parseFloat((b / Math.pow(k, i)).toFixed(1)) + ' ' + s[i]; }
function parseBytes(str) { if (!str) return 0; const parts = str.trim().split(' '); if (parts.length < 2) return 0; const val = parseFloat(parts[0]); const unit = parts[1].toUpperCase(); if (unit.includes('KB')) return val * 1024; if (unit.includes('MB')) return val * 1024 * 1024; if (unit.includes('GB')) return val * 1024 * 1024 * 1024; return val; }
function toast(msg, type = 'info') { const c = document.getElementById('toast-container'); if (!c) return; const t = document.createElement('div'); t.className = 'toast'; t.style.borderLeftColor = type === 'success' ? 'var(--accent-primary)' : 'var(--accent-warning)'; t.innerText = msg; c.appendChild(t); setTimeout(() => t.remove(), 4000); }
function toggleLogScroll() { logScrollLocked = !logScrollLocked; const btn = document.getElementById('log-scroll-toggle'); if (btn) btn.innerText = logScrollLocked ? 'Locked' : 'Auto'; }
function clearLogs() { const logContainer = document.getElementById('log-container'); if (logContainer) logContainer.innerHTML = 'Buffer cleared.'; }

function toggleTerminalFullscreen() {
    const term = document.querySelector('.terminal-window');
    if (term) term.classList.toggle('fullscreen');
}

function downloadLogs() {
    const container = document.getElementById('log-container');
    if (!container) return;
    const text = container.innerText;
    const blob = new Blob([text], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `schnorarr-logs-${new Date().toISOString()}.txt`;
    a.click();
    URL.revokeObjectURL(url);
}

function copyToClipboard(text, btn) { navigator.clipboard.writeText(text).then(() => { const old = btn.innerText; btn.innerText = 'OK'; setTimeout(() => btn.innerText = old, 2000); }); }
function addHistoryItem(data) {
    const list = document.getElementById('history-list');
    if (!list) return;
    const li = document.createElement('li');
    li.className = 'activity-item';
    const actionClass = data.Action.toLowerCase().trim().replace(/\s+/g, '-');
    li.innerHTML = `<span class="action-badge badge-${actionClass}">${data.Action}</span><div style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${data.Path}</div>`;
    list.insertBefore(li, list.firstChild);
    if (list.childNodes.length > 10) list.removeChild(list.lastChild);
}

const NodeMap = {
    canvas: null, ctx: null, speedMult: 1,
    init() { const c = document.getElementById('node-map-bg'); if (!c) return; this.canvas = document.createElement('canvas'); c.appendChild(this.canvas); this.ctx = this.canvas.getContext('2d'); this.resize(); },
    resize() { if (this.canvas) { this.canvas.width = window.innerWidth; this.canvas.height = window.innerHeight; } }
};

document.addEventListener('DOMContentLoaded', () => {
    NodeMap.init();
    const savedTheme = localStorage.getItem('schnorarr-theme');
    if (savedTheme) document.documentElement.setAttribute('data-theme', savedTheme);
    document.querySelectorAll('.sparkline-container').forEach(sl => { const histStr = sl.getAttribute('data-history') || ""; const history = histStr ? histStr.split(',').map(Number) : []; if (history.length >= 2) drawSparkline(sl.id, history, '#00ffad'); });

    updateRelativeTimes();
    setInterval(updateRelativeTimes, 30000);
});

window.addEventListener('keydown', e => {
    if (e.target.tagName === 'INPUT') return;
    if (e.key.toLowerCase() === 's') window.location.href = '/sync';
    if (e.key.toLowerCase() === 'p') window.location.href = '/pause';
    if (e.key === '/') { e.preventDefault(); const s = document.getElementById('engine-search'); if (s) s.focus(); }
    if (e.key === '?') toast("Shortcuts: S (Sync), P (Pause), / (Search)", "info");
});

if (Notification.permission !== "granted" && Notification.permission !== "denied") Notification.requestPermission();

// --- 8. Error & Receiver Modals ---
function showReceiverError() {
    const badge = document.getElementById('receiver-badge');
    if (!badge) return;
    const msg = badge.title || "No additional information available.";
    showErrorModal("Receiver Status", msg);
}

function showErrorModal(title, msg) {
    const modal = document.getElementById('error-modal');
    const titleEl = document.getElementById('error-title');
    const msgEl = document.getElementById('error-msg');
    if (modal && titleEl && msgEl) {
        titleEl.innerText = title;
        msgEl.innerText = msg;
        modal.style.display = 'flex';
    }
}

function closeErrorModal() {
    const modal = document.getElementById('error-modal');
    if (modal) modal.style.display = 'none';
}
