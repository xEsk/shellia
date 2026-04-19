const typedTarget = document.querySelector(".typed-text");
const terminalMockTarget = document.querySelector("[data-terminal-mock]");
const mockRotationMs = 15000;
const mockFadeOutMs = 420;

const terminalMocks = [
  {
    instruction: "clean unused Docker images",
    blocks: [
      { type: "divider" },
      {
        type: "summary",
        title: "plan",
        lines: ["Docker image cleanup can free local disk space, but it needs explicit confirmation before execution."],
      },
      { type: "divider" },
      {
        type: "step",
        title: "step 1/1",
        command: "docker image prune -a",
        lines: [
          { type: "bullet", text: "Prune all unused Docker images" },
          { type: "confirm", text: "Run step 1/1? [y/e/i/n]:" },
        ],
      },
    ],
  },
  {
    instruction: "run the local PHP file with Docker",
    blocks: [
      { type: "divider" },
      {
        type: "summary",
        title: "plan",
        lines: ["Find the PHP file first, then run it with the newest local PHP Docker image."],
      },
      { type: "divider" },
      {
        type: "step",
        title: "step 1/2",
        command: "find . -maxdepth 2 -type f -name '*.php'",
        lines: [
          { type: "bullet", text: "Locate the PHP file in the current project" },
          { type: "outputLabel" },
          { type: "output", text: "./fitxer_exemple.php" },
        ],
      },
      { type: "divider" },
      {
        type: "step",
        title: "step 2/2",
        command: 'docker run --rm -v "$PWD":/app -w /app php:8.5-cli php fitxer_exemple.php',
        lines: [
          { type: "bullet", text: "Run the PHP file with the selected Docker image" },
          { type: "confirm", text: "Run step 2/2? [y/e/i/n]: yes" },
          { type: "outputLabel" },
          { type: "output", text: "Hello from PHP" },
        ],
      },
      { type: "divider" },
      {
        type: "answer",
        title: "Shellia",
        lines: ["Executed fitxer_exemple.php with php:8.5-cli. Output: Hello from PHP"],
      },
    ],
  },
  {
    instruction: "update Claude Code safely",
    blocks: [
      { type: "divider" },
      {
        type: "summary",
        title: "discovery",
        lines: ["Need to identify how Claude Code is installed before choosing the safe update command."],
      },
      { type: "divider" },
      {
        type: "step",
        title: "step 1/2",
        command: "command -v claude",
        lines: [
          { type: "bullet", text: "Locate the available Claude executable" },
          { type: "outputLabel" },
          { type: "output", text: "/opt/homebrew/bin/claude" },
        ],
      },
      { type: "divider" },
      {
        type: "step",
        title: "step 2/2",
        command: "brew list --versions | grep -i claude",
        lines: [
          { type: "bullet", text: "Check local package-manager ownership" },
          { type: "confirm", text: "Run step 2/2? [y/e/i/n]:" },
        ],
      },
    ],
  },
  {
    instruction: "/shell",
    blocks: [
      { type: "divider" },
      {
        type: "summary",
        title: "mode",
        lines: ["Shell mode enabled (interactive)."],
      },
      {
        type: "prompt",
        label: "shell",
        text: "git status --short",
      },
      {
        type: "summary",
        title: "output",
        lines: [" M docs/index.html", " M docs/script.js", " M docs/styles.css"],
      },
      {
        type: "prompt",
        label: "shell",
        text: "/exit",
      },
      { type: "divider" },
      {
        type: "answer",
        title: "Shellia",
        lines: ["Session closed."],
      },
    ],
  },
  {
    instruction: "!go test ./...",
    blocks: [
      { type: "divider" },
      {
        type: "summary",
        title: "shell",
        lines: ["Running one direct command without leaving prompt mode."],
      },
      { type: "divider" },
      {
        type: "step",
        title: "shell",
        command: "go test ./...",
        lines: [
          { type: "outputLabel" },
          { type: "output", text: "ok   shellia   0.236s" },
        ],
      },
      { type: "divider" },
      {
        type: "answer",
        title: "Shellia",
        lines: ["Command completed successfully."],
      },
    ],
  },
];

