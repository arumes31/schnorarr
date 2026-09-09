// --- 1. Global State & Constants ---
let currentLogLevel = 'all';
let logScrollLocked = false;
let currentPreviewId = null;
let previewRequest = null;
let previewReady = false;
let lastTrafficTotal = 0;

function escapeHtml(text) {
    return String(text ?? '').replace(/[&<>"']/g, function (m) {
        switch (m) {
            case '&': return '&amp;';
            case '<': return '&lt;';
            case '>': return '&gt;';
            case '"': return '&quot;';
            case "'": return '&#039;';
            default: return m;
        }
    });
}

// --- 2. Core Logic Functions ---

function updateTopFiles(files) {
    const list = document.getElementById('top-files-list');
    if (!list) return;
    if (!files || files.length === 0) {
        list.innerHTML = '<li style="color: var(--text-muted); text-align: center; padding: 10px;">No completed transfers in the last 24 hours.</li>';
        return;
    }
    list.innerHTML = files.map(f => `<li class="activity-item"><div style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(f.path)} <span style="color: var(--text-muted); font-size: 13px;">(${f.size})</span></div></li>`).join('');
}

function addLogLine(data) {
    const logContainer = document.getElementById('log-container');
    if (!logContainer) return;

    // Clear initial placeholder
    if (!logContainer.querySelector('.log-line')) {
        logContainer.innerHTML = '';
    }

    const line = document.createElement('div');
    line.className = 'log-line';

    let msg = typeof data === 'string' ? data : (data.msg || '');
    let level = (data.level || 'info').toLowerCase();
    line.dataset.level = level;
    line.dataset.search = msg.toLowerCase();

    line.classList.add(`log-level-${level}`);

    if (!/^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}/.test(msg)) {
        msg = `[${new Date().toLocaleTimeString()}] ${msg}`;
    }

    // Replace logic using classes
    // First, escape the entire message to prevent XSS
    msg = escapeHtml(msg);

    msg = msg.replace(/\[(.*?)\]/g, (match, content) => {
        let cls = 'log-comp-default';
        if (content.includes('Scanner')) cls = 'log-comp-scanner';
        else if (content.includes('Transferer')) cls = 'log-comp-transferer';
        else if (content.includes('Database')) cls = 'log-comp-database';
        else if (content.includes('Health')) cls = 'log-comp-health';
        else if (content.includes('SYSTEM')) cls = 'log-comp-error';
        else if (content.includes('Engine')) cls = 'log-comp-scanner';
        else if (content.includes('Event')) cls = 'log-comp-health';
        else if (content.match(/^\d+$/)) cls = 'log-comp-number';
        else if (content.includes('ERROR')) cls = 'log-comp-error';

        return `<span class="log-bracket">[<span class="${cls}">${content}</span>]</span>`;
    });

    line.innerHTML = msg;

    const filter = document.getElementById('log-filter')?.value.toLowerCase() || '';
    line.hidden = !line.dataset.search.includes(filter) || (currentLogLevel !== 'all' && level !== currentLogLevel);

    logContainer.appendChild(line);
    scheduleLogScroll();
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
    for (const line of container.querySelectorAll('.log-line')) {
        line.hidden = !line.dataset.search.includes(filter) ||
            (currentLogLevel !== 'all' && line.dataset.level !== currentLogLevel);
    }
    document.querySelectorAll('[data-log-level]').forEach(button => {
        button.setAttribute('aria-pressed', String(button.dataset.logLevel === currentLogLevel));
    });
}

let logScrollFrame = null;
function scheduleLogScroll() {
    if (logScrollLocked || logScrollFrame !== null) return;
    logScrollFrame = requestAnimationFrame(() => {
        logScrollFrame = null;
        const container = document.getElementById('log-container');
        if (container && !logScrollLocked) container.scrollTop = container.scrollHeight;
    });
}

function getThemeColor(varName, fallback) {
    return getComputedStyle(document.documentElement).getPropertyValue(varName).trim() || fallback;
}

function updateFavicon(status) {
    let color = getThemeColor('--accent-primary', '#00ffad');
    if (status === 'critical') color = getThemeColor('--accent-error', '#ff3d00');
    if (status === 'paused') color = getThemeColor('--accent-warning', '#ffb300');

    const svg = `<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none'><path d='M12 2L2 7L12 12L22 7L12 2Z' fill='${encodeURIComponent(color)}'/><path d='M2 17L12 22L22 17' stroke='${encodeURIComponent(color)}' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/><path d='M2 12L12 17L22 12' stroke='${encodeURIComponent(color)}' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/></svg>`;
    const link = document.querySelector("link[rel*='icon']") || document.createElement('link');
    link.type = 'image/svg+xml'; link.rel = 'shortcut icon'; link.href = 'data:image/svg+xml,' + svg;
    document.getElementsByTagName('head')[0].appendChild(link);
}

// --- Spline Helper ---
function getSplinePath(points, smoothing = 0.2) {
    if (!points || points.length === 0) return "";
    if (points.length === 1) return `M ${points[0][0]} ${points[0][1]}`;

    const getControlPoint = (prev, curr, next, reverse) => {
        const p0 = prev || curr;
        const p1 = next || curr;
        const len = Math.sqrt(Math.pow(p1[0] - p0[0], 2) + Math.pow(p1[1] - p0[1], 2));
        const angle = Math.atan2(p1[1] - p0[1], p1[0] - p0[0]);
        const length = len * smoothing;
        const theta = angle + (reverse ? Math.PI : 0);
        return [curr[0] + Math.cos(theta) * length, curr[1] + Math.sin(theta) * length];
    };

    let d = `M ${points[0][0]} ${points[0][1]}`;
    for (let i = 0; i < points.length - 1; i++) {
        const cp1 = getControlPoint(points[i - 1], points[i], points[i + 1], false);
        const cp2 = getControlPoint(points[i], points[i + 1], points[i + 2], true);
        d += ` C ${cp1[0]} ${cp1[1]} ${cp2[0]} ${cp2[1]} ${points[i + 1][0]} ${points[i + 1][1]}`;
    }
    return d;
}

