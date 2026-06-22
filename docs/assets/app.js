// Oxygen Docs — shared shell, theme, nav, mermaid, code highlighting
import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";

const NAV = [
  {
    section: "Introduction",
    items: [
      { id: "index", title: "Overview", href: "index.html" },
      { id: "getting-started", title: "Getting Started", href: "getting-started.html" },
    ],
  },
  {
    section: "Core Flows",
    items: [
      { id: "upload-flow", title: "Upload Flow", href: "upload-flow.html" },
      { id: "transcoding", title: "Transcoding Pipeline", href: "transcoding.html" },
      { id: "webhooks", title: "Webhooks", href: "webhooks.html" },
      { id: "live-streaming", title: "Live Streaming", href: "live-streaming.html" },
    ],
  },
  {
    section: "Reference",
    items: [
      { id: "quality-profiles", title: "Quality & Profiles", href: "quality-profiles.html" },
      { id: "multi-tenancy", title: "Multi-Tenancy", href: "multi-tenancy.html" },
      { id: "architecture", title: "Tech Stack & Structure", href: "architecture.html" },
    ],
  },
];

const FLAT = NAV.flatMap((s) => s.items);

function elt(html) {
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  return t.content.firstElementChild;
}

function currentPage() {
  return document.body.dataset.page || "index";
}

