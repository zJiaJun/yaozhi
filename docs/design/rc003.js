// SPDX-License-Identifier: GPL-3.0-only
//
// Front view of the Xiaomi Bluetooth Remote 2 Pro (RC003), drawn from scratch
// as vector shapes for the design prototypes. Only the layout and materials
// follow the real product; no photo is used and no logo or wordmark is drawn.
//
// rc003SVG({prefix}) returns SVG markup. Every key carries data-k="<key>" and
// the class "k" (or "kw" for the four arrow zones of the ring), plus "kd" for
// the dark keys and "kl" for the two light keys at the top. Ids are prefixed
// so several remotes can share a page: `${prefix}voice` (the voice key),
// `${prefix}spec` (the moving highlight band), `${prefix}voiceg` (the red fill
// pages use for a live voice key).

const RC003_KEYS = {
  power: 'power', voice: 'voice', up: 'up', down: 'down', left: 'left', right: 'right',
  ok: 'ok', back: 'back', home: 'home', menu: 'menu', volup: 'volup', voldown: 'voldown', tv: 'tv',
};

// Geometry in the 200 × 840 viewBox (about the real 1 : 4.2 proportion).
const RC003_GEOM = {
  w: 200, h: 840,
  power: { x: 58, y: 80, r: 22 }, voice: { x: 142, y: 80, r: 22 },
  ring: { x: 100, y: 208, r: 83, inner: 44 },
  left: { x: 62, ys: [348, 425, 502], r: 31 },
  vol: { x: 110, y: 317, w: 62, h: 139 }, tv: { x: 141, y: 502, r: 31 },
};

function rc003Wedge(cx, cy, r1, r2, a0, a1) {
  const p = (r, a) => [cx + r * Math.cos(a * Math.PI / 180), cy + r * Math.sin(a * Math.PI / 180)];
  const [x0, y0] = p(r2, a0), [x1, y1] = p(r2, a1), [x2, y2] = p(r1, a1), [x3, y3] = p(r1, a0);
  return `M${x0},${y0} A${r2},${r2} 0 0 1 ${x1},${y1} L${x2},${y2} A${r1},${r1} 0 0 0 ${x3},${y3} Z`;
}

