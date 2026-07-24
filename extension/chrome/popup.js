import { captureTab } from "./capture.js";

const elements = {
  board: document.querySelector("#board"),
  folder: document.querySelector("#folder"),
  cardName: document.querySelector("#card-name"),
  captureMode: document.querySelector("#capture-mode"),
  markdownField: document.querySelector("#markdown-field"),
  markdown: document.querySelector("#markdown"),
  notes: document.querySelector("#notes"),
  includeURL: document.querySelector("#include-url"),
  form: document.querySelector("#capture-form"),
  save: document.querySelector("#save"),
  status: document.querySelector("#status"),
  message: document.querySelector("#message"),
};

let page = { title: "", url: "" };
let sourceTabId;
let captureWarning = "";
const captureCache = new Map();
let previousCaptureMode = elements.captureMode.value;

elements.board.addEventListener("change", loadFolders);
elements.captureMode.addEventListener("change", () => {
  captureCache.set(previousCaptureMode, elements.markdown.value);
  previousCaptureMode = elements.captureMode.value;
  loadCaptureMarkdown();
});
elements.form.addEventListener("submit", saveCard);

await loadPageContext();
const stored = await chrome.storage.local.get(["board", "folder"]);
await connect(stored);
if (captureWarning) showMessage(captureWarning, true);

async function loadPageContext() {
  const source = new URLSearchParams(window.location.search).get("source");
  if (source === "context-menu") {
    const stored = await chrome.storage.session.get("pendingCapture");
    await chrome.storage.session.remove("pendingCapture");
    if (stored.pendingCapture) {
      page = {
        title: stored.pendingCapture.title || "New card",
        url: stored.pendingCapture.url || "",
      };
      sourceTabId = stored.pendingCapture.tabId;
      elements.cardName.value = page.title;
      elements.captureMode.value = stored.pendingCapture.mode || "selection";
      previousCaptureMode = elements.captureMode.value;
      elements.markdown.value = stored.pendingCapture.markdown || "";
      captureCache.set(elements.captureMode.value, elements.markdown.value);
      captureWarning = stored.pendingCapture.captureError || "";
      updateMarkdownVisibility();
      return;
    }
  }

  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab) return;
  page = { title: tab.title || "New card", url: tab.url || "" };
  sourceTabId = tab.id;
  elements.cardName.value = page.title;
  if (!sourceTabId) return;
  try {
    const selection = await captureTab(sourceTabId, "selection");
    if (selection.markdown) {
      elements.captureMode.value = "selection";
      previousCaptureMode = "selection";
      elements.markdown.value = selection.markdown;
      captureCache.set("selection", selection.markdown);
    } else {
      elements.captureMode.value = "article";
      previousCaptureMode = "article";
      await loadCaptureMarkdown();
    }
  } catch (error) {
    elements.captureMode.value = "link";
    previousCaptureMode = "link";
    captureWarning = `${error.message} Link-only capture is still available.`;
  }
  updateMarkdownVisibility();
}

async function loadCaptureMarkdown() {
  const mode = elements.captureMode.value;
  updateMarkdownVisibility();
  if (mode === "link") return;
  if (captureCache.has(mode)) {
    elements.markdown.value = captureCache.get(mode);
    return;
  }
  if (!sourceTabId) {
    showMessage("The original page is unavailable; use Link only or keep the current Markdown.", true);
    return;
  }

  setBusy(true);
  elements.markdown.disabled = true;
  try {
    const capture = await captureTab(sourceTabId, mode);
    elements.markdown.value = capture.markdown;
    captureCache.set(mode, capture.markdown);
    hideMessage();
  } catch (error) {
    showMessage(error.message, true);
  } finally {
    elements.markdown.disabled = false;
    setBusy(false);
  }
}

function updateMarkdownVisibility() {
  elements.markdownField.hidden = elements.captureMode.value === "link";
}