function buildShell() {
  const page = currentPage();
  const article = document.getElementById("doc");

  const navHtml = NAV.map((sec) => {
    const links = sec.items
      .map((it) => {
        const active = it.id === page ? " active" : "";
        return `<a href="${it.href}" class="nav-link${active} block rounded-lg px-3 py-2 text-sm text-slate-600 transition hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800">${it.title}</a>`;
      })
      .join("");
    return `
      <div class="mb-6">
        <p class="px-3 mb-2 text-xs font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">${sec.section}</p>
        <div class="space-y-1">${links}</div>
      </div>`;
  }).join("");

  const sidebar = elt(`
    <aside id="sidebar" class="thin-scroll fixed inset-y-0 left-0 z-40 w-72 -translate-x-full overflow-y-auto border-r border-slate-200 bg-white px-4 py-6 transition-transform duration-200 dark:border-slate-800 dark:bg-slate-900 lg:translate-x-0">
      <a href="index.html" class="mb-8 flex items-center gap-2.5 px-2">
        <span class="flex h-9 w-9 items-center justify-center overflow-hidden rounded-xl bg-white shadow-lg shadow-slate-900/10 ring-1 ring-slate-200 dark:ring-slate-700">
          <img src="./assets/logo.png" alt="Oxygen logo" class="h-7 w-7 object-contain" />
        </span>
        <span>
          <span class="block text-base font-bold text-slate-900 dark:text-white">Oxygen</span>
          <span class="block text-xs text-slate-400">Documentation</span>
        </span>
      </a>
      <nav>${navHtml}</nav>
    </aside>`);

  const overlay = elt(`<div id="overlay" class="fixed inset-0 z-30 hidden bg-slate-900/50 backdrop-blur-sm lg:hidden"></div>`);

  const idx = FLAT.findIndex((i) => i.id === page);
  const title = idx >= 0 ? FLAT[idx].title : "Documentation";

  const topbar = elt(`
    <header class="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-slate-200 bg-white/80 px-4 backdrop-blur-md dark:border-slate-800 dark:bg-slate-900/80 sm:px-6 lg:px-10">
      <button id="menu-btn" class="rounded-lg p-2 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 lg:hidden" aria-label="Open menu">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" x2="20" y1="6" y2="6"/><line x1="4" x2="20" y1="12" y2="12"/><line x1="4" x2="20" y1="18" y2="18"/></svg>
      </button>
      <span class="text-sm font-medium text-slate-500 dark:text-slate-400">Oxygen Docs</span>
      <svg class="h-4 w-4 text-slate-300 dark:text-slate-600" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="m9 18 6-6-6-6"/></svg>
      <span class="text-sm font-semibold text-slate-900 dark:text-white">${title}</span>
      <div class="ml-auto flex items-center gap-1">
        <a href="https://github.com" target="_blank" rel="noopener" class="rounded-lg p-2 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800" aria-label="Repository">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 24 24" fill="currentColor"><path d="M12 .5C5.7.5.5 5.7.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.2.8-.5v-2c-3.2.7-3.9-1.4-3.9-1.4-.5-1.3-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.7 1.3 3.4 1 .1-.8.4-1.3.8-1.6-2.6-.3-5.3-1.3-5.3-5.7 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17 4.6 18 4.9 18 4.9c.6 1.6.2 2.8.1 3.1.8.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.4-5.3 5.7.4.4.8 1.1.8 2.2v3.3c0 .3.2.6.8.5A11.5 11.5 0 0 0 23.5 12C23.5 5.7 18.3.5 12 .5Z"/></svg>
        </a>
        <button id="theme-btn" class="rounded-lg p-2 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800" aria-label="Toggle theme">
          <svg id="icon-sun" class="hidden h-5 w-5" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M6.3 17.7l-1.4 1.4M19.1 4.9l-1.4 1.4"/></svg>
          <svg id="icon-moon" class="hidden h-5 w-5" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>
        </button>
      </div>
    </header>`);

  // TOC (built from h2s after content is in place)
  const toc = elt(`
    <aside class="hidden xl:block">
      <div class="sticky top-24 w-56 pl-4">
        <p class="mb-3 text-xs font-semibold uppercase tracking-wider text-slate-400">On this page</p>
        <nav id="toc" class="space-y-1 border-l border-slate-200 dark:border-slate-800"></nav>
      </div>
    </aside>`);

  // Prev / next
  const prev = idx > 0 ? FLAT[idx - 1] : null;
  const next = idx >= 0 && idx < FLAT.length - 1 ? FLAT[idx + 1] : null;
  const pager = elt(`
    <div class="mt-16 grid gap-4 border-t border-slate-200 pt-8 dark:border-slate-800 sm:grid-cols-2">
      ${prev
      ? `<a href="${prev.href}" class="group rounded-xl border border-slate-200 p-4 transition hover:border-[var(--brand-border-hover)] dark:border-slate-800 dark:hover:border-[var(--brand-border-hover)]">
               <span class="text-xs text-slate-400">Previous</span>
               <span class="mt-1 block font-semibold text-slate-900 group-hover:text-[var(--brand-accent-text)] dark:text-white">${prev.title}</span>
             </a>`
      : "<span></span>"
    }
      ${next
      ? `<a href="${next.href}" class="group rounded-xl border border-slate-200 p-4 text-right transition hover:border-[var(--brand-border-hover)] dark:border-slate-800 dark:hover:border-[var(--brand-border-hover)]">
               <span class="text-xs text-slate-400">Next</span>
               <span class="mt-1 block font-semibold text-slate-900 group-hover:text-[var(--brand-accent-text)] dark:text-white">${next.title}</span>
             </a>`
      : "<span></span>"
    }
    </div>`);

  // Assemble
  const main = elt(`<main class="mx-auto flex w-full max-w-6xl gap-10 px-4 py-10 sm:px-6 lg:px-10"></main>`);
  const colMain = elt(`<div class="min-w-0 flex-1"></div>`);

  article.classList.add("prose", "prose-slate", "dark:prose-invert", "max-w-none");
  colMain.appendChild(article);
  colMain.appendChild(pager);
  main.appendChild(colMain);
  main.appendChild(toc);

  const contentCol = elt(`<div class="lg:pl-72"></div>`);
  contentCol.appendChild(topbar);
  contentCol.appendChild(main);

  document.body.prepend(contentCol);
  document.body.prepend(overlay);
  document.body.prepend(sidebar);

  buildToc();
  wireMobile();
}

function buildToc() {
  const toc = document.getElementById("toc");
  const heads = document.querySelectorAll("#doc h2");
  if (!toc || heads.length === 0) {
    toc.closest("aside")?.classList.add("xl:hidden");
    return;
  }
  heads.forEach((h, i) => {
    if (!h.id) h.id = "section-" + i;
    const a = elt(
      `<a href="#${h.id}" class="toc-link block border-l-2 border-transparent py-1 pl-4 text-sm text-slate-500 transition hover:text-slate-900 dark:text-slate-400 dark:hover:text-white">${h.textContent}</a>`
    );
    toc.appendChild(a);
  });
  scrollSpy(heads);
}