function drawSparkline(canvasId, rawData, color, minMax = 1024) {
    const sl = document.getElementById(canvasId);
    if (!sl) return;
    const data = rawData.filter(v => typeof v === 'number' && !isNaN(v));
    if (data.length < 2) return;
    const svg = sl.querySelector('svg');
    if (!svg) return;
    const width = sl.offsetWidth; const height = sl.offsetHeight;
    if (width === 0 || height === 0) return;

    // Ensure defs and gradient
    let defs = svg.querySelector('defs');
    if (!defs) { defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs'); svg.prepend(defs); }

    const gradId = 'grad-' + canvasId;
    let linearGradient = defs.querySelector('#' + gradId);
    if (!linearGradient) {
        linearGradient = document.createElementNS('http://www.w3.org/2000/svg', 'linearGradient');
        linearGradient.setAttribute('id', gradId);
        linearGradient.setAttribute('x1', '0%'); linearGradient.setAttribute('y1', '0%');
        linearGradient.setAttribute('x2', '0%'); linearGradient.setAttribute('y2', '100%');
        linearGradient.innerHTML = `<stop offset="0%" class="grad-stop-1" stop-opacity="0.4"/><stop offset="100%" class="grad-stop-2" stop-opacity="0"/>`;
        defs.appendChild(linearGradient);
    }

    // Default color
    if (!color) color = getThemeColor('--accent-primary', '#00ffad');

    // Update gradient colors
    const stop1 = linearGradient.querySelector('.grad-stop-1');
    const stop2 = linearGradient.querySelector('.grad-stop-2');
    if (stop1) stop1.setAttribute('stop-color', color);
    if (stop2) stop2.setAttribute('stop-color', color);

    // Ensure paths
    let areaPath = svg.querySelector('.sparkline-area');
    let linePath = svg.querySelector('.sparkline-line');

    if (!linePath) {
        const existing = svg.querySelector('path');
        if (existing && !existing.classList.contains('sparkline-area')) {
            linePath = existing;
        } else {
            linePath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
            svg.appendChild(linePath);
        }
        linePath.classList.add('sparkline-line');
        linePath.setAttribute('fill', 'none');
    }

    if (!areaPath) {
        areaPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
        areaPath.classList.add('sparkline-area');
        svg.insertBefore(areaPath, linePath); // Area behind line
    }

    areaPath.setAttribute('fill', `url(#${gradId})`);
    areaPath.setAttribute('stroke', 'none');
    linePath.setAttribute('stroke', color);
    linePath.setAttribute('stroke-width', '2');
    linePath.setAttribute('stroke-linecap', 'round');
    linePath.setAttribute('stroke-linejoin', 'round');

    // Scale and Points
    const max = Math.max(...data, minMax);
    const points = data.map((val, i) => {
        const x = (i / (data.length - 1)) * width;
        let yRatio = val / max;
        // Ensure non-zero values have a minimum visible height (5%) even during massive spikes
        if (val > 0 && yRatio < 0.05) yRatio = 0.05;
        const y = (height - 2) - yRatio * (height - 4);
        return [x, y];
    });

    const lineD = getSplinePath(points);
    linePath.setAttribute('d', lineD);

    // Close area path
    const areaD = `${lineD} L ${points[points.length - 1][0]} ${height} L ${points[0][0]} ${height} Z`;
    areaPath.setAttribute('d', areaD);
}

function updateLatencySparkline(val) {
    const sl = document.getElementById('latency-sparkline');
    if (!sl) return;
    const valEl = document.getElementById('latency-val');

    let color = 'var(--accent-error)';
    if (val < 40) color = 'var(--accent-primary)';
    else if (val < 80) color = 'var(--accent-warning)';

    if (valEl) {
        valEl.innerText = val + 'ms';
        valEl.style.color = color;
    }

    let history = sl.getAttribute('data-history') ? sl.getAttribute('data-history').split(',').map(Number) : [];
    history.push(val); if (history.length > 30) history.shift();
    sl.setAttribute('data-history', history.join(','));
    // Use 100ms as minMax for latency to make graphs visible in the background
    drawSparkline('latency-sparkline', history, color, 100);
}

function updateSpeedSparkline(speedStr) {
    const sl = document.getElementById('speed-sparkline');
    if (!sl) return;
    const val = parseBytes(speedStr);
    let history = sl.getAttribute('data-history') ? sl.getAttribute('data-history').split(',').map(Number) : [];
    history.push(val); if (history.length > 30) history.shift();
    sl.setAttribute('data-history', history.join(','));
    // Use 10MB/s as minMax for speed graph scaling
    drawSparkline('speed-sparkline', history, 'var(--accent-primary)', 10 * 1024 * 1024);
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
        } else if (data.state === 'PAUSED') {
            document.title = 'Paused | schnorarr'; updateFavicon('paused');
        } else {
            document.title = 'schnorarr | Dashboard'; updateFavicon('normal');
        }
    }
    updatePolicyFromServer(data);
    if (data.eta) {
        const idle = data.eta === 'Done';
        const el = document.getElementById('stat-eta');
        if (el) el.textContent = idle ? 'No active transfer' : data.eta;
        const clock = document.getElementById('stat-clock');
        const seconds = idle ? 0 : parseDuration(data.eta);
        if (clock) clock.textContent = seconds > 0
            ? 'Estimated finish ' + new Date(Date.now() + seconds * 1000).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'})
            : '';
    }
    if (Object.prototype.hasOwnProperty.call(data, 'speed')) {
        const el = document.getElementById('stat-speed'); if (el) el.innerText = data.speed;
        updateSpeedSparkline(data.speed);
    }
    if (Object.prototype.hasOwnProperty.call(data, 'traffic_today')) {
        const todayEl = document.getElementById('stat-today');
        if (todayEl) todayEl.innerText = data.traffic_today;
    }
    if (Object.prototype.hasOwnProperty.call(data, 'traffic_total')) {
        const totalEl = document.getElementById('stat-total');
        if (totalEl) totalEl.innerText = data.traffic_total;
    }
    if (typeof data.latency === 'number') { updateLatencySparkline(data.latency); }
    if (Object.prototype.hasOwnProperty.call(data, 'bw_limit_mbps')) {
        const cur = document.getElementById('bw-current');
        if (cur) cur.innerText = data.bw_limit_mbps > 0 ? `${data.bw_limit_mbps} Mbps` : 'Unlimited';
        const liveLimit = document.getElementById('live-bw-limit');
        if (liveLimit) liveLimit.textContent = (data.bw_limit_mbps > 0 ? `${data.bw_limit_mbps} Mbps` : 'Unlimited') + (data.bw_source ? ` · ${data.bw_source}` : '');
        const active = document.getElementById('bw-active');
        if (active) active.innerText = data.bw_active;
        const src = document.getElementById('bw-source');
        if (src && data.bw_source) src.innerText = data.bw_source;
    }
    if (data.hasOwnProperty('receiver_healthy')) {
        const receiverBadge = document.getElementById('receiver-badge');
        if (receiverBadge) {
            receiverBadge.className = `status-pill ${data.receiver_healthy ? 'pill-active' : 'pill-critical'}`;
            receiverBadge.innerText = data.receiver_healthy ? 'Receiver online' : 'Receiver offline';
            let title = `Connection only; check storage readiness on each engine. Version: ${data.receiver_version || 'N/A'} · Uptime: ${data.receiver_uptime || 'N/A'}`;
            if (data.receiver_msg) title += `\nStatus: ${data.receiver_msg}`;
            receiverBadge.title = title;
        }
    }
    if (data.engines) {
        data.engines.forEach(eng => {
            updateStorageStatus(eng);
            const card = document.getElementById(`engine-card-${eng.id}`);
            if (card) card.dataset.state = eng.storage_blocked ? 'STORAGE WAIT' : eng.is_waiting_approval ? 'WAITING_APPROVAL' : eng.is_active ? 'SYNCING' : eng.is_paused ? 'PAUSED' : 'ACTIVE';
            const preview = document.getElementById(`engine-preview-${eng.id}`);
            if (preview) preview.textContent = eng.is_waiting_approval ? 'Review changes' : 'Preview';
            const toggle = document.getElementById(`engine-btn-toggle-${eng.id}`);
            if (toggle) {
                toggle.textContent = eng.is_paused ? 'Resume' : 'Pause';
                toggle.onclick = () => engineAction(eng.id, eng.is_paused ? 'resume' : 'pause');
            }
            const container = document.getElementById(`engine-progress-container-${eng.id}`);
            const bar = document.getElementById(`engine-progress-bar-${eng.id}`);
            const fileText = document.getElementById(`engine-current-file-${eng.id}`);
            const speedText = document.getElementById(`engine-current-speed-${eng.id}`);
            const statusPill = document.getElementById(`engine-status-${eng.id}`);
            const radar = document.getElementById(`engine-radar-${eng.id}`);
            const remoteBadge = document.getElementById(`engine-remote-${eng.id}`);
            const todayText = document.getElementById(`engine-today-${eng.id}`);
            const totalText = document.getElementById(`engine-total-${eng.id}`);
            const elapsedEl = document.getElementById(`engine-elapsed-${eng.id}`);
            const avgEl = document.getElementById(`engine-avg-${eng.id}`);
            const lastSyncEl = document.getElementById(`engine-lastsync-${eng.id}`);
            const queue = document.getElementById(`engine-queue-${eng.id}`);
            if (queue) { queue.hidden = !eng.queue_count; queue.textContent = `${eng.queue_count || 0} queued`; }

            if (lastSyncEl && eng.last_sync) {
                lastSyncEl.setAttribute('data-time', eng.last_sync);
                lastSyncEl.innerText = timeAgo(eng.last_sync);
            }
            if (todayText) todayText.innerText = eng.today;
            if (totalText) totalText.innerText = eng.total;
            if (radar) radar.hidden = !eng.is_scanning;
            if (remoteBadge) remoteBadge.hidden = !eng.is_remote_scan;
            if (statusPill) {
                if (eng.storage_blocked) {
                    statusPill.innerText = eng.is_paused ? 'PAUSED · STORAGE WAIT' : 'STORAGE WAIT';
                    statusPill.className = 'status-pill pill-waiting';
                }
                else if (eng.is_waiting_approval) {
                    statusPill.innerText = 'WAITING APPROVAL';
                    statusPill.className = 'status-pill pill-waiting';
                }
                else if (eng.is_paused) {
                    statusPill.innerText = 'PAUSED';
                    statusPill.className = 'status-pill pill-paused';
                }
                else if (eng.is_active) {
                    statusPill.innerText = 'SYNCING';
                    statusPill.className = 'status-pill pill-syncing';
                }
                else if (eng.storage_blocked) {
                    statusPill.innerText = 'STORAGE WAIT';
                    statusPill.className = 'status-pill pill-waiting';
                }
                else {
                    statusPill.innerText = 'ACTIVE';
                    statusPill.className = 'status-pill pill-active';
                }
            }
            if (container && eng.is_active) {
                container.hidden = false;
                if (bar) bar.style.width = eng.percent + '%';
                if (fileText) fileText.innerText = eng.file || '...';
                if (speedText) speedText.innerText = `${eng.speed} (${eng.eta})`;
                if (elapsedEl) elapsedEl.innerText = `Elapsed: ${eng.elapsed}`;
                if (avgEl) avgEl.innerText = `Avg: ${eng.avg_speed}`;
                const sl = document.getElementById(`sparkline-${eng.id}`);
                if (sl && eng.speed_history) { sl.setAttribute('data-history', eng.speed_history.join(',')); drawSparkline(`sparkline-${eng.id}`, eng.speed_history, '#00ffad', 1024); }
            } else if (container) container.hidden = true;
        });
        updateAttentionCount();
        filterEngines();
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

function parseDuration(str) {
    if (!str) return 0;
    let total = 0;
    const parts = str.split(' ');
    parts.forEach(p => {
        if (p.endsWith('h')) total += parseInt(p) * 3600;
        else if (p.endsWith('m')) total += parseInt(p) * 60;
        else if (p.endsWith('s')) total += parseInt(p);
    });
    return total;
}

// --- 3. WebSocket Setup ---
let socket;
let reconnectDelay = 1000;
let lastProgressAt = 0;
let connectionMessage = 'Connecting…';

function updateConnectionStatus() {
    const age = lastProgressAt ? Math.floor((Date.now() - lastProgressAt) / 1000) : null;
    const fresh = connectionMessage === 'Live' && age !== null && age < 30;
    document.documentElement.dataset.connection = fresh ? 'live' : 'stale';
    const status = document.getElementById('connection-status');
    const ageLabel = document.getElementById('connection-age');
    const logStatus = document.getElementById('log-connection-status');
    const message = fresh ? 'Live updates' : connectionMessage === 'Live' ? 'Updates delayed' : connectionMessage;
    if (status && status.textContent !== message) status.textContent = message;
    if (ageLabel) ageLabel.textContent = fresh ? '' : ` · ${age === null ? 'showing page-load data' : `last update ${age}s ago`}`;
    if (logStatus) logStatus.textContent = fresh ? 'LIVE' : 'STALE';
}

function connectWS() {
    socket = new WebSocket((window.location.protocol === 'https:' ? 'wss://' : 'ws://') + window.location.host + '/ws');

    socket.onopen = function () {
        console.log("WebSocket Connected");
        reconnectDelay = 1000; // Reset delay on success
        connectionMessage = 'Waiting for updates';
        updateConnectionStatus();
    };

    socket.onmessage = function (event) {
        try {
            const msg = JSON.parse(event.data);
            if (msg.type === 'progress') {
                lastProgressAt = Date.now();
                connectionMessage = 'Live';
                updateConnectionStatus();
                updateProgress(msg.data);
                if (msg.data.top_files) updateTopFiles(msg.data.top_files);
            }
            else if (msg.type === 'history') addHistoryItem(msg.data);
            else if (msg.type === 'log') addLogLine(msg.data);
        } catch (e) {
            if (event.data) {
                console.error("WS Message Error:", e, "Data:", event.data);
            }
        }
    };

    socket.onclose = function (e) {
        connectionMessage = 'Disconnected — reconnecting';
        updateConnectionStatus();
        console.log(`WebSocket closed: ${e.reason}. Reconnecting in ${reconnectDelay}ms...`);
        setTimeout(connectWS, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 1.5, 30000); // Exponential backoff
    };

    socket.onerror = function (err) {
        console.error("WebSocket Error:", err);
        socket.close();
    };
}

connectWS();
updateConnectionStatus();
setInterval(updateConnectionStatus, 5000);

// --- 4. Sidebar & Settings ---
function setTheme(name) {
    window.applyTheme(name);
    toast(`Theme: ${name.toUpperCase()}`, 'success');
}

function toggleWebhookVisibility() {
    const input = document.getElementById('webhook-input');
    if (input) {
        input.type = input.type === 'password' ? 'text' : 'password';
        document.getElementById('webhook-visibility').setAttribute('aria-label', input.type === 'password' ? 'Show webhook URL' : 'Hide webhook URL');
    }
}


function updateStorageStatus(eng) {
    const card = document.getElementById(`engine-card-${eng.id}`);
    if (card) card.dataset.storageBlocked = String(Boolean(eng.storage_blocked));
    const panel = document.getElementById(`storage-wait-${eng.id}`);
    if (panel) panel.hidden = !eng.storage_blocked;
    const reason = document.getElementById(`storage-reason-${eng.id}`);
    if (reason) reason.textContent = eng.storage_error || 'Waiting for the first storage check.';
    const checked = document.getElementById(`storage-checked-${eng.id}`);
    const timestamp = eng.storage_checked_at;
    if (checked) checked.textContent = !timestamp || timestamp.startsWith('0001')
        ? 'Storage not checked yet'
        : 'Last checked ' + new Date(timestamp).toLocaleString();
}

let attentionOnly = false;
function engineNeedsAttention(card) {
    return card.dataset.storageBlocked === 'true' || card.dataset.state === 'WAITING_APPROVAL' || card.dataset.state === 'CRITICAL';
}
function updateAttentionCount() {
    const count = Array.from(document.querySelectorAll('.engine-card')).filter(engineNeedsAttention).length;
    const button = document.getElementById('attention-filter');
    if (button) button.textContent = `Needs attention (${count})`;
}
function toggleAttentionFilter() {
    attentionOnly = !attentionOnly;
    document.getElementById('attention-filter')?.setAttribute('aria-pressed', String(attentionOnly));
    filterEngines();
}
function clearEngineFilters() {
    attentionOnly = false;
    const search = document.getElementById('engine-search');
    if (search) search.value = '';
    document.getElementById('attention-filter')?.setAttribute('aria-pressed', 'false');
    filterEngines();
}
function filterEngines() {
    const query = document.getElementById('engine-search')?.value.toLowerCase() || '';
    const cards = Array.from(document.querySelectorAll('.engine-card'));
    let visible = 0;
    cards.forEach(card => {
        const id = card.id.replace('engine-card-', '');
        const alias = document.getElementById(`alias-${id}`)?.innerText.toLowerCase() || '';
        const paths = Array.from(card.querySelectorAll('.path-value')).map(path => path.textContent.toLowerCase()).join(' ');
        card.hidden = !(id.includes(query) || alias.includes(query) || paths.includes(query)) || (attentionOnly && !engineNeedsAttention(card));
        if (!card.hidden) visible++;
    });
    const empty = document.getElementById('engine-empty');
    if (empty) empty.hidden = cards.length === 0 || visible > 0;
}

let policyPending = false;
function syncPolicy() {
    return {
        mode: document.getElementById('sync-mode-switch')?.getAttribute('data-val') || 'dry',
        deletions: document.getElementById('auto-approve-toggle')?.getAttribute('data-val') === 'on',
        override: document.getElementById('override-switch')?.getAttribute('data-val') === 'override'
    };
}
function refreshPolicySummary() {
    const policy = syncPolicy();
    const verb = policy.mode === 'dry' ? 'Dry run' : policy.mode === 'manual' ? 'Scan' : 'Sync';
    const mode = document.getElementById('current-mode');
    if (mode) mode.textContent = policy.mode === 'dry' ? 'Dry run' : policy.mode === 'manual' ? 'Manual' : 'Automatic';
    const summary = document.getElementById('current-policy');
    if (summary) summary.textContent = policy.mode === 'dry' ? 'No files changed' :
        (policy.mode === 'manual' ? 'Changes wait for review · ' : '') +
        (policy.deletions ? 'Deletions auto-approved' : 'Deletions require approval') + ' · ' +
        (policy.override ? 'Sender overrides conflicts' : 'Conflicts require review');
    document.querySelectorAll('[data-sync-label]').forEach(label => {
        const scope = label.dataset.syncLabel;
        label.textContent = verb + (scope === 'all' ? ' all' : scope === 'selected' ? ' selected' : '');
    });
}
function updatePolicyFromServer(data) {
    if (policyPending) return;
    const fields = [
        ['sync-mode-switch', data.sync_mode],
        ['auto-approve-toggle', data.auto_approve],
        ['override-switch', typeof data.sender_override === 'boolean' ? (data.sender_override ? 'override' : 'ask') : undefined]
    ];
    fields.forEach(([id, value]) => {
        const control = document.getElementById(id);
        if (!control || value === undefined) return;
        control.setAttribute('data-val', value);
        if (id === 'auto-approve-toggle') control.checked = value === 'on';
        else control.value = value;
    });
    refreshPolicySummary();
}
function confirmPolicyChange(title, message, action) {
    const dialog = document.getElementById('policy-confirm-modal');
    if (!dialog || dialog.open) return Promise.resolve(false);
    document.getElementById('policy-confirm-title').textContent = title;
    document.getElementById('policy-confirm-message').textContent = message;
    document.getElementById('policy-confirm-action').textContent = action;
    dialog.returnValue = 'cancel';
    return new Promise(resolve => {
        dialog.addEventListener('close', () => resolve(dialog.returnValue === 'confirm'), {once: true});
        dialog.showModal();
    });
}
async function savePolicy(control, next, endpoint, field, value, review) {
    if (!control || policyPending) return;
    const current = control.getAttribute('data-val');
    policyPending = true;
    control.disabled = true;
    const policyControls = document.querySelectorAll('#sync-policy input, #sync-policy select');
    policyControls.forEach(field => field.disabled = true);
    try {
        if (review && !(await confirmPolicyChange(...review))) return;
        const formData = new FormData();
        formData.append(field, value);
        const response = await fetch(endpoint, {method: 'POST', body: formData});
        if (!response.ok || response.redirected) throw new Error(await responseError(response));
        control.setAttribute('data-val', next);
        toast('Sync policy saved for all engines.', 'success');
    } catch (error) {
        toast(`Policy was not saved: ${error.message}`, 'error');
    } finally {
        const saved = control.getAttribute('data-val') || current;
        if (control.id === 'auto-approve-toggle') control.checked = saved === 'on';
        else control.value = saved;
        control.disabled = false;
        policyControls.forEach(field => field.disabled = false);
        policyPending = false;
        refreshPolicySummary();
        if (review) control.focus();
    }
}
async function cycleSyncMode() {
    const el = document.getElementById('sync-mode-switch');
    if (!el) return;
    const next = el.value;
    const policy = syncPolicy();
    const review = next === 'auto' ? ['Enable automatic synchronization?',
        `All engines can apply eligible changes when detected. Deletions ${policy.deletions ? 'are auto-approved' : 'require approval'}; conflicts ${policy.override ? 'use the sender version' : 'require review'}. Receiver-only top-level directories are preserved.`,
        'Enable automatic sync'] : null;
    await savePolicy(el, next, '/settings/sync-mode', 'mode', next, review);
}
async function updateAutoApprove(checkbox) {
    const next = checkbox.checked ? 'on' : 'off';
    const review = next === 'on' ? ['Auto-approve file deletions?',
        'Applies to all engines. Files missing from directories present on both servers may be deleted without a separate deletion approval. Receiver-only top-level directories are preserved. Dry run still changes no files.',
        'Enable auto-approval'] : null;
    await savePolicy(checkbox, next, '/settings/auto-approve', 'auto_approve', next, review);
}
async function cycleOverrideMode() {
    const el = document.getElementById('override-switch');
    if (!el) return;
    const next = el.value;
    const review = next === 'override' ? ['Use the sender version for conflicts?',
        'Applies to all engines. Conflicting destination files may be replaced by their source versions when changes are applied. Dry run still changes no files.',
        'Enable sender override'] : null;
    await savePolicy(el, next, '/settings/sender-override', 'enabled', next === 'override', review);
}

async function applyBwlimit(event) {
    event.preventDefault();
    const input = document.getElementById('bwlimit-input'); if (!input) return;
    const formData = new FormData(); formData.append('mbps', input.value);
    try {
        const resp = await fetch('/api/settings/bwlimit', { method: 'POST', body: formData });
        if (resp.ok && !resp.redirected) {
            toast(`Bandwidth Limit: ${input.value} Mbps`, 'success');
        } else {
            const txt = await responseError(resp);
            toast(`Error: ${txt}`, 'error');
        }
    } catch (e) {
        toast(`Request failed: ${e.message}`, 'error');
    }
}

async function saveSchedule(event) {
    event.preventDefault();
    const formData = new FormData();
    const enabled = document.getElementById('sched-enabled');
    if (enabled && enabled.checked) formData.append('scheduler_enabled', 'on');
    formData.append('quiet_start', document.getElementById('sched-quiet-start')?.value || '');
    formData.append('quiet_end', document.getElementById('sched-quiet-end')?.value || '');
    formData.append('quiet_limit', document.getElementById('sched-quiet-limit')?.value || '0');
    formData.append('normal_limit', document.getElementById('sched-normal-limit')?.value || '0');
    try {
        const resp = await fetch('/settings/scheduler', { method: 'POST', body: formData });
        if (resp.ok && !resp.redirected) {
            toast('Schedule Saved', 'success');
        } else {
            const txt = await responseError(resp);
            toast(`Error: ${txt}`, 'error');
        }
    } catch (e) {
        toast(`Request failed: ${e.message}`, 'error');
    }
}

// --- 5. Engine Actions ---
function onEngineSelect() {
    const selected = document.querySelectorAll('.engine-select:checked');
    const bar = document.getElementById('group-actions-bar');
    const count = document.getElementById('group-count');
    if (selected.length > 0) { if (count) count.innerText = selected.length; if (bar) bar.classList.add('active'); }
    else { if (bar) bar.classList.remove('active'); const master = document.getElementById('select-all-engines'); if (master) master.checked = false; }
}

function toggleAllEngines(master) {
    document.querySelectorAll('.engine-select').forEach(cb => {
        cb.checked = master.checked;
    });
    onEngineSelect();
}
function deselectAll() { const master = document.getElementById('select-all-engines'); if (master) master.checked = false; toggleAllEngines({ checked: false }); }

let bulkPending = false;
async function executeBulkAction(action) {
    if (bulkPending) return;
    const selected = Array.from(document.querySelectorAll('.engine-select:checked')).map(cb => cb.value);
    if (selected.length === 0) return;
    bulkPending = true;
    const buttons = document.querySelectorAll('.group-actions-toolbar button');
    buttons.forEach(button => button.disabled = true);
    try {
        const resp = await fetch('/api/engines/bulk', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids: selected, action: action }) });
        if (!resp.ok || resp.redirected) throw new Error(await responseError(resp));
        if (resp.ok && !resp.redirected) { toast(`${selected.length} engines: ${action} requested. Watch their status for updates.`, 'success'); if (action !== 'sync') setTimeout(() => window.location.reload(), 800); else deselectAll(); }
    } catch (e) { toast(`Bulk ${action} failed: ${e.message}`, 'error'); }
    finally { bulkPending = false; buttons.forEach(button => button.disabled = false); }
}

