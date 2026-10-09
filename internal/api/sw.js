// The hub's service worker (PLAN.md, Offline; Notifications). Two jobs.
//
// It keeps what a reader has seen on the device, so the hub opens and
// reads without a network. A page — the feed, a thread, a profile, a
// search — is asked of the hub first, every time, and kept as it comes;
// the kept copy shows when the hub cannot be reached: no network, no
// answer in three seconds, or the edge's 502–504 while the hub restarts.
// The copy says so: the worker writes the time it was served onto its
// <html> (data-saved), the page shows its Offline strip, and a live page
// catches up by itself once the hub answers. A page never kept gets
// /offline, kept as the worker installs, which loads the page it stands
// for once the hub answers — or once the answer the worker is still
// waiting for lands. Pictures are content-addressed and never
// change, so a kept one is shown without asking, and a page is kept with
// its pictures. Posting, the event stream, the JSON API, video and sound
// go to the network as if there were no worker.
//
// And it shows the hub's pushes, opening the page on a click.
//
// The hub stamps VERSION from the pages' bytes (the template, their
// words, this file), so a deploy that changes any of them is a byte-
// different worker: it installs, and fetches the offline page anew.
const VERSION = "__HUB_BUILD__";
const SHELL = "hub-shell-" + VERSION, PAGES = "hub-pages", FILES = "hub-files";
const KEEP_PAGES = 150, KEEP_FILES = 800, FILE_MAX = 12 << 20, WAIT = 3000;
// the pages kept: the feed (any of its pages), a thread, a profile, a search
const PAGE = /^\/(?:p\/[0-9a-f]{8,64}|u\/[0-9a-f]{16}(?:\/(?:notifications|bookmarks))?|search)?$/i;
// kept, and asked for again each time: shown at once, refreshed behind
const ICONS = new Set(["/favicon.ico", "/apple-touch-icon.png", "/icon-192.png", "/icon-512.png", "/icon-maskable-512.png", "/manifest.webmanifest"]);

const key = url => url.origin + url.pathname + url.search;
const any = { ignoreVary: true }; // a page varies by Accept-Language, which one browser always sends the same
// work that outlives the answer: the copy written, the cache trimmed
const later = (e, p) => { p = p.catch(() => {}); try { e.waitUntil(p); } catch (err) {} };

self.addEventListener("install", e => {
  e.waitUntil((async () => {
    const shell = await caches.open(SHELL);
    await shell.add(new Request("/offline", { cache: "reload" }));
    // the start page, so an app opened offline right after it was
    // installed has its feed
    const pages = await caches.open(PAGES), home = new URL("/", self.location.origin);
    if (!(await pages.match(key(home), any))) await save(pages, home).catch(() => {});
    await self.skipWaiting();
  })());
});

self.addEventListener("activate", e => {
  e.waitUntil((async () => {
    for (const k of await caches.keys()) if (k !== SHELL && k !== PAGES && k !== FILES) await caches.delete(k);
    // the browser starts a page's request while the worker boots
    if (self.registration.navigationPreload) await self.registration.navigationPreload.enable();
    await self.clients.claim();
  })());
});

// A page open as the worker took over was loaded before it, and asks to
// be kept (the registering script, on controllerchange). Asked of the
// page rather than done as the worker activates, which would hold every
// request until the page and its pictures were in.
self.addEventListener("message", e => {
  const u = e.data && e.data.keep;
  if (typeof u !== "string") return;
  const url = new URL(u, self.location.origin);
  if (url.origin !== self.location.origin || !PAGE.test(url.pathname)) return;
  e.waitUntil(caches.open(PAGES).then(async pages => { if (!(await pages.match(key(url), any))) await save(pages, url); }).catch(() => {}));
});

self.addEventListener("fetch", e => {
  const r = e.request, url = new URL(r.url);
  if (r.method !== "GET" || url.origin !== self.location.origin) return;
  if (r.mode === "navigate") {
    // a top-level page; a frame (the replies under a blog post) is left alone
    if (r.destination === "document") e.respondWith(PAGE.test(url.pathname) ? page(e, url) : other(e, url));
    return;
  }
  // a live page fetching itself: the network's, and the kept copy follows it
  if (r.headers.get("X-Hub-Live") && PAGE.test(url.pathname)) return e.respondWith(fetch(r).then(res => keep(e, url, res)));
  if (r.headers.has("Range")) return; // video and sound: asked for in spans, never kept
  if (url.pathname.startsWith("/v1/embed/")) return e.respondWith(file(e, url));
  if (url.pathname.startsWith("/v1/identicon/") || ICONS.has(url.pathname)) return e.respondWith(icon(e, url));
});