function rc003SVG({ prefix = 'rc-', cls = '' } = {}) {
  const P = prefix, G = RC003_GEOM, R = G.ring, L = G.left, V = G.vol, T = G.tv;
  const dark = (k, cx, cy, r) =>
    `<circle class="k kd" data-k="${k}" cx="${cx}" cy="${cy}" r="${r}" fill="url(#${P}dk)" stroke="#000" stroke-opacity=".55" stroke-width="1"/>` +
    `<circle cx="${cx}" cy="${cy}" r="${r - 1.2}" fill="none" stroke="#fff" stroke-opacity=".10" stroke-width="1" pointer-events="none"/>`;
  const light = (k, cx, cy, r, id) =>
    `<circle class="k kl" data-k="${k}"${id ? ` id="${id}"` : ''} cx="${cx}" cy="${cy}" r="${r}" fill="url(#${P}lk)" stroke="#2b2c30" stroke-width="1.6"/>`;
  const ink = 'stroke="#ececef" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" fill="none" pointer-events="none"';
  const inkDark = 'stroke="#2b2c30" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" fill="none" pointer-events="none"';
  const [yb, yh, ym] = L.ys;
  const vm = V.y + V.h / 2, vx = V.x + V.w / 2;
  return `
<svg viewBox="0 0 ${G.w} ${G.h}" class="${cls}" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <linearGradient id="${P}side" x1="0" x2="1">
      <stop offset="0" stop-color="#6f7177"/><stop offset=".035" stop-color="#a9abb1"/><stop offset=".09" stop-color="#e2e3e6"/>
      <stop offset=".5" stop-color="#d4d5d9"/><stop offset=".91" stop-color="#e0e1e4"/><stop offset=".965" stop-color="#a3a5ab"/><stop offset="1" stop-color="#66686e"/>
    </linearGradient>
    <linearGradient id="${P}top" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#fff" stop-opacity=".45"/><stop offset=".12" stop-color="#fff" stop-opacity=".08"/>
      <stop offset=".6" stop-color="#fff" stop-opacity="0"/><stop offset="1" stop-color="#000" stop-opacity=".16"/>
    </linearGradient>
    <pattern id="${P}brush" width="3" height="40" patternUnits="userSpaceOnUse">
      <rect width="3" height="40" fill="none"/><line x1=".5" y1="0" x2=".5" y2="40" stroke="#fff" stroke-opacity=".10"/><line x1="2" y1="0" x2="2" y2="40" stroke="#000" stroke-opacity=".035"/>
    </pattern>
    <linearGradient id="${P}specg" x1="0" x2="1"><stop offset="0" stop-color="#fff" stop-opacity="0"/><stop offset=".5" stop-color="#fff" stop-opacity=".55"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></linearGradient>
    <radialGradient id="${P}lk" cx=".42" cy=".32" r=".8"><stop offset="0" stop-color="#fbfbfc"/><stop offset=".7" stop-color="#e3e4e7"/><stop offset="1" stop-color="#c7c8cd"/></radialGradient>
    <radialGradient id="${P}dk" cx=".4" cy=".28" r=".85"><stop offset="0" stop-color="#55565c"/><stop offset=".55" stop-color="#36373c"/><stop offset="1" stop-color="#1f2024"/></radialGradient>
    <radialGradient id="${P}ring" cx=".45" cy=".35" r=".8"><stop offset="0" stop-color="#2a2b30"/><stop offset=".7" stop-color="#121316"/><stop offset="1" stop-color="#070708"/></radialGradient>
    <radialGradient id="${P}okg" cx=".42" cy=".3" r=".85"><stop offset="0" stop-color="#2c2d32"/><stop offset="1" stop-color="#0d0e10"/></radialGradient>
    <radialGradient id="${P}voiceg" cx=".42" cy=".32" r=".85"><stop offset="0" stop-color="#ff9d9d"/><stop offset="1" stop-color="#e5484d"/></radialGradient>
    <clipPath id="${P}clip"><rect x="4" y="4" width="192" height="832" rx="15"/></clipPath>
    <filter id="${P}soft" x="-20%" y="-20%" width="140%" height="140%"><feGaussianBlur stdDeviation="1.2"/></filter>
  </defs>

  <!-- body: flat aluminium slab, rounded long edges -->
  <rect x="4" y="4" width="192" height="832" rx="15" fill="url(#${P}side)"/>
  <rect x="4" y="4" width="192" height="832" rx="15" fill="url(#${P}brush)"/>
  <rect x="4" y="4" width="192" height="832" rx="15" fill="url(#${P}top)"/>
  <g clip-path="url(#${P}clip)"><rect id="${P}spec" x="62" y="0" width="34" height="840" fill="url(#${P}specg)" opacity=".5"/></g>
  <rect x="4.5" y="4.5" width="191" height="831" rx="14.5" fill="none" stroke="#fff" stroke-opacity=".7"/>
  <rect x="5.5" y="5.5" width="189" height="829" rx="13.5" fill="none" stroke="#000" stroke-opacity=".10"/>

  <!-- top row: light keys with a thin dark ring -->
  <g class="rings">${[0, 1, 2].map(() => `<circle cx="${G.voice.x}" cy="${G.voice.y}" r="${G.voice.r}"/>`).join('')}</g>
  ${light('power', G.power.x, G.power.y, G.power.r)}
  <path d="M58 70.5v8.5M50.6 75.5a10.2 10.2 0 1 0 14.8 0" ${inkDark}/>
  ${light('voice', G.voice.x, G.voice.y, G.voice.r, `${P}voice`)}
  <rect x="137.6" y="68.5" width="8.8" height="13.5" rx="4.4" ${inkDark}/>
  <path d="M133.6 78.5a8.4 8.4 0 0 0 16.8 0M142 87v3" ${inkDark}/>
  <circle class="ready-dot" cx="${G.voice.x}" cy="${G.voice.y - 34}" r="2.4"/>

  <!-- navigation ring -->
  <circle cx="${R.x}" cy="${R.y}" r="${R.r + 2.2}" fill="none" stroke="#000" stroke-opacity=".28" stroke-width="2"/>
  <circle cx="${R.x}" cy="${R.y}" r="${R.r}" fill="url(#${P}ring)"/>
  <circle cx="${R.x}" cy="${R.y}" r="${R.r - .8}" fill="none" stroke="#fff" stroke-opacity=".10" stroke-width="1.2"/>
  <path class="kw" data-k="up" d="${rc003Wedge(R.x, R.y, R.inner + 3, R.r, -135, -45)}"/>
  <path class="kw" data-k="right" d="${rc003Wedge(R.x, R.y, R.inner + 3, R.r, -45, 45)}"/>
  <path class="kw" data-k="down" d="${rc003Wedge(R.x, R.y, R.inner + 3, R.r, 45, 135)}"/>
  <path class="kw" data-k="left" d="${rc003Wedge(R.x, R.y, R.inner + 3, R.r, 135, 225)}"/>
  ${[[0, -73], [73, 0], [0, 73], [-73, 0]].map(([dx, dy]) => `<circle cx="${R.x + dx}" cy="${R.y + dy}" r="1.8" fill="#9a9ba1" pointer-events="none"/>`).join('')}
  <circle cx="${R.x}" cy="${R.y}" r="${R.inner + 2}" fill="#000" opacity=".55" pointer-events="none"/>
  <circle class="k kd" data-k="ok" cx="${R.x}" cy="${R.y}" r="${R.inner}" fill="url(#${P}okg)" stroke="#fff" stroke-opacity=".09"/>

  <!-- left column -->
  ${dark('back', L.x, yb, L.r)}<path d="M65.5 339l-8.5 9 8.5 9" ${ink}/>
  ${dark('home', L.x, yh, L.r)}<path d="M53.5 427l8.5-8 8.5 8M56.5 425v9.5h11V425" ${ink}/>
  ${dark('menu', L.x, ym, L.r)}<path d="M53.5 494.5h17M53.5 502h17M53.5 509.5h17" ${ink}/>

  <!-- volume rocker and TV -->
  <rect x="${V.x - 1}" y="${V.y - 1}" width="${V.w + 2}" height="${V.h + 2}" rx="${V.w / 2 + 1}" fill="#000" opacity=".5"/>
  <path class="k kd" data-k="volup" fill="url(#${P}dk)" d="M${V.x} ${vm}V${V.y + V.w / 2}a${V.w / 2} ${V.w / 2} 0 0 1 ${V.w} 0V${vm}z"/>
  <path class="k kd" data-k="voldown" fill="url(#${P}dk)" d="M${V.x} ${vm}h${V.w}V${V.y + V.h - V.w / 2}a${V.w / 2} ${V.w / 2} 0 0 1 -${V.w} 0z"/>
  <path d="M${V.x + 6} ${vm}h${V.w - 12}" stroke="#000" stroke-opacity=".6" stroke-width="1.2"/>
  <path d="M${V.x + 6} ${vm + 1.4}h${V.w - 12}" stroke="#fff" stroke-opacity=".07" stroke-width="1"/>
  <path d="M${vx} ${V.y + 22}v14M${vx - 7} ${V.y + 29}h14M${vx - 7} ${V.y + V.h - 30}h14" ${ink}/>
  ${dark('tv', T.x, T.y, T.r)}
  <rect x="${T.x - 11.5}" y="${T.y - 8.5}" width="23" height="17" rx="4.5" stroke="#ececef" stroke-width="2" fill="none" pointer-events="none"/>
  <text x="${T.x}" y="${T.y + 3.2}" text-anchor="middle" font-size="8.6" font-weight="800" fill="#ececef" pointer-events="none" font-family="-apple-system,Helvetica,sans-serif">TV</text>
</svg>`;
}