async function connect(previous = {}) {
  setBusy(true);
  hideMessage();
  try {
    const result = await nativeRequest({ action: "list_boards" });
    const boards = result.boards || [];
    fillSelect(
      elements.board,
      boards,
      (board) => board.path,
      (board) => `${board.name}${board.pinned ? " ★" : ""}`,
    );
    if (!boards.length) throw new Error("No boards known. Open a board in kbrd first.");
    selectRemembered(elements.board, previous.board);
    elements.board.disabled = false;
    await loadFolders(previous.folder);
    elements.status.textContent = "Connected to installed kbrd";
    elements.status.className = "status connected";
    elements.save.disabled = false;
  } catch (error) {
    elements.status.textContent = "Cannot reach installed kbrd";
    elements.status.className = "status error";
    showMessage(`${error.message} Re-run “kbrd extension install” to register the native host.`, true);
  } finally {
    setBusy(false);
  }
}

async function loadFolders(remembered = "") {
  const board = elements.board.value;
  if (!board) return;
  elements.folder.disabled = true;
  elements.save.disabled = true;
  try {
    const result = await nativeRequest({ action: "list_folders", board });
    const folders = result.folders || [];
    fillSelect(elements.folder, folders, (folder) => folder, (folder) => folder);
    selectRemembered(elements.folder, remembered);
    elements.folder.disabled = !folders.length;
    elements.save.disabled = !folders.length;
    if (!folders.length) showMessage("This board has no columns.", true);
  } catch (error) {
    showMessage(error.message, true);
  }
}

async function saveCard(event) {
  event.preventDefault();
  hideMessage();
  setBusy(true);
  const board = elements.board.value;
  const folder = elements.folder.value;
  try {
    const result = await nativeRequest({
      action: "add_file_to_board",
      board,
      folder,
      name: elements.cardName.value.trim(),
      content: buildMarkdown(),
      source: "chrome",
      source_app: "Chromium browser",
      url: elements.includeURL.checked ? page.url : "",
      capture: true,
    });
    await chrome.storage.local.set({ board, folder });
    const warnings = result.warnings || [];
    const suffix = warnings.length ? ` Hook warning: ${warnings[0].message}` : "";
    showMessage(`Saved to ${board} / ${folder}.${suffix}`, warnings.length > 0);
    elements.save.textContent = "Saved";
    setTimeout(() => window.close(), warnings.length ? 1800 : 650);
  } catch (error) {
    showMessage(error.message, true);
  } finally {
    setBusy(false);
  }
}

function buildMarkdown() {
  const content = [];
  const markdown = elements.markdown.value.trim();
  if (elements.captureMode.value !== "link" && markdown) content.push(markdown, "");
  const notes = elements.notes.value.trim();
  if (notes) content.push(notes, "");
  return content.join("\n");
}

function fillSelect(select, items, value, label) {
  select.replaceChildren(...items.map((item) => {
    const option = document.createElement("option");
    option.value = value(item);
    option.textContent = label(item);
    return option;
  }));
}

function selectRemembered(select, value) {
  if ([...select.options].some((option) => option.value === value)) select.value = value;
}

function setBusy(busy) {
  elements.save.disabled = busy || elements.folder.disabled;
  if (busy) elements.save.textContent = "Working…";
  else if (elements.save.textContent !== "Saved") elements.save.textContent = "Save card";
}

function showMessage(text, error) {
  elements.message.textContent = text;
  elements.message.className = `message${error ? " error" : ""}`;
  elements.message.hidden = false;
}

function hideMessage() {
  elements.message.hidden = true;
}

async function nativeRequest(message) {
  let response;
  try {
    response = await chrome.runtime.sendNativeMessage("dev.kbrd.capture", message);
  } catch (error) {
    throw new Error(error.message || "Native messaging host is unavailable.");
  }
  if (!response) throw new Error("Native messaging host returned no response.");
  if (!response.ok) throw new Error(response.error || "kbrd operation failed.");
  return response.data || {};
}