async function responseError(response) {
    if (response.redirected || response.status === 401) return 'Your session expired. Sign in and try again.';
    const message = await response.text();
    return message.trim().startsWith('<') ? `Server returned ${response.status}. Try again.` : message.trim().slice(0, 300) || `Server returned ${response.status}. Try again.`;
}

function engineAction(id, action) {
    fetch(`/api/engine/${id}/${action}`, { method: 'POST' })
        .then(async resp => {
            if (resp.ok && !resp.redirected) {
                const alias = document.getElementById(`alias-${id}`)?.innerText || `Engine ${id}`;
                toast(`${alias}: ${action} requested. Watch this engine for updates.`, 'success');
                if (action !== 'sync') setTimeout(() => window.location.reload(), 500);
            } else {
                const txt = await responseError(resp);
                toast(`Error: ${txt}`, 'error');
            }
        })
        .catch(e => {
            toast(`Action failed: ${e.message}`, 'error');
            console.error(e);
        });
}

async function editAlias(id) {
    const el = document.getElementById(`alias-${id}`);
    const current = el ? el.innerText : '';
    const next = prompt("Enter new alias:", current);
    if (next !== null && next.trim() !== "" && next !== current) {
        const formData = new FormData(); formData.append('alias', next.trim());
        try {
            const response = await fetch(`/api/engine/${id}/alias`, { method: 'POST', body: formData });
            if (!response.ok || response.redirected) throw new Error(await responseError(response));
            if (el) {
                el.innerText = next.trim();
                el.setAttribute('aria-label', `Rename ${next.trim()}, engine ${id}`);
            }
            const checkbox = document.querySelector(`.engine-select[value="${id}"]`);
            if (checkbox) checkbox.setAttribute('aria-label', `Select engine ${id} ${next.trim()}`);
            toast('Alias updated', 'success');
        } catch (error) { toast(`Alias was not saved: ${error.message}`, 'error'); }
    }
}

