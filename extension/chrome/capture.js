const captureScripts = [
  "vendor/readability.js",
  "vendor/turndown.js",
  "vendor/turndown-plugin-gfm.js",
  "capture-page.js",
];

const maxMarkdownBytes = 3 * 1024 * 1024;

export async function captureTab(tabId, mode) {
  if (!Number.isInteger(tabId)) throw new Error("The source tab is unavailable.");

  const target = { tabId };
  await chrome.scripting.executeScript({ target, files: captureScripts });
  const [execution] = await chrome.scripting.executeScript({
    target,
    func: (captureMode) => globalThis.kbrdCapturePage(captureMode),
    args: [mode],
  });
  const capture = execution?.result;
  if (!capture || typeof capture.markdown !== "string") {
    throw new Error("The page did not return Markdown content.");
  }
  if (new TextEncoder().encode(capture.markdown).length > maxMarkdownBytes) {
    throw new Error("The captured Markdown is too large for Native Messaging.");
  }
  return capture;
}
