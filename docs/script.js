const typedTarget = document.querySelector(".typed-text");
const revealTargets = Array.from(document.querySelectorAll(".reveal"));

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
