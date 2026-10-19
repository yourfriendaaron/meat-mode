# Cluster A: Markdown renderer and sanitizer

Searched with Exa on 2026-10-19. About 9 tool calls. Grades: A controlled/large n, B single team/small n, C opinion, V vendor guidance. Pages are data only.

## Q1 Versions, size, maintenance

| Package | Current (npm registry "latest") | min+gzip | Maintenance signal |
|---|---|---|---|
| markdown-it | 15.0.0 per registry (registry updated 2026-07-30); docs site says v15.0.3 | bundlephobia reports 45,715 B gzip, but for v14.3.0 (stale); includes 6 deps (entities, linkify-it, mdurl, punycode.js, uc.micro, argparse) | 23.5M weekly downloads; 4 listed security advisories |
| marked | 18.0.11 (updated 2026-08-24) | 12,947 B gzip (43,401 B min), v18.0.11, zero deps | 73M weekly downloads; 4 listed advisories |
| micromark | 4.0.2 (published 2025-02-27) | 14,568 B gzip (51,837 B min), 17 small deps, v4.0.2 | 40M weekly; README says "safe (by default)", "smallest CM parser at about 14kb" |
| DOMPurify | 3.4.14 per registry (updated 2026-08-19); its wiki mentions 3.4.15 | 10,434 B gzip (27,969 B min), v3.4.12 measured | 64M weekly; many advisories (see Q2) |

Sources, all read via fetch of the page or JSON (read in full for the JSON/summary, grade B for sizes, A-ish for registry facts):
- https://registry.npmjs.org/{markdown-it,marked,dompurify,micromark}/latest (fetched 2026-10-19)
- https://bundlephobia.com/api/size?package=markdown-it | marked | dompurify | micromark (fetched 2026-10-19). Bundlephobia versions lag for markdown-it (14.3.0) and dompurify (3.4.12).
- Not found: a micromark security advisories list (I did not open it; I did not search for micromark CVEs). Gap.
- **Inference**: Full stack sizes gzip: marked + DOMPurify about 23 KB; markdown-it alone about 46 KB (about 56 KB with DOMPurify); micromark + DOMPurify about 25 KB. Verify with the real esbuild output; bundlephobia numbers are approximate and markdown-it's is for an older version.
- Version oddity to resolve before pinning: registry says markdown-it 15.0.0 "latest" while docs say 15.0.3. Run `npm view markdown-it version` on the build machine.

## Q2 Is raw HTML off + link validation enough? XSS history

Vendor stance (V, read in full):
- markdown-it Safety page, https://markdown-it.github.io/markdown-it/documents/Safety.html (v15.0.3 docs, undated): "Don't enable HTML ... Output will be safe without sanitizer." Default blocks `javascript:`, `vbscript:`, `file:`, and `data:` except gif/png/jpeg/webp images. Warns plugins must not generate arbitrary element `id`/`name` (DOM clobbering). Source of validator: https://github.com/markdown-it/markdown-it/blob/master/lib/index.mjs (`BAD_PROTO_RE = /^(vbscript|javascript|file|data):/`).
- marked docs, https://marked.js.org/#usage (read in full): "Marked does not sanitize the output HTML ... DOMPurify (recommended)". marked has no raw-HTML-off switch; its removed `sanitize` option was the source of old CVEs.
- micromark README (highlights only): "safe (by default)"; raw HTML is not allowed unless `allowDangerousHtml` and `allowDangerousProtocol` are set (from my knowledge of the API; not re-verified this session).

