globalThis.kbrdCapturePage = function capturePage(mode) {
  if (mode === "link") return { markdown: "", title: document.title };

  const captured = mode === "selection" ? captureSelection() : captureArticle();
  if (!captured.root) return { markdown: "", title: captured.title || document.title };

  normalizeURLs(captured.root);
  removeUnsafeElements(captured.root);

  const turndown = new globalThis.TurndownService({
    headingStyle: "atx",
    bulletListMarker: "-",
    codeBlockStyle: "fenced",
    emDelimiter: "*",
    strongDelimiter: "**",
  });
  turndown.use(globalThis.turndownPluginGfm.gfm);

  return {
    markdown: normalizeMarkdown(turndown.turndown(captured.root)),
    title: captured.title || document.title,
  };

  function captureSelection() {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || selection.rangeCount === 0) {
      return { root: null, title: document.title };
    }

    const root = document.createElement("div");
    for (let index = 0; index < selection.rangeCount; index += 1) {
      if (index > 0) root.append(document.createElement("hr"));
      root.append(selection.getRangeAt(index).cloneContents());
    }
    return { root, title: document.title };
  }

  function captureArticle() {
    const documentClone = document.cloneNode(true);
    preserveTaskMarkers(documentClone);
    const article = new globalThis.Readability(documentClone, {
      charThreshold: 50,
    }).parse();
    const root = document.createElement("div");
    if (article?.content) {
      root.innerHTML = article.content;
      return { root, title: article.title };
    }

    const fallback = document.querySelector("article, main, body");
    if (fallback) root.append(fallback.cloneNode(true));
    return { root, title: document.title };
  }

  function preserveTaskMarkers(documentClone) {
    for (const checkbox of documentClone.querySelectorAll('li input[type="checkbox"]')) {
      checkbox.replaceWith(documentClone.createTextNode(checkbox.checked ? "[x] " : "[ ] "));
    }
  }

  function normalizeMarkdown(markdown) {
    return markdown
      .replace(/^(\s*[-+*]\s+)\\\[([ xX])\\\](?=\s)/gm, "$1[$2]")
      .trim();
  }

  function normalizeURLs(root) {
    for (const element of root.querySelectorAll("a[href], img[src]")) {
      const attribute = element.tagName === "A" ? "href" : "src";
      const value = element.getAttribute(attribute);
      if (!value) continue;
      try {
        const absolute = new URL(value, document.baseURI);
        if (absolute.protocol === "http:" || absolute.protocol === "https:") {
          element.setAttribute(attribute, absolute.href);
        } else {
          element.removeAttribute(attribute);
        }
      } catch {
        element.removeAttribute(attribute);
      }
    }
  }

  function removeUnsafeElements(root) {
    for (const element of root.querySelectorAll(
      "script, style, noscript, template, iframe, object, embed, form, button",
    )) {
      element.remove();
    }
  }
};
