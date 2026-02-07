// WebSocket Connection
const socket = new WebSocket((window.location.protocol === 'https:' ? 'wss://' : 'ws://') + window.location.host + '/ws');
const logContainer = document.getElementById('log-container');

socket.onmessage = function (event) {
    const msg = JSON.parse(event.data);
    if (msg.type === 'progress') updateProgress(msg.data);
    else if (msg.type === 'history') addHistoryItem(msg.data);
    else if (msg.type === 'log') addLogLine(msg.data);
};

function addLogLine(data) {
    const line = document.createElement('div');
    let color = '#00FFAD';
    let msg = typeof data === 'string' ? data : (data.msg || '');
    let level = data.level || 'info';
    if (level === 'error') color = '#ff3d00';
    if (level === 'warn') color = '#ffb300';
    if (/^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}/.test(msg)) {
    } else {
        const timeStr = new Date().toLocaleTimeString();
        msg = `[${timeStr}] ${msg}`;
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
    msg = msg.replace(/'([^']*)'/g, '<span style="color: #81e6d9;">\'$1\'</span>');
    line.innerHTML = `<span style="color: ${color};">${msg}</span>`;
    const filter = document.getElementById('log-filter').value.toLowerCase();
    if (filter && !msg.toLowerCase().includes(filter)) line.style.display = 'none';
    line.style.opacity = '0'; line.style.animation = 'fadeIn 0.3s forwards';
    logContainer.appendChild(line); logContainer.scrollTop = logContainer.scrollHeight;
    if (logContainer.childNodes.length > 300) logContainer.removeChild(logContainer.firstChild);
}

function filterLogs() {
    const filter = document.getElementById('log-filter').value.toLowerCase();
    const lines = logContainer.querySelectorAll('div');
    lines.forEach(line => { line.style.display = line.innerText.toLowerCase().includes(filter) ? 'block' : 'none'; });
}

function updateProgress(data) {
    if (data.state) {
        const badge = document.getElementById('main-status-badge');
        badge.innerText = data.state;
        badge.className = `status-pill pill-${data.state.toLowerCase().replace('_', '-')}`;
        if (data.state === 'SYNCING') document.title = `⚡ ${data.speed} | schnorarr`;
        else document.title = 'schnorarr | Sync Dashboard';
    }
    if (data.eta) document.getElementById('stat-eta').innerText = data.eta;
    if (data.speed) {
        document.getElementById('stat-speed').innerText = data.speed;
        let val = parseBytes(data.speed);
        let mbps = val / (1024 * 1024);
        let width = Math.min((mbps / 100) * 100, 100);
        document.getElementById('speed-bar').style.width = width + '%';
        if (val > 0) updateGlobalTraffic(val);
    }
    if (data.engines) {
        data.engines.forEach(eng => {
            const container = document.getElementById(`engine-progress-container-${eng.id}`);
            const bar = document.getElementById(`engine-progress-bar-${eng.id}`);
            const fileText = document.getElementById(`engine-current-file-${eng.id}`);
            const speedText = document.getElementById(`engine-current-speed-${eng.id}`);
            const statusPill = document.getElementById(`engine-status-${eng.id}`);
            const queueBadge = document.getElementById(`engine-queue-${eng.id}`);
            const flowLine = document.getElementById(`flow-${eng.id}`);
            const radar = document.getElementById(`engine-radar-${eng.id}`);
            const todayText = document.getElementById(`engine-today-${eng.id}`);
            const totalText = document.getElementById(`engine-total-${eng.id}`);

            if (todayText) todayText.innerText = eng.today;
            if (totalText) totalText.innerText = eng.total;
            if (radar) radar.style.display = eng.is_scanning ? 'flex' : 'none';
            
            if (eng.is_scanning && statusPill) {
                statusPill.innerText = 'SCANNING';
                statusPill.className = 'status-pill pill-syncing';
            }

            if (queueBadge) {
                if (eng.queue_count > 0) { queueBadge.innerText = `${eng.queue_count} PENDING`; queueBadge.style.display = 'inline-block'; }
                else queueBadge.style.display = 'none';
            }

            if (container && eng.is_active) {
                container.style.display = 'block';
                const card = document.getElementById(`engine-card-${eng.id}`);
                if (card) card.classList.add('syncing-glow');
                if (flowLine) flowLine.classList.add('syncing');
                bar.style.width = eng.percent + '%';
                fileText.innerText = eng.file || '...';
                speedText.innerText = `${eng.speed} (${eng.eta})`;
                if (statusPill && !statusPill.classList.contains('pill-waiting') && !statusPill.classList.contains('pill-paused')) {
                    statusPill.innerText = 'SYNCING'; statusPill.className = 'status-pill pill-syncing';
                }
            } else if (container) {
                container.style.display = 'none';
                const card = document.getElementById(`engine-card-${eng.id}`);
                if (card) card.classList.remove('syncing-glow');
                if (flowLine) flowLine.classList.remove('syncing');
                if (statusPill && statusPill.innerText === 'SYNCING') {
                    statusPill.innerText = 'ACTIVE'; statusPill.className = 'status-pill pill-active';
                }
            }
        });
    }
}