// A page: the hub's answer when it comes in time; the kept copy, marked,
// when it does not come or does not come in time; the offline page when
// none was kept. The hub's answer is kept whenever it comes, even after
// the copy or the offline page was shown.
//
// In time is three seconds. A link that stalls rather than fails — weak
// Wi-Fi, a tunnel dropping packets — never fails the fetch, so the
// deadline is the only way out, and with no kept copy it used to give
// nothing: the page hung until the browser gave up (Codex, 2026-10-08).
// Now it gives the offline page, and remembers that one waits at that
// address (waiting). The fetch runs on; when its answer lands and is
// kept, the window at that address is told (tell), the offline page
// loads itself, and that load is handed the copy just kept (arrived):
// at once and as the hub served it, since it is the hub's answer to
// the question the page asked a moment ago, rather than the same slow
// link asked again and three more seconds waited on it.
const waiting = new Map(); // key → true: the offline page stands at that address, its fetch in flight
const arrived = new Map(); // key → when the page landed for the offline page waiting there
async function page(e, url) {
  const k = key(url), pages = await caches.open(PAGES);
  const net = Promise.resolve(e.preloadResponse).then(res => res || fetch(e.request)).then(res => keep(e, url, res, pages));
  later(e, net);
  const at = arrived.get(k);
  arrived.delete(k);
  if (at && Date.now() - at < 30000) {
    const res = await pages.match(k, any);
    if (res) return res;
  }
  const saved = async () => { const res = await pages.match(k, any); return res && marked(res); };
  return new Promise(resolve => {
    let done = false;
    const give = res => { if (!res || done) return false; done = true; resolve(res); return true; };
    const away = async res => { give(await saved()) || give(await offline(url)) || give(res || Response.error()); };
    const slow = setTimeout(async () => {
      if (give(await saved())) return;
      if (give(await offline(url))) waiting.set(k, true);
    }, WAIT);
    net.then(res => {
      clearTimeout(slow);
      // the edge's own answer while the hub restarts is no page
      if (res.status >= 502 && res.status <= 504) away(res); else give(res);
    }, () => { clearTimeout(slow); waiting.delete(k); away(); });
  });
}

// the offline page standing at that address hears that its page is here
async function tell(k) {
  for (const c of await self.clients.matchAll({ type: "window" })) if (key(new URL(c.url)) === k) c.postMessage({ arrived: k });
}

// any other top-level load: the network's, or the offline page
async function other(e, url) {
  try {
    const res = (await e.preloadResponse) || await fetch(e.request);
    return res.status >= 502 && res.status <= 504 ? (await offline(url)) || res : res;
  } catch (err) {
    return (await offline(url)) || Response.error();
  }
}

// The hub's answer for a page, kept and handed on: the page kept with its
// pictures (and the offline page in its language), a page the hub says
// is gone let go; an offline page waiting for it told, once it is kept.
function keep(e, url, res, pages) {
  const k = key(url);
  if (res.status === 200 && /^text\/html/.test(res.headers.get("Content-Type") || "")) {
    const copy = res.clone();
    later(e, (async () => {
      const c = pages || await caches.open(PAGES);
      await c.put(k, copy);
      if (waiting.delete(k)) {
        for (const [o, t] of arrived) if (Date.now() - t > 30000) arrived.delete(o);
        arrived.set(k, Date.now());
        await tell(k);
      }
      await trim(PAGES, KEEP_PAGES);
      const lang = url.searchParams.get("lang");
      if (lang) await offlineIn(lang);
      const kept = await c.match(k, any);
      if (kept) await pictures(await kept.text());
    })());
  } else {
    waiting.delete(k); // nothing landed for it; its offline page asks the hub itself
    if (res.status === 404 || res.status === 410) later(e, caches.open(PAGES).then(c => c.delete(k, any)));
  }
  return res;
}

// a page fetched by the worker itself, to keep with its pictures; marked
// as the pages' own refetch is, so it is no page view (PLAN.md, Stats)
async function save(pages, url) {
  const res = await fetch(url.href, { cache: "no-store", headers: { "X-Hub-Live": "1" } });
  if (res.status !== 200) return;
  await pages.put(key(url), res.clone());
  await pictures(await res.text());
}