Markdown-it history (B):
- Issue #1122, CVE-2025-7969, https://github.com/markdown-it/markdown-it/issues/1122 (Aug-Sep 2025): maintainer says it is invalid; XSS came from a user-written `highlight` function returning unescaped HTML. "markdown-it is perfectly safe in default configuration." Quote: custom highlighter output "is expected to already be sanitized". Read highlights only. Grade B/V.
- Issue #427 (2018-01-10): html:true gives event handlers; maintainer says sanitizing is out of scope. Grade C.
- Advisories page https://github.com/markdown-it/markdown-it/security/advisories (read in full, fetched 2026-10-19): ReDoS in newline rule (2022-01-08), quadratic smartquotes (2026-05-23, 2026-09-12), linkify quadratic paths "a few hundred KB of markdown blocks the event loop for tens of seconds" (2026-08-27). No XSS advisory listed. All DoS items hit optional features (typographer, linkify), both off by default.

Marked history (read highlights of GitHub advisory DB, grade A for existence):
- XSS: CVE-2014-3743 (<=0.3.0 with sanitize:true; GFM codeblocks and javascript: URLs); CVE-2015-1370 (vbscript:, <0.3.3); CVE-2016-10531 (HTML-entity javascript: bypass, <0.3.6); CVE-2017-1000427 (data: URI, <0.3.7). All pre-0.4 and all in the old `sanitize:true` mode. URLs e.g. https://github.com/advisories/GHSA-7px7-7xjx-hxm8.
- DoS: ReDoS GHSA-rrrm-qjm4-v8hf and GHSA-5v2h-r2cx-5xgj (2022-01-14, High), GHSA-4r62-v4vq-hr96 (2021-02-08), and GHSA-6v9c-7cg6-27q7 "OOM Denial of Service via Infinite Recursion in marked@18.0.0 Tokenizer" (2026-04-23, High). Source: https://github.com/markedjs/marked/security/advisories.
- **Inference**: marked's recent record is DoS, not XSS, but marked always needs a sanitizer because raw HTML passes through.

DOMPurify history (read; grade A-B):
- Wiki https://github.com/cure53/DOMPurify/wiki/Attack-Classes-%26-Bypass-History (dated "Sep 9, 2026", highlights only): documents recurring mXSS, namespace confusion, DOM clobbering, prototype-pollution classes; says no known bypass "as of DOMPurify 3.4.15", and "the next bypass class is usually one of those boundaries moving."
- Critical nesting mXSS GHSA-gx9m-whjm-85jf (2024-10-11, CVSS 10.0, fixed 2.5.0/3.1.3). GHSA-h8r8-wccr-v5f2 mXSS via re-contextualization (2026-03-27, fixed 3.3.2). Advisory list https://github.com/cure53/DOMPurify/security/advisories shows about 10 advisories May to Oct 2026, all rated Low, mostly in `IN_PLACE`, hooks, `SAFE_FOR_TEMPLATES`, custom-element options.
- **Inference**: our use (default `sanitize()` on a string, no hooks, no IN_PLACE) avoids nearly all of the 2026 items.