let lastTrafficToday = 0; let lastTrafficTotal = 0;
function updateGlobalTraffic(speedBytesPerSec) {
    const todayEl = document.getElementById('stat-today');
    const totalEl = document.getElementById('stat-total');
    if (lastTrafficToday === 0) lastTrafficToday = parseBytes(todayEl.innerText);
    if (lastTrafficTotal === 0) lastTrafficTotal = parseBytes(totalEl.innerText);
    lastTrafficToday += speedBytesPerSec; lastTrafficTotal += speedBytesPerSec;
    
    todayEl.innerText = formatBytes(lastTrafficToday);
    totalEl.innerText = formatBytes(lastTrafficTotal);

    const now = new Date();
    const dateStr = now.getFullYear() + '/' + String(now.getMonth() + 1).padStart(2, '0') + '/' + String(now.getDate()).padStart(2, '0');
    const chartBar = document.getElementById(`chart-bar-${dateStr}`);
    const chartVal = document.getElementById(`chart-val-${dateStr}`);
    if (chartVal) chartVal.innerText = formatBytes(lastTrafficToday);
    if (chartBar) {
        let height = Math.min((lastTrafficToday / (1024 * 1024 * 1024)) * 100, 100);
        chartBar.style.height = Math.max(height, 5) + '%'; 
    }
}

function parseBytes(str) {
    const parts = str.split(' '); if (parts.length < 2) return 0;
    const val = parseFloat(parts[0]); const unit = parts[1].toUpperCase();
    if (unit.includes('KB') || unit.includes('KIB')) return val * 1024;
    if (unit.includes('MB') || unit.includes('MIB')) return val * 1024 * 1024;
    if (unit.includes('GB') || unit.includes('GIB')) return val * 1024 * 1024 * 1024;
    if (unit.includes('TB') || unit.includes('TIB')) return val * 1024 * 1024 * 1024 * 1024;
    return val;
}

function formatBytes(b) {
    if (b === 0) return '0 B';
    const k = 1024, s = ['B', 'KB', 'MB', 'GB', 'TB'], i = Math.floor(Math.log(b) / Math.log(k));
    return parseFloat((b / Math.pow(k, i)).toFixed(1)) + ' ' + s[i];
}

function addHistoryItem(data) {
    const list = document.getElementById('history-list');
    const li = document.createElement('li'); li.className = 'activity-item';
    const actionClass = data.Action.toLowerCase().replace('dry-', 'dry');
    li.innerHTML = `
        <span class="action-badge badge-${actionClass} ${data.Action.startsWith('DRY-') ? 'badge-dry' : ''}">${data.Action}</span>
        <div style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
            ${data.Path} <span style="color: var(--text-muted); font-size: 11px;">(${data.Size || '0 B'})</span>
        </div>
        <span style="font-family: monospace; font-size: 11px; color: var(--text-muted);">${data.Time}</span>
    `;
    list.insertBefore(li, list.firstChild);
    if (list.childNodes.length > 12) list.removeChild(list.lastChild);
}

function cycleSyncMode() {
    const el = document.getElementById('sync-mode-switch');
    const current = el.getAttribute('data-val');
    let next = current === 'dry' ? 'manual' : (current === 'manual' ? 'auto' : 'dry');
    el.setAttribute('data-val', next);
    const formData = new FormData(); formData.append('mode', next);
    fetch('/settings/sync-mode', { method: 'POST', body: formData })
    .then(r => r.json()).then(data => {
        if (data.status === 'success') toast(`Sync Mode: ${next.toUpperCase()}`, 'success');
        else { toast("Failed", "error"); el.setAttribute('data-val', current); }
    });
}

function updateAutoApprove(checkbox) {
    const val = checkbox.checked ? 'on' : 'off';
    document.getElementById('auto-approve-label').innerText = val.toUpperCase();
    const formData = new FormData(); formData.append('auto_approve', val);
    fetch('/settings/auto-approve', { method: 'POST', body: formData })
    .then(r => r.json()).then(data => {
        if (data.status === 'success') toast(`Auto-Approve: ${val.toUpperCase()}`, 'success');
        else { toast("Failed", "error"); checkbox.checked = !checkbox.checked; }
    });
}

function engineAction(id, action) {
    fetch(`/api/engine/${id}/${action}`, { method: 'POST' })
    .then(r => {
        if (r.ok) { toast(`${action.toUpperCase()} triggered`, 'success'); setTimeout(() => window.location.reload(), 1000); }
        else toast(`Error: ${r.statusText}`, 'error');
    });
}