// --- 6. Modal & Preview ---
function toggleAllPreview(master) {
    document.querySelectorAll('.preview-select').forEach(cb => cb.checked = master.checked);
}

async function showPreview(id, mode = 'preview') {
    previewRequest?.abort();
    const request = new AbortController();
    previewRequest = request;
    previewReady = false;
    currentPreviewId = id;
    const modal = document.getElementById('modal-container');
    const body = document.getElementById('preview-body');
    const loading = document.getElementById('preview-loading');
    const stats = document.getElementById('preview-stats');
    const details = document.getElementById('preview-details');
    const confirmBtn = document.getElementById('preview-confirm-btn');
    const errorPanel = document.getElementById('preview-error');
    if (confirmBtn) confirmBtn.disabled = true;
    if (errorPanel) errorPanel.hidden = true;
    if (details) details.replaceChildren();
    document.getElementById('preview-id').innerText = document.getElementById(`alias-${id}`)?.innerText || id;
    if (modal && !modal.open) modal.showModal();
    if (loading) loading.style.display = 'block';
    if (body) body.style.display = 'none';
    const timeout = setTimeout(() => request.abort(), 30000);
    try {
        const resp = await fetch(`/api/engine/${encodeURIComponent(id)}/preview`, {signal: request.signal});
        if (!resp.ok || resp.redirected) {
            throw new Error(await responseError(resp));
        }
        const plan = await resp.json();
        if (previewRequest !== request || !modal.open) return;
        for (const key of ['filesToSync', 'filesToDelete', 'conflicts', 'dirsToDelete', 'dirsToCreate']) {
            if (plan[key] == null) plan[key] = [];
            if (!Array.isArray(plan[key])) throw new Error('The server returned an invalid preview. Try again.');
        }
        if (loading) loading.style.display = 'none'; if (body) body.style.display = 'block';

        let totalCount = plan.filesToSync.length + plan.filesToDelete.length + plan.conflicts.length + (plan.renames ? Object.keys(plan.renames).length : 0);
        let deleteCount = plan.filesToDelete.length + plan.dirsToDelete.length;

        if (stats) stats.innerHTML = `<div class="stat-card" style="padding:15px;"><div class="stat-label">Changes</div><div class="stat-value" style="font-size:20px;">${totalCount}</div></div><div class="stat-card" style="padding:15px;"><div class="stat-label">Sync</div><div class="stat-value" style="font-size:20px;">${plan.filesToSync.length}</div></div><div class="stat-card" style="padding:15px;"><div class="stat-label">Delete</div><div class="stat-value" style="font-size:20px; color:var(--accent-error);">${deleteCount}</div></div><div class="stat-card" style="padding:15px;"><div class="stat-label">Conflicts</div><div class="stat-value" style="font-size:20px; color:var(--accent-warning);">${plan.conflicts.length}</div></div>`;

        let html = '<table style="width:100%; border-collapse: collapse; font-size:12px;">';
        html += '<tr style="text-align:left; color:var(--text-muted); border-bottom:1px solid var(--border-glass);">';
        html += '<th style="padding:10px; width: 44px;"><input type="checkbox" aria-label="Select all preview changes" onchange="toggleAllPreview(this)" checked></th>';
        html += '<th style="padding:10px;">Action</th><th>File</th><th>Details</th></tr>';

        const renderRow = (type, path, details, badgeClass, isChecked = true) => {
            return `<tr style="border-bottom:1px solid rgba(255,255,255,0.05);">
                <td style="padding:10px;"><input type="checkbox" class="preview-select" aria-label="Select ${type}: ${escapeHtml(path)}" value="${encodeURIComponent(path)}" ${isChecked ? 'checked' : ''}></td>
                <td style="padding:10px;"><span class="action-badge ${badgeClass}">${type}</span></td>
                <td style="word-break: break-all;">${escapeHtml(path)}</td>
                <td>${details}</td>
            </tr>`;
        };

        plan.conflicts.forEach(c => {
            const isSourceNewer = new Date(c.sourceTime) > new Date(c.receiverTime);
            html += renderRow("DIFF", c.path, `<div style="font-size:12px; color:var(--accent-warning);">${isSourceNewer ? 'Sender is NEWER' : 'Sender is OLDER'}</div><div style="font-size:12px; color:var(--text-muted);">Size diff: ${formatBytes(Math.abs(c.sourceSize - c.receiverSize))}</div>`, "badge-renamed", true);
        });

        plan.filesToSync.forEach(f => {
            if (!plan.conflicts.some(c => c.path === f.path)) {
                html += renderRow("ADD", f.path, formatBytes(f.size), "badge-added", true);
            }
        });

        plan.renames = plan.renames || {};
        for (const [oldPath, newPath] of Object.entries(plan.renames)) {
            html += renderRow("MOVE", oldPath, `-> ${escapeHtml(newPath)}`, "badge-renamed", true);
        }

        plan.filesToDelete.forEach(p => {
            html += renderRow("DEL", p, "-", "badge-deleted", true);
        });

        plan.dirsToDelete.forEach(p => {
            html += renderRow("DEL-DIR", p, "-", "badge-deleted", true);
        });

        if (plan.dirsToCreate) {
            plan.dirsToCreate.forEach(p => {
                html += renderRow("ADD-DIR", p, "-", "badge-added", true);
            });
        }

        html += '</table>';
        if (details) details.innerHTML = html;
        previewReady = true;
        if (confirmBtn) confirmBtn.disabled = !document.querySelector('.preview-select');
    } catch (e) {
        if (previewRequest !== request || !modal.open) return;
        if (body) body.style.display = 'none';
        if (errorPanel) errorPanel.hidden = false;
        document.getElementById('preview-error-message').textContent = e.name === 'AbortError'
            ? 'The preview took too long. Check storage and connection status, then try again.'
            : `Could not load preview. ${e.message}`;
    } finally {
        clearTimeout(timeout);
        if (previewRequest === request && loading) loading.style.display = 'none';
    }
}

