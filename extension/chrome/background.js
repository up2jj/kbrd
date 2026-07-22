const captureSelectionMenuID = "capture-selection";

function registerContextMenu() {
  chrome.contextMenus.remove(captureSelectionMenuID, () => {
    // Missing menu entries are normal after a fresh install.
    void chrome.runtime.lastError;
    chrome.contextMenus.create({
      id: captureSelectionMenuID,
      title: "Capture selection to kbrd…",
      contexts: ["selection"],
    });
  });
}

// Run at service-worker startup as well as installation time. Unpacked
// extensions are updated by replacing their files and pressing Reload, which
// does not consistently produce an onInstalled event across Chromium browsers.
registerContextMenu();
chrome.runtime.onInstalled.addListener(registerContextMenu);

chrome.contextMenus.onClicked.addListener((info, tab) => {
  if (info.menuItemId !== captureSelectionMenuID) return;
  openSelectionCapture(info, tab).catch((error) => {
    console.error("Cannot open kbrd capture window", error);
  });
});

async function openSelectionCapture(info, tab) {
  await chrome.storage.session.set({
    pendingCapture: {
      title: tab?.title || "New card",
      url: info.pageUrl || tab?.url || "",
      selection: info.selectionText || "",
    },
  });
  await chrome.windows.create({
    url: chrome.runtime.getURL("popup.html?source=context-menu"),
    type: "popup",
    width: 420,
    height: 680,
    focused: true,
  });
}