let currentPreviewId = null; let currentPreviewAction = 'sync';
function showPreview(id, action = 'sync') {
    currentPreviewId = id; currentPreviewAction = action;
    document.getElementById('modal-container').style.display = 'block';
    document.getElementById('preview-loading').style.display = 'block';
    document.getElementById('preview-body').style.display = 'none';
    document.getElementById('preview-id').innerText = id;
    fetch(`/api/engine/${id}/preview`).then(r => r.json()).then(plan => {
        document.getElementById('preview-loading').style.display = 'none';
        document.getElementById('preview-body').style.display = 'block';
        renderPreview(plan);
    });
}

function renderPreview(plan) {
    const stats = document.getElementById('preview-stats');
    const details = document.getElementById('preview-details');
    const count = (arr) => (arr || []).length;
    const renames = Object.keys(plan.Renames || {}).length;
    stats.innerHTML = `
        <div class="stat-card" style="padding: 12px; text-align: center;"><div class="stat-label" style="font-size: 8px;">Syncs</div><div style="color: var(--accent-primary); font-weight: bold;">${count(plan.FilesToSync)}</div></div>
        <div class="stat-card" style="padding: 12px; text-align: center;"><div class="stat-label" style="font-size: 8px;">Deletes</div><div style="color: var(--accent-error); font-weight: bold;">${count(plan.FilesToDelete)}</div></div>
        <div class="stat-card" style="padding: 12px; text-align: center;"><div class="stat-label" style="font-size: 8px;">Renames</div><div style="color: var(--accent-warning); font-weight: bold;">${renames}</div></div>
        <div class="stat-card" style="padding: 12px; text-align: center;"><div class="stat-label" style="font-size: 8px;">Dirs</div><div style="color: var(--accent-secondary); font-weight: bold;">${count(plan.DirsToCreate)}</div></div>
    `;
    let html = '';
    if (count(plan.FilesToSync) > 0) {
        html += '<div style="margin-bottom: 20px;"><div class="path-label">Adds / Updates</div>';
        plan.FilesToSync.forEach(f => {
            if (currentPreviewAction === 'approve') {
                html += `<div style="font-size: 11px; color: var(--accent-error); margin-bottom: 4px; display:flex; align-items:center; gap:8px;"><input type="checkbox" class="deletion-check" value="${f.Path}" checked><span>➕ ${f.Path} (${formatBytes(f.Size)})</span></div>`;
            } else { html += `<div style="font-size: 11px; color: var(--accent-primary); margin-bottom: 4px;">➕ ${f.Path} (${formatBytes(f.Size)})</div>`; }
        });
        html += '</div>';
    }
    if (count(plan.FilesToDelete) > 0) {
        html += '<div><div class="path-label">Deletions</div>';
        plan.FilesToDelete.forEach(f => {
            if (currentPreviewAction === 'approve') {
                html += `<div style="font-size: 11px; color: var(--accent-error); margin-bottom: 4px; display:flex; align-items:center; gap:8px;"><input type="checkbox" class="deletion-check" value="${f}" checked><span>❌ ${f}</span></div>`;
            } else { html += `<div style="font-size: 11px; color: var(--accent-error); margin-bottom: 4px;">❌ ${f}</div>`; }
        });
        html += '</div>';
    }
    if (!html) html = '<div style="text-align: center; color: var(--text-muted);">No changes detected.</div>';
    details.innerHTML = html;
}

function toggleAllDeletions(cb) { document.querySelectorAll('.deletion-check').forEach(el => el.checked = cb.checked); }
function closeModal() { document.getElementById('modal-container').style.display = 'none'; }
function confirmSyncFromPreview() {
    if (!currentPreviewId) return;
    if (currentPreviewAction === 'approve') {
        const selected = Array.from(document.querySelectorAll('.deletion-check:checked')).map(cb => cb.value);
        fetch(`/api/engine/${currentPreviewId}/approve-list`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ files: selected }) })
        .then(r => { toast('Approved', 'success'); setTimeout(() => window.location.reload(), 1000); });
        closeModal();
    } else { engineAction(currentPreviewId, currentPreviewAction); closeModal(); }
}

function copyToClipboard(text, btn) {
    navigator.clipboard.writeText(text).then(() => {
        const original = btn.innerText; btn.innerText = '✅';
        setTimeout(() => { btn.innerText = original; }, 2000);
        toast('Copied', 'success');
    });
}

function updateRelativeTimes() {
    const now = new Date();
    document.querySelectorAll('.relative-time').forEach(el => {
        const date = new Date(el.getAttribute('data-time'));
        if (isNaN(date.getTime())) return;
        const diff = Math.floor((now - date) / 1000);
        if (diff < 5) el.innerText = 'just now';
        else if (diff < 60) el.innerText = diff + 's ago';
        else if (diff < 3600) el.innerText = Math.floor(diff / 60) + 'm ago';
        else el.innerText = Math.floor(diff / 3600) + 'h ago';
    });
}

document.getElementById('auto-approve-label').innerText = document.getElementById('auto-approve-toggle').checked ? 'ON' : 'OFF';
updateRelativeTimes(); setInterval(updateRelativeTimes, 30000);
