const elements = {
  board: document.querySelector("#board"),
  folder: document.querySelector("#folder"),
  cardName: document.querySelector("#card-name"),
  selection: document.querySelector("#selection"),
  notes: document.querySelector("#notes"),
  includeURL: document.querySelector("#include-url"),
  form: document.querySelector("#capture-form"),
  save: document.querySelector("#save"),
  status: document.querySelector("#status"),
  message: document.querySelector("#message"),
};

let page = { title: "", url: "" };

elements.board.addEventListener("change", loadFolders);
elements.form.addEventListener("submit", saveCard);

await loadPageContext();
const stored = await chrome.storage.local.get(["board", "folder"]);
await connect(stored);

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
      elements.cardName.value = page.title;
      elements.selection.value = stored.pendingCapture.selection || "";
      return;
    }
  }

  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab) return;
  page = { title: tab.title || "New card", url: tab.url || "" };
  elements.cardName.value = page.title;
  if (!tab.id) return;
  try {
    const results = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => window.getSelection()?.toString() || "",
    });
    elements.selection.value = results[0]?.result || "";
  } catch {
    // Browser-owned pages do not permit script injection; title/URL still work.
  }
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
    await nativeRequest({
      action: "add_file_to_board",
      board,
      folder,
      name: elements.cardName.value.trim(),
      content: buildContent(),
    });
    await chrome.storage.local.set({ board, folder });
    showMessage(`Saved to ${board} / ${folder}.`, false);
    elements.save.textContent = "Saved";
    setTimeout(() => window.close(), 650);
  } catch (error) {
    showMessage(error.message, true);
  } finally {
    setBusy(false);
  }
}

function buildContent() {
  const metadata = ["---", "source: chrome", `captured_at: ${yamlString(new Date().toISOString())}`];
  if (elements.includeURL.checked && page.url) metadata.push(`url: ${yamlString(page.url)}`);
  metadata.push("---", "");

  const selected = elements.selection.value.trim();
  if (selected) {
    metadata.push(...selected.split("\n").map((line) => `> ${line}`), "");
  }
  const notes = elements.notes.value.trim();
  if (notes) metadata.push(notes, "");
  return metadata.join("\n");
}

function yamlString(value) {
  return JSON.stringify(value);
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