function scrollSpy(heads) {
  const links = document.querySelectorAll(".toc-link");
  const map = new Map();
  links.forEach((l) => map.set(l.getAttribute("href").slice(1), l));
  const obs = new IntersectionObserver(
    (entries) => {
      entries.forEach((e) => {
        if (e.isIntersecting) {
          links.forEach((l) => l.classList.remove("active"));
          map.get(e.target.id)?.classList.add("active");
        }
      });
    },
    { rootMargin: "-80px 0px -70% 0px" }
  );
  heads.forEach((h) => obs.observe(h));
}

function wireMobile() {
  const sidebar = document.getElementById("sidebar");
  const overlay = document.getElementById("overlay");
  const btn = document.getElementById("menu-btn");
  const open = () => {
    sidebar.classList.remove("-translate-x-full");
    overlay.classList.remove("hidden");
  };
  const close = () => {
    sidebar.classList.add("-translate-x-full");
    overlay.classList.add("hidden");
  };
  btn?.addEventListener("click", open);
  overlay?.addEventListener("click", close);
  sidebar.querySelectorAll("a").forEach((a) => a.addEventListener("click", close));
}

function initTheme() {
  const stored = localStorage.getItem("oxygen-theme");
  const dark = stored ? stored === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches;
  applyTheme(dark);
  document.getElementById("theme-btn")?.addEventListener("click", () => {
    const isDark = !document.documentElement.classList.contains("dark");
    applyTheme(isDark);
    localStorage.setItem("oxygen-theme", isDark ? "dark" : "light");
  });
}

function applyTheme(dark) {
  document.documentElement.classList.toggle("dark", dark);
  document.getElementById("icon-sun")?.classList.toggle("hidden", !dark);
  document.getElementById("icon-moon")?.classList.toggle("hidden", dark);
}

function wrapTables() {
  document.querySelectorAll("#doc table").forEach((t) => {
    if (t.parentElement.classList.contains("table-wrap")) return;
    const wrap = document.createElement("div");
    wrap.className = "table-wrap thin-scroll";
    t.parentNode.insertBefore(wrap, t);
    wrap.appendChild(t);
  });
}

function highlightCode() {
  if (window.hljs) {
    document.querySelectorAll("#doc pre code").forEach((b) => window.hljs.highlightElement(b));
  }
}

function initMermaid() {
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "loose",
    fontFamily: "Inter, sans-serif",
    theme: "base",
    themeVariables: {
      background: "#ffffff",
      primaryColor: "#fef3c7", // amber-100
      primaryBorderColor: "#d97706", // amber-600
      primaryTextColor: "#1c1917", // stone-900
      secondaryColor: "#f5f5f4", // stone-100
      tertiaryColor: "#fafaf9", // stone-50
      lineColor: "#78716c", // stone-500
      textColor: "#1c1917",
      mainBkg: "#fef3c7",
      nodeBorder: "#d97706",
      clusterBkg: "#fafaf9",
      clusterBorder: "#e7e5e4", // stone-200
      titleColor: "#1c1917",
      edgeLabelBackground: "#ffffff",
      // sequence diagrams
      actorBkg: "#fef3c7",
      actorBorder: "#d97706",
      actorTextColor: "#1c1917",
      signalColor: "#78716c",
      signalTextColor: "#1c1917",
      labelBoxBkgColor: "#fef3c7",
      labelBoxBorderColor: "#d97706",
      labelTextColor: "#1c1917",
      loopTextColor: "#1c1917",
      noteBkgColor: "#fef9c3",
      noteTextColor: "#1c1917",
      noteBorderColor: "#ca8a04",
    },
  });
  mermaid.run({ querySelector: ".mermaid" });
}

function setFavicon() {
  const link = document.createElement("link");
  link.rel = "icon";
  link.type = "image/png";
  link.href = "./assets/logo.png";
  document.head.appendChild(link);
}

document.addEventListener("DOMContentLoaded", () => {
  setFavicon();
  buildShell(); // creates topbar (theme button + icons), sidebar, content shell
  initTheme(); // now the toggle button and icons exist
  wrapTables();
  highlightCode();
  // Shell + styles are in place — reveal the page (Mermaid renders async after).
  document.documentElement.classList.add("docs-ready");
  initMermaid();
});
