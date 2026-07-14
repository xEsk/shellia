/* ───────────────────────────────────────────────────────────
 * Shellia landing — all motion + interactions
 * single file, vanilla JS, no deps
 * ─────────────────────────────────────────────────────────── */

// ═══════════════ tiny helpers ═══════════════
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
const esc = (v) =>
  String(v).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");

// ═══════════════ terminal mocks ═══════════════
const mocks = [
  {
    instruction: "clean unused Docker images",
    blocks: [
      {
        t: "session",
        lines: [
          "/Users/Xesc/Documents/Programacio/Go/shellia",
        ],
      },
      { t: "sum", title: "plan", lines: ["Clean unused Docker images with a non-interactive prune command."] },
      { t: "div" },
      {
        t: "step", title: "step 1/1", cmd: "docker image prune -a -f",
        lines: [
          { t: "bullet", v: "Remove unused Docker images" },
          { t: "confirm", v: "Run step 1/1? [y/e/i/n]:" },
          { t: "cursor" },
        ],
      },
    ],
  },
  {
    instruction: "find files larger than 100MB",
    blocks: [
      { t: "div" },
      { t: "sum", title: "plan", lines: ["Search current tree for files over 100MB. Read-only, classified safe."] },
      { t: "div" },
      {
        t: "step", title: "step 1/1", cmd: "find . -type f -size +100M -printf '%s\\t%p\\n'",
        lines: [
          { t: "bullet", v: "Locate large files in the working tree" },
          { t: "out-l" },
          { t: "out", v: "184302341  ./dist/build.tar.gz" },
          { t: "out", v: "112098234  ./node_modules/.cache/turbo.bin" },
        ],
      },
    ],
  },
  {
    instruction: "run my Go tests, then commit if green",
    blocks: [
      {
        t: "session",
        lines: [
          "/Users/Xesc/Documents/Programacio/Go/shellia",
        ],
      },
      { t: "sum", title: "plan", lines: ["Run the Go test suite first; if it passes, commit the current changes."] },
      { t: "div" },
      {
        t: "step", title: "step 1/3", cmd: "go test ./...",
        lines: [
          { t: "bullet", v: "Run all Go tests in the repository" },
          { t: "confirm", v: "Run step 1/3? [y/e/i/n]: yes" },
          { t: "out-l" },
          { t: "out", v: "ok    shellia 1.805s" },
        ],
      },
      { t: "div" },
      {
        t: "step", title: "step 2/3", cmd: "git add docs/index.html docs/script.js docs/styles.css executor.go ui.go ui_test.go docs/bkp docs/design-canvas.jsx docs/old",
        lines: [
          { t: "bullet", v: "Stage the current tracked and untracked changes for commit if tests pass" },
          { t: "confirm", v: "Run step 2/3? [y/e/i/n]:" },
        ],
      },
    ],
  },
  {
    instruction: "/shell",
    blocks: [
      { t: "sum", title: "mode", lines: ["Shell mode enabled (interactive)."] },
      { t: "shell-prompt", text: "git status --short" },
      {
        t: "git",
        lines: [
          ["M", "docs/index.html"],
          ["M", "docs/script.js"],
          ["D", "docs/styles.css"],
          ["M", "executor.go"],
          ["M", "ui.go"],
          ["M", "ui_test.go"],
          ["??", "docs/bkp/"],
          ["??", "docs/design-canvas.jsx"],
          ["??", "docs/old/"],
        ],
      },
      { t: "shell-prompt", text: "/ai" },
      { t: "sum", title: "mode", lines: ["Prompt mode enabled."] },
    ],
  },
  {
    instruction: "!brew outdated",
    blocks: [
      { t: "ans", lines: ["Starting interactive command. Shellia will resume when it exits."] },
      {
        t: "raw",
        lines: [
          "ansible (13.5.0_1) < 13.6.0_1",
          "certifi (2026.2.25) < 2026.4.22",
          "cryptography (46.0.6) < 48.0.0",
          "go (1.26.1) < 1.26.3",
          "goreleaser (2.15.3) < 2.15.4",
          "libsodium (1.0.21) < 1.0.22",
          "mole (1.35.0) < 1.39.0",
          "ruby (4.0.4) < 4.0.5",
          "stripe-cli (1.40.0) < 1.41.1",
          "claude-code (2.1.126) != 2.1.139",
          "codex (0.130.0) != 0.132.0",
        ],
      },
      { t: "prompt", label: "you", text: "", cursor: true },
    ],
  },
];

