const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function dashboard(fetch = async () => ({ ok: false, status: 503, statusText: 'Service Unavailable', text: async () => 'Storage unavailable' })) {
    const elements = new Map();
    const listeners = new Map();
    function element(id) {
        if (!elements.has(id)) elements.set(id, {
            id, style: {}, dataset: {}, hidden: false, disabled: false, open: false,
            innerText: '', textContent: '', innerHTML: '', value: '', children: [], handlers: {},
            classList: { add() {}, remove() {}, toggle() {} },
            setAttribute(name, value) { this[name] = value; },
            getAttribute(name) { return this[name]; },
            querySelectorAll() { return this.children; },
            querySelector() { return this.children.find(child => child.checked) || null; },
            addEventListener(name, callback) { this.handlers[name] = callback; },
            appendChild(child) { this.children.push(child); },
            append(...children) { this.children.push(...children); },
            replaceChildren(...children) { this.children = children; },
            showModal() { this.open = true; },
            close() { this.open = false; this.handlers.close?.(); },
            focus() {}, remove() {},
        });
        return elements.get(id);
    }
    const document = {
        getElementById: element, documentElement: element('root'), body: element('body'),
        activeElement: element('opener'), hidden: false,
        querySelectorAll: selector => selector === '.engine-select:checked' ? [{value: '1'}] : [],
        querySelector: () => null,
        addEventListener() {}, createElement: () => element(`created-${elements.size}`),
    };
    const window = { location: { protocol: 'http:', host: 'fixture', href: '/' }, addEventListener: (key, fn) => listeners.set(key, fn) };
    const context = vm.createContext({ document, window, console, fetch, FormData, AbortController,
        WebSocket: class { close() {} }, Notification: { permission: 'denied' },
        setTimeout: () => 1, clearTimeout() {}, setInterval: () => 1, requestAnimationFrame: () => 1,
        localStorage: {getItem() {return null;}, setItem() {}}, navigator: {},
    });
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../internal/ui/web/static/js/dashboard.js'), 'utf8'), context);
    const notices = [];
    context.toast = (message, type) => notices.push({ message, type });
    return {context, element, listeners, notices};
}

test('top files escapes markup in both paths and sizes', () => {
    const {context, element} = dashboard();
    context.updateTopFiles([{
        path: '<img src=x onerror=alert(1)>.mkv',
        size: '<svg onload=alert(2)>',
    }]);
    const html = element('top-files-list').innerHTML;
    assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;\.mkv/);
    assert.match(html, /\(&lt;svg onload=alert\(2\)&gt;\)/);
    assert.doesNotMatch(html, /<(?:img|svg)\b/);
});

test('top files renders formatted sizes and the empty state', () => {
    const {context, element} = dashboard();
    context.updateTopFiles([{path: 'Movie.mkv', size: '1.5 GB'}]);
    assert.match(element('top-files-list').innerHTML, /Movie\.mkv/);
    assert.match(element('top-files-list').innerHTML, /\(1\.5 GB\)/);
    context.updateTopFiles([]);
    assert.match(element('top-files-list').innerHTML, /No completed transfers/);
});

test('failed preview stops loading, exposes recovery, and cannot execute', async () => {
    const {context, element} = dashboard();
    await context.showPreview('1');
    assert.equal(element('preview-loading').style.display, 'none');
    assert.equal(element('preview-error').hidden, false);
    assert.match(element('preview-error-message').textContent, /preview|storage/i);
    assert.equal(element('preview-confirm-btn').disabled, true);
});

test('bulk HTTP failure reports an actionable error', async () => {
    const {context, notices} = dashboard();
    await context.executeBulkAction('pause');
    assert.equal(notices.length, 1);
    assert.equal(notices[0].type, 'error');
    assert.match(notices[0].message, /storage|pause|failed/i);
});

test('single-character S and P cannot trigger operations', () => {
    const {context, listeners} = dashboard();
    for (const key of ['s', 'p']) listeners.get('keydown')?.({key, target: {tagName: 'BODY', closest: () => null}, preventDefault() {}});
    assert.equal(context.window.location.href, '/');
});

test('disconnected telemetry is visibly stale', () => {
    const {context, element} = dashboard();
    vm.runInContext('socket.onclose({reason: "network lost"})', context);
    assert.match(element('connection-status').textContent, /reconnect|disconnect/i);
    assert.equal(element('root').dataset.connection, 'stale');
});

test('a superseded preview response cannot replace the current plan', async () => {
    const resolvers = [];
    const {context, element} = dashboard(() => new Promise(resolve => resolvers.push(resolve)));
    const first = context.showPreview('1');
    const second = context.showPreview('2');
    resolvers[1]({ok: false, status: 503, statusText: 'Second unavailable', text: async () => 'Second unavailable'});
    await second;
    const message = element('preview-error-message').textContent;
    resolvers[0]({ok: false, status: 503, statusText: 'First unavailable', text: async () => 'First unavailable'});
    await first;
    assert.ok(message.length > 0);
    assert.equal(element('preview-error-message').textContent, message);
});

