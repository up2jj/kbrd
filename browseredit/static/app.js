(() => {
  'use strict';
  const token = document.body.dataset.token;
  const root = `/s/${token}`;
  const status = document.querySelector('#status');
  const saveButton = document.querySelector('#save');
  const exitButton = document.querySelector('#exit');
  const banner = document.querySelector('#banner');
  const linkCompletion = document.querySelector('#link-completion');
  const bytes = crypto.getRandomValues(new Uint8Array(18));
  const clientID = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
  const leaseKey = `kbrd-editor-lease:${token}`;
  let lease = sessionStorage.getItem(leaseKey) || '';
  let editor;
  let documentState;
  let latestDisk;
  let writer = false;
  let dirty = false;
  let loading = true;
  let applyingRemote = false;
  let readOnlyState = false;
  let recoveryPending = false;
  let recoveryDraftBody = '';
  let lastEditBody = '';
  let linkTargets = [];
  let linkQuery = '';
  let linkMatches = [];
  let linkSelected = 0;
  let draftTimer;
  let mutation = Promise.resolve();

  function setStatus(text, state = text) {
    status.textContent = text;
    status.dataset.state = state;
  }
  function showBanner(message, actions = []) {
    banner.replaceChildren(document.createTextNode(message));
    for (const [label, fn] of actions) {
      const button = document.createElement('button');
      button.type = 'button'; button.textContent = label; button.addEventListener('click', fn);
      banner.appendChild(document.createElement('br')); banner.appendChild(button);
    }
    banner.hidden = false;
  }
  function hideBanner() { banner.hidden = true; banner.replaceChildren(); }
  async function request(path, options = {}) {
    const headers = new Headers(options.headers || {});
    if (lease) headers.set('X-Kbrd-Editor-Lease', lease);
    if (options.body) headers.set('Content-Type', 'application/json');
    const response = await fetch(root + path, {...options, headers});
    let data;
    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json')) data = await response.json();
    else data = await response.text();
    if (!response.ok) { const error = new Error(typeof data === 'string' ? data : (data.message || response.statusText)); error.response = response; error.data = data; throw error; }
    return data;
  }
  function enqueue(fn) {
    mutation = mutation.then(fn, fn);
    return mutation;
  }
  function body() { return editor ? editor.getMarkdown() : ''; }
  function closeLinkCompletion() {
    linkCompletion.hidden = true;
    linkCompletion.replaceChildren();
    linkQuery = '';
    linkMatches = [];
    linkSelected = 0;
  }
  function textBeforeCaret() {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0 || !selection.isCollapsed) return '';
    const range = selection.getRangeAt(0);
    const editorRoot = document.querySelector('#editor');
    if (!editorRoot.contains(range.endContainer)) return '';
    const element = range.endContainer.nodeType === Node.ELEMENT_NODE ? range.endContainer : range.endContainer.parentElement;
    const block = element && element.closest('p,li,h1,h2,h3,h4,h5,h6,blockquote,td,th,.ProseMirror > div');
    if (!block) return '';
    const before = range.cloneRange();
    before.setStart(block, 0);
    return before.toString();
  }
  function positionLinkCompletion() {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0) return;
    const range = selection.getRangeAt(0).cloneRange();
    range.collapse(false);
    const rect = range.getBoundingClientRect();
    const menu = linkCompletion.getBoundingClientRect();
    const left = Math.min(Math.max(8, rect.left), Math.max(8, innerWidth - menu.width - 8));
    let top = rect.bottom + 6;
    if (top + menu.height > innerHeight - 8) top = Math.max(8, rect.top - menu.height - 6);
    linkCompletion.style.left = `${left}px`;
    linkCompletion.style.top = `${top}px`;
  }
  function renderLinkCompletion(query) {
    const normalized = query.toLowerCase();
    if (query !== linkQuery) linkSelected = 0;
    linkQuery = query;
    linkMatches = linkTargets
      .filter(target => target.name.toLowerCase().includes(normalized))
      .sort((a, z) => Number(!a.name.toLowerCase().startsWith(normalized)) - Number(!z.name.toLowerCase().startsWith(normalized)))
      .slice(0, 8);
    linkSelected = Math.min(linkSelected, Math.max(0, linkMatches.length - 1));
    linkCompletion.replaceChildren();
    if (linkMatches.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'link-completion-empty';
      empty.textContent = 'No matching cards';
      linkCompletion.appendChild(empty);
    } else {
      linkMatches.forEach((target, index) => {
        const option = document.createElement('button');
        option.type = 'button';
        option.className = 'link-completion-option';
        option.setAttribute('role', 'option');
        option.setAttribute('aria-selected', index === linkSelected ? 'true' : 'false');
        const name = document.createElement('strong');
        name.textContent = target.name;
        const column = document.createElement('span');
        column.textContent = target.column;
        option.append(name, column);
        option.addEventListener('mousedown', event => {
          event.preventDefault();
          insertLinkTarget(index);
        });
        linkCompletion.appendChild(option);
      });
    }
    linkCompletion.hidden = false;
    requestAnimationFrame(positionLinkCompletion);
  }
  function syncLinkCompletion() {
    if (!writer || recoveryPending || readOnlyState || linkTargets.length === 0) {
      closeLinkCompletion();
      return;
    }
    const match = textBeforeCaret().match(/\[\[([^\[\]|\n]{0,120})$/);
    if (!match) {
      closeLinkCompletion();
      return;
    }
    renderLinkCompletion(match[1]);
  }
  function insertLinkTarget(index = linkSelected) {
    const target = linkMatches[index];
    if (!target || !editor) return;
    const selection = editor.getSelection();
    const end = selection[1];
    const typedLength = linkQuery.length + 2;
    let start;
    if (Array.isArray(end)) start = [end[0], Math.max(0, end[1] - typedLength)];
    else start = Math.max(1, end - typedLength);
    closeLinkCompletion();
    editor.replaceSelection(`[[${target.name}]]`, start, end);
  }
  function moveLinkSelection(delta) {
    if (linkMatches.length === 0) return;
    linkSelected = (linkSelected + delta + linkMatches.length) % linkMatches.length;
    const options = linkCompletion.querySelectorAll('.link-completion-option');
    options.forEach((option, index) => option.setAttribute('aria-selected', index === linkSelected ? 'true' : 'false'));
    options[linkSelected].scrollIntoView({block:'nearest'});
  }
  function applyDocument(doc) {
    closeLinkCompletion();
    documentState = doc; latestDisk = doc;
    if (!editor) return;
    applyingRemote = true;
    editor.setMarkdown(doc.body || '', false);
    applyingRemote = false;
    lastEditBody = doc.body || '';
    dirty = false;
    saveButton.disabled = recoveryPending || !writer || !doc.wysiwygSafe;
    if (recoveryPending) setStatus('recovery choice required', 'readonly');
    else if (writer) setStatus('saved', 'saved');
    else setStatus('read-only', 'readonly');
  }
  async function persistDraft(allowRecovery = false) {
    if (!writer || (recoveryPending && !allowRecovery) || !dirty || !documentState) return;
    setStatus('saving draft', 'draft');
    const result = await request('/draft', {method:'PUT', body:JSON.stringify({baseRevision:documentState.revision, body:body()})});
    if (result.stale) { latestDisk = result.document; conflict(); }
    else setStatus('draft saved locally', 'draft');
    return result;
  }
  function scheduleDraft() {
    clearTimeout(draftTimer);
    draftTimer = setTimeout(() => enqueue(persistDraft).catch(offline), 650);
  }
  async function save() {
    if (!writer || recoveryPending || !documentState) return;
    clearTimeout(draftTimer);
    setStatus('saving', 'saving');
    try {
      const result = await request('/save', {method:'POST', body:JSON.stringify({baseRevision:documentState.revision, body:body()})});
      applyDocument(result.document); hideBanner();
    } catch (error) {
      if (error.response && error.response.status === 412) { latestDisk = error.data.document; conflict(); }
      else offline(error);
    }
  }
  async function exitEditor() {
    clearTimeout(draftTimer);
    exitButton.disabled = true;
    if (writer && dirty && !recoveryPending) {
      try {
        await persistDraft();
      } catch (error) {
        exitButton.disabled = false;
        offline(error);
        showBanner('The recovery draft could not be saved. The editor was left open so your changes are not lost.');
        return;
      }
    }
    setStatus('closing', 'readonly');
    if (writer && lease) {
      try {
        await request('/close', {method:'POST'});
      } catch (error) {
        console.error(error);
      }
      writer = false;
      lease = '';
      sessionStorage.removeItem(leaseKey);
    }
    saveButton.disabled = true;
    setEditorReadOnly(true);
    window.close();
    setTimeout(() => {
      setStatus('ready to close', 'readonly');
      showBanner('The browser could not close this tab automatically. You can close it now.');
    }, 250);
  }
  function conflict() {
    setStatus('conflict', 'conflict');
    showBanner('The card changed on disk while this browser draft has edits.', [
      ['Reload disk', () => enqueue(async () => { const doc = await request('/draft', {method:'DELETE'}); applyDocument(doc); hideBanner(); })],
      ['Keep my body', () => { if (latestDisk) documentState = latestDisk.document || latestDisk; enqueue(save); }],
      ['Stay conflicted', hideBanner]
    ]);
  }
  function offline(error) { console.error(error); setStatus('offline', 'offline'); }
  function makeReadOnly(message) {
    closeLinkCompletion();
    writer = false; saveButton.disabled = true;
    setEditorReadOnly(true);
    setStatus('read-only', 'readonly');
    if (message) showBanner(message);
  }
  function setEditorReadOnly(value) {
    readOnlyState = value;
    if (!editor) return;
    if (value) editor.blur();
    const editorRoot = document.querySelector('#editor');
    new MutationObserver(() => {
      if (readOnlyState) queueMicrotask(() => setEditorReadOnly(true));
    }).observe(editorRoot, {childList:true, subtree:true});
    editorRoot.classList.toggle('kbrd-readonly', value);
    editorRoot.querySelectorAll('[contenteditable]').forEach(el => el.setAttribute('contenteditable', value ? 'false' : 'true'));
    editorRoot.querySelectorAll('.toastui-editor-toolbar button').forEach(button => { button.disabled = value; });
  }
  async function claim() {
    try {
      const result = await request('/claim', {method:'POST', body:JSON.stringify({clientId:clientID})});
      lease = result.lease; sessionStorage.setItem(leaseKey, lease); writer = true;
      await request('/heartbeat', {method:'POST'});
    } catch (error) {
      if (error.response && error.response.status === 409) { lease = ''; sessionStorage.removeItem(leaseKey); makeReadOnly('Already open in another browser tab. This tab is a read-only observer.'); return; }
      throw error;
    }
  }
  async function recover(draftBody) {
    applyingRemote = true; editor.setMarkdown(draftBody, false); applyingRemote = false;
    dirty = true;
    const result = await persistDraft(true);
    recoveryPending = false;
    recoveryDraftBody = '';
    setEditorReadOnly(false);
    saveButton.disabled = false;
    if (!result.stale) { hideBanner(); setStatus('unsaved', 'unsaved'); }
  }
  async function discardRecovery() {
    const doc = await request('/draft', {method:'DELETE'});
    recoveryPending = false;
    recoveryDraftBody = '';
    applyDocument(doc);
    setEditorReadOnly(false);
    hideBanner();
  }
  function showRecoveryPrompt() {
    showBanner('A recovery draft from an earlier editing session is available.', [['Recover', () => enqueue(() => recover(recoveryDraftBody)).catch(offline)], ['Discard', () => enqueue(discardRecovery).catch(offline)]]);
  }
  function connectEvents() {
    const events = new EventSource(root + '/events');
    events.addEventListener('document', event => {
      const doc = JSON.parse(event.data);
      latestDisk = doc;
      if (!dirty || !writer) applyDocument(doc);
    });
    events.addEventListener('conflict', event => { latestDisk = JSON.parse(event.data); if (writer) conflict(); });
    events.addEventListener('handoff', () => enqueue(async () => {
      makeReadOnly('Ownership is moving to the TUI. Your recovery draft is being flushed.');
      writer = true;
      if (dirty) await persistDraft();
      await request('/handoff-ready', {method:'POST'});
      makeReadOnly('Ownership moved to the TUI.');
    }).catch(offline));
    events.addEventListener('resume', event => {
      const doc = JSON.parse(event.data);
      writer = true;
      if (recoveryPending) {
        setEditorReadOnly(true);
        saveButton.disabled = true;
        setStatus('recovery choice required', 'readonly');
        showRecoveryPrompt();
        return;
      }
      setEditorReadOnly(false);
      saveButton.disabled = !doc.wysiwygSafe;
      hideBanner();
      setStatus(dirty ? 'unsaved' : 'saved', dirty ? 'unsaved' : 'saved');
    });
    events.addEventListener('invalidated', event => { events.close(); const data = JSON.parse(event.data); makeReadOnly(data.reason || 'This editor session ended.'); });
    events.onerror = () => setStatus('offline', 'offline');
  }
  async function init() {
    if (!window.toastui || !window.toastui.Editor) throw new Error('embedded editor failed to load');
    await claim();
    const doc = await request('/document');
    documentState = doc; latestDisk = doc;
    linkTargets = Array.isArray(doc.linkTargets) ? doc.linkTargets.filter(target => target && typeof target.name === 'string' && typeof target.column === 'string') : [];
    editor = new toastui.Editor({
      el: document.querySelector('#editor'), height:'calc(100vh - 130px)', initialValue:doc.body || '',
      initialEditType: doc.wysiwygSafe ? 'wysiwyg' : 'markdown', previewStyle:'vertical', usageStatistics:false,
      toolbarItems:[['heading','bold','italic','strike'],['hr','quote'],['ul','ol','task'],['table','link'],['code','codeblock']]
    });
    lastEditBody = doc.body || '';
    const editorRoot = document.querySelector('#editor');
    editorRoot.addEventListener('input', () => queueMicrotask(syncLinkCompletion), true);
    editorRoot.addEventListener('keydown', event => {
      if (linkCompletion.hidden) return;
      switch (event.key) {
      case 'ArrowDown': if (linkMatches.length > 0) { event.preventDefault(); moveLinkSelection(1); } break;
      case 'ArrowUp': if (linkMatches.length > 0) { event.preventDefault(); moveLinkSelection(-1); } break;
      case 'Enter':
      case 'Tab': if (linkMatches.length > 0) { event.preventDefault(); insertLinkTarget(); } else closeLinkCompletion(); break;
      case 'Escape': event.preventDefault(); closeLinkCompletion(); break;
      }
    }, true);
    editorRoot.addEventListener('click', event => {
      if (event.target.closest('.toastui-editor-toolbar')) {
        closeLinkCompletion();
        return;
      }
      if (event.target.closest('.tab-item, .toastui-editor-md-tab-container, .toastui-editor-ww-tab-container')) {
        applyingRemote = true;
        setTimeout(() => { applyingRemote = false; setEditorReadOnly(readOnlyState); }, 0);
      }
      setTimeout(syncLinkCompletion, 0);
    }, true);
    editor.on('changeMode', () => {
      closeLinkCompletion();
      setTimeout(() => setEditorReadOnly(readOnlyState), 0);
    });
    editor.on('change', () => {
      if (loading || applyingRemote || !writer || recoveryPending) return;
      const currentBody = body();
      if (currentBody === lastEditBody) return;
      lastEditBody = currentBody;
      dirty = true; setStatus('unsaved', 'unsaved'); scheduleDraft();
      setTimeout(syncLinkCompletion, 0);
    });
    recoveryPending = writer && doc.wysiwygSafe && doc.draftPresent;
    recoveryDraftBody = recoveryPending ? doc.draftBody : '';
    loading = false;
    if (!writer || !doc.wysiwygSafe) {
      makeReadOnly(doc.warning || 'Already open in another browser tab.');
      setTimeout(() => setEditorReadOnly(true), 0);
    }
    else if (recoveryPending) {
      saveButton.disabled = true;
      setEditorReadOnly(true);
      setStatus('recovery choice required', 'readonly');
      showRecoveryPrompt();
    }
    else { saveButton.disabled = false; setStatus('saved', 'saved'); }
    if (doc.draftPresent && !recoveryPending) showBanner('A recovery draft is available to the browser writer. This tab is read-only.');
    saveButton.addEventListener('click', () => enqueue(save));
    exitButton.addEventListener('click', () => enqueue(exitEditor));
    document.addEventListener('keydown', event => { if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') { event.preventDefault(); enqueue(save); } });
    setInterval(() => { if (writer) request('/heartbeat', {method:'POST'}).catch(() => makeReadOnly('The writer lease expired. Reload to reclaim editing.')); }, 15000);
    connectEvents();
  }
  addEventListener('pagehide', () => { if (writer && lease) fetch(root + '/close', {method:'POST', headers:{'X-Kbrd-Editor-Lease':lease}, keepalive:true}).catch(() => {}); });
  init().catch(offline);
})();