// ═══════════════ renderers ═══════════════
function renderCmd(cmd) {
  return `<div class="tcmd"><span class="pfx">run ›</span><span class="txt">${esc(cmd)}</span></div>`;
}
function renderPromptText(label, text, cursor = false) {
  return `<p class="tline">
    <span class="prompt">${esc(label)}</span><span class="prompt-arr">›</span>${esc(text)}${cursor ? '<span class="prompt-cursor"></span>' : ""}
  </p>`;
}
function renderQuestionPrompt(text = "", cursor = false) {
  return `<div class="tblock reveal">
    <p class="tline muted">What do you want <span class="shellia">Shell<span class="ia">ia</span></span> to do?</p>
    ${renderPromptText("you", text, cursor)}
  </div>`;
}
function formatConfirm(value) {
  const escaped = esc(value);
  return escaped.replace("confirm", '<span class="kw">confirm</span>').replace("yes", '<span class="confirm-yes">yes</span>');
}
function renderLine(l) {
  switch (l.t) {
    case "confirm":
      return `<p class="tline output"><span class="bullet warn">•</span> ${formatConfirm(l.v)}</p>`;
    case "out-l":
      return `<p class="tline output"><span class="bullet muted-bullet">•</span> system output</p>`;
    case "out":
      return `<p class="tline output-text">${esc(l.v)}</p>`;
    case "cursor":
      return `<p class="tline"><span class="prompt-cursor prompt-cursor-line"></span></p>`;
    default:
      return `<p class="tline"><span class="bullet">•</span> ${esc(l.v)}</p>`;
  }
}
function renderBlock(b) {
  if (b.t === "div") return `<div class="tdiv reveal"></div>`;
  if (b.t === "prompt") {
    return renderQuestionPrompt(b.text, b.cursor);
  }
  if (b.t === "shell-prompt") {
    return `<div class="tblock reveal">
      <p class="tline muted">Enter a shell command to run.</p>
      <p class="tline"><span class="prompt shell-prompt">shell</span><span class="prompt-arr">›</span>${esc(b.text)}</p>
    </div>`;
  }
  if (b.t === "sum") {
    return `<div class="tblock reveal">
      <p class="tbtitle tbtitle-${esc(b.title)}">${esc(b.title)}</p>
      ${b.lines.map((l) => `<p class="tline">${esc(l)}</p>`).join("")}
    </div>`;
  }
  if (b.t === "session") {
    return `<div class="tblock reveal">
      <p class="tline"><span class="shellia">Shell<span class="ia">ia</span></span> <span class="session-dot">·</span> <span class="session-mode">dev</span></p>
      ${b.lines.map((l) => `<p class="tline muted">${esc(l)}</p>`).join("")}
    </div>`;
  }
  if (b.t === "ans") {
    return `<div class="tblock reveal">
      <p class="tbtitle"><span class="shellia">Shell<span class="ia">ia</span></span></p>
      ${b.lines.map((l) => `<p class="tline answer">${esc(l)}</p>`).join("")}
    </div>`;
  }
  if (b.t === "raw") {
    return `<div class="tblock reveal">
      ${b.lines.map((l) => `<p class="tline output-raw">${esc(l)}</p>`).join("")}
    </div>`;
  }
  if (b.t === "git") {
    return `<div class="tblock reveal">
      ${b.lines.map(([status, file]) => `<p class="tline output-raw"><span class="status-danger">${esc(status)}</span> ${esc(file)}</p>`).join("")}
    </div>`;
  }
  return `<div class="tblock reveal">
    <p class="tbtitle tbtitle-step">${esc(b.title)}</p>
    ${renderCmd(b.cmd)}
    ${b.lines.map(renderLine).join("")}
  </div>`;
}

// ═══════════════ typed text effect ═══════════════
const typeTimers = new WeakMap();
function typeText(node, text, delay = 46, onDone) {
  if (!node) {
    if (onDone) onDone();
    return;
  }
  const prev = typeTimers.get(node);
  if (prev) clearTimeout(prev);
  node.classList.remove("done");
  let i = 0;
  const tick = () => {
    node.textContent = text.slice(0, i);
    if (i < text.length) {
      const char = text[i] || "";
      const nextDelay =
        delay +
        Math.random() * 42 +
        (char === " " ? 34 : 0) +
        (/[,.!?/]/.test(char) ? 90 : 0);
      i++;
      typeTimers.set(node, setTimeout(tick, nextDelay));
    } else {
      node.classList.add("done");
      if (onDone) onDone();
    }
  };
  tick();
}

// ═══════════════ rotating terminal mock ═══════════════
const terminalMockStates = new WeakMap();