test('successful preview escapes paths and permits selection', async () => {
    const {context, element} = dashboard(async () => ({ok: true, json: async () => ({
        filesToSync: [{path: 'Movies/<img src=x onerror=alert(1)>.mkv', size: 1024}],
        filesToDelete: null, conflicts: null, dirsToDelete: null,
    })}));
    context.document.querySelector = selector => selector === '.preview-select' ? {} : null;
    await context.showPreview('1');
    assert.equal(element('preview-confirm-btn').disabled, false);
    assert.match(element('preview-details').innerHTML, /&lt;img/);
    assert.doesNotMatch(element('preview-details').innerHTML, /<img/);
});

test('closing a loading preview cancels it and ignores its late response', async () => {
    let finish;
    let signal;
    const {context, element} = dashboard((url, options) => {
        signal = options.signal;
        return new Promise(resolve => finish = resolve);
    });
    const request = context.showPreview('1');
    context.closeModal();
    assert.equal(signal.aborted, true);
    finish({ok: false, status: 503, text: async () => 'Late failure'});
    await request;
    assert.equal(element('modal-container').open, false);
    assert.equal(element('preview-error').hidden, true);
});

test('repeated bulk clicks send only one pending request', async () => {
    let count = 0;
    let finish;
    const {context} = dashboard(() => { count++; return new Promise(resolve => finish = resolve); });
    const request = context.executeBulkAction('pause');
    await context.executeBulkAction('pause');
    assert.equal(count, 1);
    finish({ok: false, status: 503, text: async () => 'Unavailable'});
    await request;
});

test('enabling deletion approval requires confirmation and cancel sends no request', async () => {
    let requests = 0;
    const {context, element} = dashboard(async () => { requests++; return {ok: true}; });
    context.confirmPolicyChange = async () => false;
    const checkbox = element('auto-approve-toggle');
    checkbox.checked = true;
    checkbox.setAttribute('data-val', 'off');
    await context.updateAutoApprove(checkbox);
    assert.equal(requests, 0);
    assert.equal(checkbox.checked, false);
    assert.equal(checkbox.disabled, false);
});

test('failed policy save retains the confirmed mode and summary', async () => {
    const {context, element} = dashboard();
    context.confirmPolicyChange = async () => true;
    element('sync-mode-switch').setAttribute('data-val', 'dry');
    const dry = element('mode-dry'); dry.value = 'dry'; dry.checked = false;
    const auto = element('mode-auto'); auto.value = 'auto'; auto.checked = true;
    element('sync-mode-switch').children = [dry, auto];
    await context.cycleSyncMode('auto');
    assert.equal(dry.checked, true);
    assert.equal(auto.checked, false);
    assert.equal(element('sync-mode-switch').getAttribute('data-val'), 'dry');
    assert.equal(element('current-mode').textContent, 'Dry run');
});

test('server policy updates select the matching segment without a request', () => {
    const {context, element} = dashboard(() => { throw new Error('Unexpected policy write'); });
    const ask = element('conflict-ask'); ask.value = 'ask'; ask.checked = true;
    const override = element('conflict-override'); override.value = 'override'; override.checked = false;
    element('override-switch').children = [ask, override];
    context.updatePolicyFromServer({sender_override: true});
    assert.equal(ask.checked, false);
    assert.equal(override.checked, true);
});

test('endpoint motion follows transferring, paused, blocked, and idle states', () => {
    const {context, element} = dashboard();
    const cases = [
        {is_active: true, expected: 'true'},
        {is_active: true, is_paused: true, expected: 'false'},
        {is_active: true, storage_blocked: true, expected: 'false'},
        {is_active: true, is_waiting_approval: true, expected: 'false'},
        {is_active: false, expected: 'false'},
    ];
    for (const state of cases) {
        context.updateEndpointFlow({id: '1', ...state});
        assert.equal(element('flow-1').dataset.active, state.expected);
    }
});

test('card state prioritizes storage, approval, and pause over active transfers', () => {
    const {context, element} = dashboard();
    const cases = [
        {is_active: true, is_paused: true, expected: 'PAUSED'},
        {is_active: true, is_paused: true, storage_blocked: true, expected: 'STORAGE WAIT'},
        {is_active: true, is_paused: true, is_waiting_approval: true, expected: 'WAITING_APPROVAL'},
        {is_active: true, expected: 'SYNCING'},
        {is_active: false, expected: 'ACTIVE'},
    ];
    for (const state of cases) {
        context.updateProgress({engines: [{id: '1', ...state}]});
        assert.equal(element('engine-card-1').dataset.state, state.expected);
    }
});

test('storage recovery clears the visible failure without inventing an unchecked timestamp', () => {
    const {context, element} = dashboard();
    context.updateStorageStatus({id: '1', storage_blocked: true, storage_error: '', storage_checked_at: '0001-01-01T00:00:00Z'});
    assert.match(element('storage-checked-1').textContent, /not checked/i);
    context.updateStorageStatus({id: '1', storage_blocked: true, storage_error: 'Destination storage: marker missing', storage_checked_at: '2026-09-09T20:00:00Z'});
    assert.equal(element('storage-reason-1').textContent, 'Destination storage: marker missing');
    assert.equal(element('storage-wait-1').hidden, false);
    context.updateStorageStatus({id: '1', storage_blocked: false, storage_error: '', storage_checked_at: '2026-09-09T20:01:00Z'});
    assert.equal(element('storage-wait-1').hidden, true);
    assert.equal(element('storage-reason-1').textContent, 'Waiting for the first storage check.');
});
