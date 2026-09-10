const TOKEN_PATTERN = /"(?:\\.|[^"\\])*"(?=\s*:)|"(?:\\.|[^"\\])*"|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?|\b(?:true|false)\b|\bnull\b|[{}\[\],:]/g;

function tokenClass(token) {
  if (token.startsWith('"')) return "json-token--string";
  if (token === "true" || token === "false") return "json-token--boolean";
  if (token === "null") return "json-token--null";
  if (/^-?\d/.test(token)) return "json-token--number";
  return "json-token--punctuation";
}

function createToken(text, className = "") {
  const span = document.createElement("span");
  span.textContent = text;
  if (className) span.className = `json-token ${className}`;
  return span;
}

function renderFormattedJSON(element, formatted) {
  const nodes = [];
  let cursor = 0;

  for (const match of formatted.matchAll(TOKEN_PATTERN)) {
    if (match.index > cursor) nodes.push(createToken(formatted.slice(cursor, match.index)));

    const token = match[0];
    const following = formatted.slice(match.index + token.length);
    const className = token.startsWith('"') && /^\s*:/.test(following)
      ? "json-token--key"
      : tokenClass(token);
    nodes.push(createToken(token, className));
    cursor = match.index + token.length;
  }

  if (cursor < formatted.length) nodes.push(createToken(formatted.slice(cursor)));
  element.replaceChildren(...nodes);
}

export function renderText(element, text) {
  element.textContent = String(text ?? "");
  return false;
}

export function renderJSON(element, value, { compact = false } = {}) {
  const formatted = JSON.stringify(value, null, compact ? undefined : 2);
  if (formatted === undefined) return renderText(element, value);
  renderFormattedJSON(element, formatted);
  return true;
}

export function renderJSONText(element, text, options = {}) {
  try {
    return renderJSON(element, JSON.parse(text), options);
  } catch {
    return renderText(element, text);
  }
}

function translatedLabel(translate, key, fallback) {
  const label = translate(key);
  return label === key ? fallback : label;
}

export function mountJSONCopyButtons(root, {
  translate = (key) => key,
  clipboard = globalThis.navigator?.clipboard,
  setTimeoutImpl = globalThis.setTimeout,
} = {}) {
  for (const button of root.querySelectorAll("[data-json-copy]")) {
    const initialLabel = button.textContent;
    button.addEventListener("click", async () => {
      const target = root.querySelector(button.dataset.jsonCopy);
      if (!target) return;

      try {
        if (!clipboard?.writeText) throw new Error("Clipboard API unavailable");
        await clipboard.writeText(target.textContent);
        button.textContent = translatedLabel(translate, "json.copied", "Copied");
      } catch {
        button.textContent = translatedLabel(translate, "json.copy_failed", "Copy failed");
      }

      setTimeoutImpl(() => { button.textContent = initialLabel; }, 1_500);
    });
  }
}