function closeModal() { document.getElementById('modal-container')?.close(); }
document.getElementById('modal-container')?.addEventListener('close', () => {
    previewRequest?.abort();
    previewRequest = null;
    previewReady = false;
});

let sharedTokenRequest = null;
let sharedTokenOpener = null;
let sharedTokenScroll = '';
let sharedTokenScrollLocked = false;

function positionSharedToken() {
    const dialog = document.getElementById('shared-token-modal');
    const viewport = window.visualViewport;
    if (!dialog || !viewport) return;
    dialog.style.left = `${viewport.offsetLeft + viewport.width / 2}px`;
    dialog.style.top = `${viewport.offsetTop + viewport.height / 2}px`;
    dialog.style.maxWidth = `${viewport.width - 32}px`;
    dialog.style.maxHeight = `${viewport.height - 32}px`;
}
window.visualViewport?.addEventListener('resize', positionSharedToken);
window.visualViewport?.addEventListener('scroll', positionSharedToken);

async function showSharedToken(id, opener) {
    const dialog = document.getElementById('shared-token-modal');
    const content = document.getElementById('shared-token-content');
    const status = document.getElementById('shared-token-status');
    if (!dialog || !content || !status) return;
    if (sharedTokenRequest) sharedTokenRequest.abort();
    const request = new AbortController();
    sharedTokenRequest = request;
    sharedTokenOpener = opener;
    content.hidden = true;
    document.getElementById('shared-token-value').value = '';
    status.textContent = 'Loading token…';
    status.dataset.error = 'false';
    if (!dialog.open) {
        if (!sharedTokenScrollLocked) {
            sharedTokenScroll = document.body.style.overflow;
            document.body.style.overflow = 'hidden';
            sharedTokenScrollLocked = true;
        }
        positionSharedToken();
        dialog.showModal();
    }
    const timeout = setTimeout(() => request.abort(), 10000);
    try {
        const response = await fetch(`/api/engine/${encodeURIComponent(id)}/shared-token`, { signal: request.signal, cache: 'no-store' });
        if (!response.ok || response.redirected) throw new Error('Token unavailable');
        const data = await response.json();
        if (!/^[a-f0-9]{64}$/.test(data.token)) throw new Error('Invalid token');
        if (sharedTokenRequest !== request || !dialog.open) return;
        document.getElementById('shared-token-value').value = data.token;
        document.getElementById('shared-token-engine').textContent = data.engine;
        document.getElementById('shared-token-source').textContent = data.source;
        document.getElementById('shared-token-target').textContent = data.target;
        content.hidden = false;
        status.textContent = 'Use this token in both folders.';
    } catch (error) {
        if (sharedTokenRequest !== request || !dialog.open) return;
        status.dataset.error = 'true';
        status.textContent = 'Couldn’t load the token. Close this dialog and try again. If your session expired, sign in first.';
    } finally {
        clearTimeout(timeout);
    }
}

