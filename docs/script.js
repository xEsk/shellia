const typedTarget = document.querySelector(".typed-text");
const revealTargets = Array.from(document.querySelectorAll(".reveal"));

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

function typeText(node, text, delay = 18) {
  if (!node) {
    return;
  }

  let index = 0;
  function tick() {
    node.textContent = text.slice(0, index);
    if (index < text.length) {
      index += 1;
      window.setTimeout(tick, delay);
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

if (typedTarget) {
  const text = typedTarget.dataset.typed || "";
  typeText(typedTarget, text);
}

revealBlocks(revealTargets);