// the kept copy as the page it was, its <html> carrying when it was served
async function marked(res) {
  const at = Date.parse(res.headers.get("Date") || "") || Date.now();
  const h = new Headers(res.headers);
  h.delete("Content-Length");
  return new Response((await res.text()).replace("<html", '<html data-saved="' + at + '"'), { status: 200, headers: h });
}

// the offline page, in the language the address asked for
async function offline(url) {
  const shell = await caches.open(SHELL), lang = url.searchParams.get("lang");
  return (lang && await shell.match("/offline?lang=" + encodeURIComponent(lang), any)) || await shell.match("/offline", any);
}
async function offlineIn(lang) {
  const shell = await caches.open(SHELL), k = "/offline?lang=" + encodeURIComponent(lang);
  if (!(await shell.match(k, any))) await shell.add(new Request(k, { cache: "reload" }));
}

const keepable = res => res.status === 200 && res.type === "basic" && +(res.headers.get("Content-Length") || 0) <= FILE_MAX;

// a picture (or a page an admin embedded): the kept one, else the network's, kept
async function file(e, url) {
  const c = await caches.open(FILES);
  const hit = await c.match(url.href);
  if (hit) return hit;
  const res = await fetch(e.request);
  if (keepable(res)) later(e, c.put(url.href, res.clone()).then(() => trim(FILES, KEEP_FILES)));
  return res;
}

// an id's face, an icon, the manifest: the kept one at once, refreshed behind
async function icon(e, url) {
  const c = await caches.open(FILES);
  const hit = await c.match(url.href);
  const net = fetch(e.request).then(async res => {
    if (keepable(res)) { await c.put(url.href, res.clone()); await trim(FILES, KEEP_FILES); }
    return res;
  });
  if (hit) { later(e, net); return hit; }
  return net;
}

// The pictures a kept page shows — a post's pictures, a drawing's bytes,
// a video's still, an avatar or an id's face — fetched and kept, four at
// a time, so the page read offline has them, the ones below the fold
// too. Never a video or a sound itself; nothing at all when the reader
// asked the browser to save data.
async function pictures(html) {
  if (self.navigator.connection && self.navigator.connection.saveData) return;
  const want = new Set();
  for (const m of html.matchAll(/<img\b[^>]*?\s(?:src|data-src)="(\/v1\/(?:embed|identicon)\/[^"]+)"/g)) want.add(m[1]);
  for (const m of html.matchAll(/\sposter="(\/v1\/embed\/[^"]+)"/g)) want.add(m[1]);
  const c = await caches.open(FILES), todo = [];
  for (const p of want) {
    const u = new URL(p, self.location.origin).href;
    if (!(await c.match(u))) todo.push(u);
  }
  const one = async () => {
    for (let u; (u = todo.shift());) {
      try { const res = await fetch(u); if (keepable(res)) await c.put(u, res); } catch (err) {}
    }
  };
  await Promise.all([one(), one(), one(), one()]);
  await trim(FILES, KEEP_FILES);
}

// A cache holds its newest entries (a page asked for again is put again,
// so newest is last read), the oldest let go past the limit. One trim at
// a time a cache; one asked for meanwhile runs again after it.
const trims = {};
function trim(name, max) {
  const t = trims[name];
  if (t) { t.again = true; return t.p; }
  const me = trims[name] = { again: false };
  return me.p = (async () => {
    try {
      do {
        me.again = false;
        const c = await caches.open(name), ks = await c.keys();
        for (const k of ks.slice(0, Math.max(0, ks.length - max))) await c.delete(k);
      } while (me.again);
    } finally { delete trims[name]; }
  })();
}

self.addEventListener("push", e => {
  let m = {};
  try { m = e.data ? e.data.json() : {}; } catch (err) {}
  e.waitUntil(self.registration.showNotification(m.title || "New post", {
    body: m.body || "", icon: "/icon-192.png", tag: m.tag, data: { url: m.url || "/" } }));
});
self.addEventListener("notificationclick", e => {
  e.notification.close();
  const url = new URL((e.notification.data && e.notification.data.url) || "/", self.location.origin).href;
  e.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then(list => {
    const open = list.find(c => c.url === url);
    return open ? open.focus() : self.clients.openWindow(url);
  }));
});