function startTerminalMock(typedSel, mockSel, rotationMs, fadeMs = 520) {
  const typed = $(typedSel);
  const mock = $(mockSel);
  if (!typed || !mock) return;

  const previous = terminalMockStates.get(mock);
  if (previous) {
    previous.timers.forEach(clearTimeout);
    if (previous.raf) cancelAnimationFrame(previous.raf);
  }

  const state = {
    index: previous ? previous.index : Math.floor(Math.random() * mocks.length),
    raf: 0,
    runId: (previous ? previous.runId : 0) + 1,
    timers: [],
  };
  terminalMockStates.set(mock, state);

  const addTimer = (fn, delay) => {
    const runId = state.runId;
    const id = setTimeout(() => {
      if (terminalMockStates.get(mock)?.runId === runId) fn();
    }, delay);
    state.timers.push(id);
    return id;
  };

  function paint(idx) {
    if (terminalMockStates.get(mock) !== state) return;
    state.index = idx % mocks.length;
    state.timers.forEach(clearTimeout);
    state.timers = [];
    typed.classList.remove("fade");
    mock.classList.remove("fade");
    const m = mocks[state.index];
    mock.innerHTML = m.blocks.map(renderBlock).join("");
    const reveals = $$(".reveal", mock);
    const revealDelay = 420;
    const stepDelay = 260;
    typeText(typed, m.instruction, 56, () => {
      if (terminalMockStates.get(mock) !== state) return;
      state.raf = requestAnimationFrame(() => {
        if (terminalMockStates.get(mock) !== state) return;
        reveals.forEach((el, n) => {
          addTimer(() => el.classList.add("in"), revealDelay + n * stepDelay);
        });
      });
    });
    // dynamic rotation: time-to-fully-revealed + reading buffer + per-block dwell
    const promptDuration = m.instruction.length * 100;
    const revealedAt = promptDuration + revealDelay + reveals.length * stepDelay;
    const readingBuffer = 11000;                       // baseline pause once fully revealed
    const perBlockDwell = reveals.length * 700;        // bigger demos linger longer
    const total = rotationMs || revealedAt + readingBuffer + perBlockDwell;
    addTimer(advance, total);
  }

  function advance() {
    if (terminalMockStates.get(mock) !== state) return;
    state.index = (state.index + 1) % mocks.length;
    typed.classList.add("fade");
    mock.classList.add("fade");
    addTimer(() => paint(state.index), fadeMs);
  }

  paint(state.index);
}

// ═══════════════ separator typing animation ═══════════════
function setupSeparators() {
  const seps = $$("[data-sep]");
  seps.forEach((sep) => {
    const prompt = sep.dataset.prompt || "› running";
    const seq = sep.dataset.sequence || "";
    const ms = sep.dataset.ms || "00ms";
    sep.innerHTML = `<span class="sep-prompt">${esc(prompt)}</span><span class="sep-text"></span><span class="sep-ms">${esc(ms)}</span>`;
    const target = $(".sep-text", sep);

    let played = false;
    const io = new IntersectionObserver(
      (entries) => {
        entries.forEach((e) => {
          if (e.isIntersecting && !played) {
            played = true;
            typeText(target, seq, 14);
          }
        });
      },
      { threshold: 0.4 }
    );
    io.observe(sep);
  });
}

// ═══════════════ scroll reveal for how-it-works steps ═══════════════
function setupReveal() {
  const items = $$("[data-reveal]");
  const io = new IntersectionObserver(
    (entries) => {
      entries.forEach((e) => {
        if (e.isIntersecting) {
          e.target.classList.add("in");
          io.unobserve(e.target);
        }
      });
    },
    { threshold: 0.3 }
  );
  items.forEach((el) => io.observe(el));
}

// ═══════════════ navigation scrollspy ═══════════════
function setupNavSpy() {
  const links = $$(".nav-links a[href^='#']");
  const sections = links
    .map((link) => $(link.getAttribute("href")))
    .filter(Boolean);
  if (!links.length || !sections.length) return;

  const setActive = (id) => {
    links.forEach((link) => {
      link.classList.toggle("active", link.getAttribute("href") === `#${id}`);
    });
  };
  const clearActive = () => links.forEach((link) => link.classList.remove("active"));

  const updateActive = () => {
    const cursor = window.scrollY + window.innerHeight * 0.34;
    if (cursor < sections[0].offsetTop) {
      clearActive();
      return;
    }
    const current = sections
      .filter((section) => section.offsetTop <= cursor)
      .at(-1);
    if (current) setActive(current.id);
  };

  updateActive();
  window.addEventListener("scroll", updateActive, { passive: true });
  window.addEventListener("resize", updateActive);
}