async function loadLatestRelease() {
  const versionEl = document.getElementById("download-version");
  const assetLinks = Array.from(document.querySelectorAll(".download-asset[data-os]"));

  if (!versionEl && assetLinks.length === 0) {
    return;
  }

  try {
    const response = await fetch("https://api.github.com/repos/xEsk/shellia/releases/latest", {
      headers: { Accept: "application/vnd.github+json" },
    });
    if (!response.ok) {
      return;
    }

    const release = await response.json();
    const version = release.tag_name || "";
    const assets = Array.isArray(release.assets) ? release.assets : [];

    if (versionEl && version) {
      versionEl.textContent = version;
    }

    assetLinks.forEach((link) => {
      const os = link.dataset.os;
      const arch = link.dataset.arch;
      const asset = assets.find((a) => a.name.includes(`_${os}_`) && a.name.includes(`_${arch}.`));
      if (asset && asset.browser_download_url) {
        link.href = asset.browser_download_url;
      }
    });
  } catch (_) {
    // silently fall back to releases page links already set in href
  }
}

loadLatestRelease();

let typeTimer = 0;

function typeText(node, text, delay = 18) {
  if (!node) {
    return;
  }

  window.clearTimeout(typeTimer);
  let index = 0;
  function tick() {
    node.textContent = text.slice(0, index);
    if (index < text.length) {
      index += 1;
      typeTimer = window.setTimeout(tick, delay);
    }
  }

  tick();
}

function revealBlocks(blocks) {
  blocks.forEach((block, index) => {
    window.setTimeout(() => {
      block.classList.add("is-visible");
    }, 1150 + index * 220);
  });
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function renderCommandBox(command) {
  return `
    <div class="terminal-command-box">
      <div class="terminal-command-line">
        <span class="terminal-command-prefix">run ›</span>
        <span class="terminal-command-text">${escapeHTML(command)}</span>
      </div>
    </div>
  `;
}

function renderTerminalLine(line) {
  switch (line.type) {
    case "confirm":
      return `
        <p class="terminal-line terminal-line-output">
          <span class="bullet bullet-warn">•</span>
          <span class="terminal-keyword">confirm</span> ${escapeHTML(line.text)}
        </p>
      `;
    case "outputLabel":
      return '<p class="terminal-line terminal-line-output"><span class="bullet">•</span> system output</p>';
    case "output":
      return `<p class="terminal-line terminal-output-text">${escapeHTML(line.text)}</p>`;
    case "bullet":
    default:
      return `<p class="terminal-line"><span class="bullet">•</span> ${escapeHTML(line.text)}</p>`;
  }
}

function renderTerminalBlock(block) {
  if (block.type === "divider") {
    return '<div class="terminal-divider reveal"></div>';
  }

  if (block.type === "prompt") {
    return `
      <p class="terminal-line terminal-session-prompt reveal">
        <span class="prompt-label">${escapeHTML(block.label)}</span>
        <span class="prompt-arrow">›</span>
        ${escapeHTML(block.text)}
      </p>
    `;
  }

  if (block.type === "summary") {
    return `
      <div class="terminal-block reveal">
        <p class="terminal-block-title">${escapeHTML(block.title)}</p>
        ${block.lines.map((line) => `<p class="terminal-line">${escapeHTML(line)}</p>`).join("")}
      </div>
    `;
  }

  if (block.type === "answer") {
    return `
      <div class="terminal-block terminal-answer reveal">
        <p class="terminal-block-title shellia-word">Shell<span class="shellia-ia">ia</span></p>
        ${block.lines.map((line) => `<p class="terminal-line terminal-answer-line">${escapeHTML(line)}</p>`).join("")}
      </div>
    `;
  }

  return `
    <div class="terminal-block reveal">
      <p class="terminal-block-title">${escapeHTML(block.title)}</p>
      ${renderCommandBox(block.command)}
      ${block.lines.map(renderTerminalLine).join("")}
    </div>
  `;
}

function renderTerminalMock(index) {
  if (!typedTarget || !terminalMockTarget || terminalMocks.length === 0) {
    return;
  }

  const mock = terminalMocks[index % terminalMocks.length];
  typedTarget.classList.remove("is-fading");
  terminalMockTarget.classList.remove("is-fading");
  terminalMockTarget.innerHTML = mock.blocks.map(renderTerminalBlock).join("");
  typeText(typedTarget, mock.instruction);

  window.requestAnimationFrame(() => {
    revealBlocks(Array.from(terminalMockTarget.querySelectorAll(".reveal")));
  });
}

function startTerminalMocks() {
  let index = Math.floor(Math.random() * terminalMocks.length);
  renderTerminalMock(index);

  if (terminalMocks.length < 2) {
    return;
  }

  window.setInterval(() => {
    index = (index + 1) % terminalMocks.length;
    typedTarget.classList.add("is-fading");
    terminalMockTarget.classList.add("is-fading");
    window.setTimeout(() => {
      renderTerminalMock(index);
    }, mockFadeOutMs);
  }, mockRotationMs);
}

startTerminalMocks();