function closeSharedToken() { document.getElementById('shared-token-modal')?.close(); }

async function copySharedToken() {
    const field = document.getElementById('shared-token-value');
    const status = document.getElementById('shared-token-status');
    if (!field?.value) return;
    try {
        await navigator.clipboard.writeText(field.value);
        status.textContent = 'Token copied.';
    } catch {
        field.focus();
        field.select();
        status.textContent = 'Token selected. Use your browser’s Copy command.';
    }
}

function downloadSharedToken() {
    const token = document.getElementById('shared-token-value')?.value;
    if (!token) return;
    const url = URL.createObjectURL(new Blob([token + '\n'], { type: 'text/plain' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = '.schnorarr-shared-token';
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    document.getElementById('shared-token-status').textContent = 'Save the downloaded file in both folders.';
}

document.getElementById('shared-token-modal')?.addEventListener('close', () => {
    if (document.getElementById('shared-token-modal').open) return;
    sharedTokenRequest?.abort();
    sharedTokenRequest = null;
    document.body.style.overflow = sharedTokenScroll;
    sharedTokenScrollLocked = false;
    sharedTokenOpener?.focus({ preventScroll: true });
});

async function confirmSyncFromPreview() {
    if (!currentPreviewId || !previewReady) return;

    // Gather selected files
    const selected = Array.from(document.querySelectorAll('.preview-select:checked')).map(cb => decodeURIComponent(cb.value));
    if (selected.length === 0) {
        toast("No changes selected", "warning");
        return;
    }

    const btn = document.getElementById('preview-confirm-btn');
    if (btn) btn.disabled = true;

    try {
        const resp = await fetch(`/api/engine/${currentPreviewId}/approve-list`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ files: selected })
        });

        if (resp.ok && !resp.redirected) {
            const alias = document.getElementById(`alias-${currentPreviewId}`)?.innerText || `Engine ${currentPreviewId}`;
            toast(`${alias}: selected changes submitted. Watch this engine for updates.`, "success");
            closeModal();
            setTimeout(() => window.location.reload(), 1000);
        } else {
            toast("Failed to start sync", "error");
        }
    } catch (e) {
        toast("Request failed", "error");
    } finally {
        if (btn) btn.disabled = false;
    }
}
// --- 7. UI Helpers ---
function formatBytes(b) { b = Math.abs(b); if (b === 0) return '0 B'; const k = 1024, s = ['B', 'KB', 'MB', 'GB', 'TB'], i = Math.floor(Math.log(b) / Math.log(k)); return parseFloat((b / Math.pow(k, i)).toFixed(2)) + ' ' + s[i]; }
function parseBytes(str) {
    if (!str) return 0;
    const parts = str.trim().split(/\s+/);
    if (parts.length < 2) return 0;
    const val = parseFloat(parts[0]);
    if (isNaN(val)) return 0;
    const unit = parts[1].toUpperCase();
    if (unit.includes('K')) return val * 1024;
    if (unit.includes('M')) return val * 1024 * 1024;
    if (unit.includes('G')) return val * 1024 * 1024 * 1024;
    if (unit.includes('T')) return val * 1024 * 1024 * 1024 * 1024;
    if (unit.includes('P')) return val * 1024 * 1024 * 1024 * 1024 * 1024;
    if (unit.includes('E')) return val * 1024 * 1024 * 1024 * 1024 * 1024 * 1024;
    return val;
}
function toast(msg, type = 'info') {
    const container = document.getElementById('toast-container');
    if (!container) return;
    const notice = document.createElement('div');
    notice.className = 'toast';
    notice.dataset.type = type;
    const text = document.createElement('span');
    text.setAttribute('role', type === 'error' ? 'alert' : 'status');
    const dismiss = document.createElement('button');
    dismiss.type = 'button';
    dismiss.className = 'copy-btn';
    dismiss.textContent = 'Dismiss';
    dismiss.addEventListener('click', () => notice.remove());
    notice.append(text, dismiss);
    container.appendChild(notice);
    text.textContent = msg;
    if (type !== 'error') setTimeout(() => notice.remove(), 8000);
}
function toggleLogScroll() {
    logScrollLocked = !logScrollLocked;
    const btn = document.getElementById('log-scroll-toggle');
    if (btn) {
        btn.textContent = logScrollLocked ? 'Resume scrolling' : 'Pause scrolling';
        btn.setAttribute('aria-pressed', String(logScrollLocked));
    }
}
function clearLogs() { const logContainer = document.getElementById('log-container'); if (logContainer) logContainer.textContent = 'Displayed logs cleared. New log entries will appear here.'; }