// ═══════════════ classifier loop ═══════════════
const classifierData = [
  { cmd: "ls -la", lvl: "safe" },
  { cmd: "pwd", lvl: "safe" },
  { cmd: "git status", lvl: "safe" },
  { cmd: "cat .env.example", lvl: "safe" },
  { cmd: "docker ps", lvl: "safe" },
  { cmd: "git pull origin main", lvl: "risky" },
  { cmd: "npm install lodash", lvl: "risky" },
  { cmd: "docker image prune -a", lvl: "risky" },
  { cmd: "mv old.config new.config", lvl: "risky" },
  { cmd: "kubectl apply -f deploy.yaml", lvl: "risky" },
  { cmd: "rm -rf node_modules", lvl: "danger" },
  { cmd: "sudo apt update && upgrade", lvl: "danger" },
  { cmd: "chmod -R 777 /var", lvl: "danger" },
  { cmd: "dd if=/dev/zero of=/dev/sda", lvl: "danger" },
  { cmd: "git push --force origin main", lvl: "danger" },
];

function setupClassifier() {
  const wrap = $("[data-cls-rows]");
  if (!wrap) return;

  const MAX_VISIBLE = 6;
  const ROW_DELAY = 1700;
  const TAG_DELAY = 380;

  let queue = [...classifierData].sort(() => Math.random() - 0.5);
  let active = [];

  function addRow() {
    if (queue.length === 0) queue = [...classifierData].sort(() => Math.random() - 0.5);
    const item = queue.shift();
    const row = document.createElement("div");
    row.className = `cls-row ${item.lvl}`;
    row.innerHTML = `
      <span class="cmd">$ ${esc(item.cmd)}</span>
      <span class="badge">${item.lvl}</span>
    `;
    wrap.appendChild(row);
    active.push(row);
    requestAnimationFrame(() => {
      row.classList.add("in");
      setTimeout(() => row.classList.add("tagged"), TAG_DELAY);
    });

    while (active.length > MAX_VISIBLE) {
      const old = active.shift();
      old.style.transition = "opacity .4s ease, transform .4s ease, max-height .4s ease, margin .4s ease, padding .4s ease";
      old.style.opacity = "0";
      old.style.transform = "translateY(-12px)";
      old.style.maxHeight = "0";
      old.style.padding = "0 14px";
      old.style.margin = "0";
      setTimeout(() => old.remove(), 400);
    }
  }

  let started = false;
  const io = new IntersectionObserver(
    (entries) => {
      entries.forEach((e) => {
        if (e.isIntersecting && !started) {
          started = true;
          // seed
          for (let i = 0; i < 3; i++) setTimeout(addRow, i * 350);
          setInterval(addRow, ROW_DELAY);
        }
      });
    },
    { threshold: 0.25 }
  );
  io.observe(wrap.closest(".classifier"));
}

// ═══════════════ modes tabs ═══════════════
function setupModeTabs() {
  const tabs = $$(".modes-tab");
  if (!tabs.length) return;
  tabs.forEach((t) => {
    t.addEventListener("click", () => {
      const target = t.dataset.mode;
      tabs.forEach((x) => x.classList.toggle("on", x === t));
      $$("[data-mode-pane]").forEach((p) => {
        p.hidden = p.dataset.modePane !== target;
      });
    });
  });
}

// ═══════════════ FAQ ═══════════════
function setupFaq() {
  $$(".faq-item").forEach((item) => {
    const q = $(".faq-q", item);
    q.addEventListener("click", () => {
      const open = item.classList.contains("open");
      item.classList.toggle("open", !open);
    });
  });
  // open first by default
  const first = $(".faq-item");
  if (first) first.classList.add("open");
}

// ═══════════════ release fetch (best-effort) ═══════════════
async function loadRelease() {
  const vEl = $("#download-version");
  const links = $$(".download-asset[data-os]");
  if (!vEl && !links.length) return;
  try {
    const r = await fetch("https://api.github.com/repos/xEsk/shellia/releases/latest", {
      headers: { Accept: "application/vnd.github+json" },
    });
    if (!r.ok) {
      if (vEl) vEl.textContent = "v0.1.0";
      return;
    }
    const j = await r.json();
    const tag = j.tag_name || "v0.x";
    if (vEl) vEl.textContent = tag;
    const assets = Array.isArray(j.assets) ? j.assets : [];
    links.forEach((l) => {
      const os = l.dataset.os, arch = l.dataset.arch;
      const a = assets.find((x) => x.name.includes(`_${os}_`) && x.name.includes(`_${arch}.`));
      if (a && a.browser_download_url) l.href = a.browser_download_url;
    });
  } catch (_) {
    if (vEl) vEl.textContent = "v0.1.0";
  }
}

// ═══════════════ boot ═══════════════
function boot() {
  // start terminal A (default visible)
  startTerminalMock("[data-typed]", "[data-term-mock]");
  setupSeparators();
  setupReveal();
  setupNavSpy();
  setupClassifier();
  setupModeTabs();
  setupFaq();
  loadRelease();
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", boot);
} else {
  boot();
}