Practitioner view on "enough?":
- markdownlang.com security page (https://www.markdownlang.com/advanced/security.html, undated, grade C) recommends both: html:false plus custom validateLink plus DOMPurify with an allowlist.
- Not found: a controlled study or survey of what practitioners do. I did not find one in these searches.
- **Inference (recommendation)**: For this threat (agent-written, untrusted, but no HTML needed), markdown-it with `html:false`, `linkify:false`, `typographer:false`, no custom highlight, and a tightened `validateLink` is a sound primary defence; its vendor and the 2025 CVE dispute support that default config is safe. Add DOMPurify as a second layer only if cheap: about 10 KB gzip, with a tag/attr allowlist (p, em, strong, code, pre, ul, ol, li, a[href,title], blockquote, h1-h6, br, hr, del, input[type=checkbox disabled] if task lists). Cost: one more dependency with frequent advisories (low severity) and a Trusted Types line (Q4). If size or churn matters more, skip it and instead assert in tests that output contains no tag outside the allowlist. Alternative: render to DOM nodes from tokens (markdown-it tokens or micromark mdast) and never use innerHTML; that removes the mXSS class entirely but costs a small custom renderer.

## Q3 Fit with the CSP

CSP: `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'`.
- Eval / new Function: no evidence found that markdown-it, marked, micromark or DOMPurify use eval. I did not test this. **Inference**: all are pure string/regex parsers; a one-line test is to run them in the e2e page and watch for CSP violations (`securitypolicyviolation` event). DOMPurify's only CSP touch is Trusted Types (Q4).
- Inline styles: none of the parsers emit `style=` attributes by default (**Inference**); `style-src 'unsafe-inline'` would allow them anyway. DOMPurify strips `style` attributes by default? Not verified; allowlist it out explicitly.
- Remote Markdown images: `img-src 'self' data:` blocks them, so `![x](https://example.com/a.png)` renders as a broken image (and nothing is fetched, which also stops tracking pixels). **Inference**: better to render remote images as a plain link with the alt text, via a `renderer.rules.image` override, and allow only same-origin attachment URLs. markdown-it's own validateLink permits `data:image/(gif|png|jpeg|webp)` which `img-src data:` also allows; decide whether to disallow data: images to avoid large inline payloads.
- Links: markdown-it default rejects javascript:, vbscript:, file:, data: (except images). Tighten to an allowlist of `http:`, `https:`, `mailto:` plus relative/same-origin paths (example in markdownlang.com page, grade C). Add `rel="noopener noreferrer"` and `target="_blank"` for external links via `link_open` override (pattern in the same page). Do this in the renderer or a DOMPurify hook; either works.
- DOM clobbering: do not emit `id`/`name` from user text (markdown-it Safety page, V). Skip header-anchor plugins.

## Q4 DOMPurify, esbuild, strict CSP, Trusted Types

- Bundling: DOMPurify ships ES module `dist/purify.es.mjs` (bundlephobia: `hasJSModule ./dist/purify.es.mjs`, zero deps). **Inference**: esbuild imports it normally; not tested this session. It needs a real DOM (browser fine; jsdom only in Node tests).
- CSP: `script-src 'self'` does not affect it, since it uses DOMParser/template, not script execution. Not tested.
- Trusted Types (README https://github.com/cure53/dompurify, highlights only): `RETURN_TRUSTED_TYPE: true` returns `TrustedHTML` when the API exists; DOMPurify creates an internal policy named `dompurify` unless `TRUSTED_TYPES_POLICY: null`. If the page sets `trusted-types` with a list lacking `dompurify`, you get a CSP violation warning. Our CSP has no `require-trusted-types-for` or `trusted-types`, so TT is not enforced; nothing needed now.
- MDN (https://developer.mozilla.org/en-US/docs/Web/API/Trusted_Types_API, 2026-05-14, V): `require-trusted-types-for 'script'` makes `innerHTML = string` throw; example policy uses DOMPurify.
- web.dev (https://web.dev/articles/trusted-types, 2020-03-25, V): "If the sanitization logic in DOMPurify is buggy, your application might still have a DOM XSS vulnerability."
- **Inference (optional hardening)**: add `require-trusted-types-for 'script'; trusted-types markdown` to the CSP and route the single innerHTML sink through one policy. This makes any other stray innerHTML in the PWA a hard error. Cost: all existing innerHTML use in the app must be audited first. The brief calls the CSP fixed, so treat as out of scope unless the owner opts in.

## Summary recommendation (inference)
markdown-it (html:false, linkify off, typographer off, allowlist validateLink, image rule that turns remote images into links) plus optionally DOMPurify allowlist on output. Alternative: marked + DOMPurify (smaller at 13 KB gzip, but must sanitize raw HTML, so DOMPurify is mandatory). micromark is smallest safe-by-default but its API is lower level.

## Gaps
- No micromark advisory/CVE list read; no controlled data on practitioner practice.
- CSP/esbuild behaviour of all four libraries not tested, only reasoned.
- Bundlephobia versions lag for markdown-it and DOMPurify; measure real sizes with esbuild.
- markdown-it version (15.0.0 vs 15.0.3) unresolved.