function toggleTerminalFullscreen() {
    const term = document.querySelector('.terminal-window');
    if (term) {
        const expanded = term.classList.toggle('fullscreen');
        const button = document.getElementById('log-fullscreen-toggle');
        if (button) {
            button.textContent = expanded ? 'Collapse logs' : 'Expand logs';
            button.setAttribute('aria-pressed', String(expanded));
        }
    }
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
    const actionClass = data.action.toLowerCase().trim().replace(/\s+/g, '-');
    li.innerHTML = `<span class="action-badge badge-${actionClass}">${escapeHtml(data.action)}</span>
        <div class="activity-description">${escapeHtml(data.path)}
            <span style="color: var(--text-muted); font-size: 13px;">(${escapeHtml(data.size || '0 B')})</span>
        </div><span class="activity-time">${escapeHtml(data.time || new Date().toLocaleTimeString())}</span>`;
    list.insertBefore(li, list.firstChild);
    if (list.childNodes.length > 15) list.removeChild(list.lastChild);

    // Mirror to live log
    addLogLine({
        msg: `[Event] ${data.action}: ${data.path}`,
        level: 'info'
    });
}

document.addEventListener('DOMContentLoaded', () => {
    refreshPolicySummary();
    updateAttentionCount();
    const grid = document.querySelector('.engine-grid');
    if (grid) {
        const rank = card => engineNeedsAttention(card) ? 0 : card.dataset.state === 'SYNCING' ? 1 : card.dataset.state === 'PAUSED' ? 3 : 2;
        Array.from(grid.querySelectorAll('.engine-card')).sort((a, b) => rank(a) - rank(b)).forEach(card => grid.appendChild(card));
    }
    document.querySelectorAll('.sparkline-container').forEach(sl => {
        const histStr = sl.getAttribute('data-history') || "";
        const history = histStr ? histStr.split(',').map(Number) : [];
        if (history.length >= 2) {
            let color = '#00ffad';
            let minMax = 1024;
            if (sl.id === 'speed-sparkline') { color = 'var(--accent-primary)'; minMax = 10 * 1024 * 1024; }
            else if (sl.id === 'latency-sparkline') { color = '#ffb300'; minMax = 100; }
            drawSparkline(sl.id, history, color, minMax);
        }
    });

    updateRelativeTimes();
    setInterval(updateRelativeTimes, 30000);
});

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
        if (!modal.open) modal.showModal();
    }
}

function closeErrorModal() {
    const modal = document.getElementById('error-modal');
    if (modal) modal.close();
}
