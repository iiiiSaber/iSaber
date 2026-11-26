# RSS subscription specification

Status: Approved and implemented locally. November 26, 2026. Production rollout is separate.

Readers can subscribe to `https://isaber.top/feed.xml` in an RSS reader and receive newly published public writing. Publishing maintains the feed automatically. Readers do not need a blog account, and the owner does not need to maintain an XML file.

## First release

| Decision | Behavior |
| --- | --- |
| Format | RSS 2.0, encoded as UTF-8 XML |
| Subscription URL | `https://isaber.top/feed.xml` |
| Included writing | The latest 20 currently public, published articles |
| Languages | English and Chinese together in one feed |
| Entry content | Title, summary, publication date, category when assigned, and article link |
| Order | Publication date descending, independent of global and category pins |
| Subscription entry point | An RSS icon beside GitHub in the public header, plus an RSS link in the shared footer |
| Discovery | An HTML feed discovery link for public page layouts |
| Delivery | Readers poll the URL on their own schedules; updates are not instant push notifications |

Full article content, category feeds, language filters, Atom/JSON feeds, email subscriptions, subscriber accounts, and traffic tracking are outside this first release.

## Reader experience

The header RSS icon uses the existing icon size, spacing, hover color, focus treatment, and touch target. It remains available in the mobile header. Its accessible name is “RSS feed”. The footer link says “RSS”. Both are ordinary links to `/feed.xml`, work without JavaScript, and open in the current tab. Readers can copy the link address into their preferred reader.

Opening the link directly returns XML; the browser may display XML or download it. There is no separate subscription page, modal, or animation. The RSS reader provides the reading interface.

Public page layouts advertise the feed with this discovery metadata. Owner administration pages do not need it. A signed-in owner visiting a public page discovers the same public feed.

```html
<link rel="alternate" type="application/rss+xml"
      title="iSaber RSS" href="https://isaber.top/feed.xml">
```

This follows the RSS Advisory Board's [feed discovery specification](https://www.rssboard.org/rss-autodiscovery).

## Article selection and publication behavior

An entry is eligible only when its article has a current published revision and `visibility = public`. All displayed metadata comes from that published revision, never from an unpublished draft. The article's existing publication timestamp supplies the feed date.

Select at most 20 entries using `published_at DESC, id DESC`. The article ID resolves timestamp ties deterministically. Global pin state, manual pin order, category pins, writing dates, update dates, Archive filters, and visitor preferences do not affect selection or ordering. Feed query parameters do not provide filters, authentication, or an owner view.

| Owner action | Feed result on the next successful request |
| --- | --- |
| First publication as public | Article enters the feed if it falls within the latest 20 |
| Publish or edit a private article | Article remains excluded |
| Save an unpublished revision | Existing public entry remains unchanged |
| Publish a changed title, summary, or category | Entry metadata changes; its ID and original publication date stay the same |
| Change only the article body | The summary feed stays unchanged unless its included metadata also changes |
| Pin, unpin, or reorder pins | Feed selection and order stay unchanged |
| Unpublish or make private | Entry is removed; the next eligible article fills its place |
| Restore public visibility or republish an older article | Original ID and publication date remain; the article returns only if it belongs in the latest 20 |

An older article becoming public is not automatically treated as a new publication. The feed is a window into recent public writing, not a complete historical export. A reader that does not refresh before more than 20 newer publications arrive may miss entries that have left that window. Individual readers decide how to present metadata updates and whether to notify the subscriber.

## XML content

The document follows the [RSS 2.0 specification](https://www.rssboard.org/rss-specification). These are application choices within that format:

| Element | Value |
| --- | --- |
| Channel `title` | `iSaber` |
| Channel `link` | Canonical site origin plus `/` |
| Channel `description` | `Personal journals, technical articles, and things I make. English and 中文.` |
| Channel `atom:link` | Self reference to the canonical `/feed.xml`, with `rel="self"` and `type="application/rss+xml"` |
| Entry `title` | Published article title |
| Entry `link` | Absolute canonical `/articles/{slug}` URL |
| Entry `guid` | `urn:uuid:{article_id}`, with `isPermaLink="false"` |
| Entry `description` | Published summary, treated as text; omitted when blank |
| Entry `pubDate` | Existing `published_at` timestamp in an RSS-compatible date format using UTC, such as `Thu, 26 Nov 2026 04:00:00 GMT` |
| Entry `category` | Published category name; omitted when unassigned |

The Atom self reference uses the namespace `http://www.w3.org/2005/Atom`; this remains an RSS feed. Stable GUIDs help readers recognize previously received entries, as described in the [RSS best practices profile](https://www.rssboard.org/rss-profile#element-channel-item-guid).

Summaries use the stored published summary without falling back to Markdown or draft content. They are escaped for HTML text display inside the XML description, then serialized safely as XML. A summary containing `<script>`, `&`, quotation marks, or `]]>` must remain harmless readable text. Preserve Chinese characters, emoji, and ordinary punctuation; remove XML 1.0 prohibited control characters during serialization.

The first release omits article HTML, images, enclosures, comments, tags, author email, and custom date extensions. It also omits the optional channel language because the feed is bilingual, and omits `lastBuildDate` rather than inventing a publication-change timestamp. No request-time clock value is inserted into the feed body, so unchanged content produces unchanged XML.

## Privacy and caching

RSS is always anonymous and public, regardless of cookies, session state, or authentication headers. Its query must enforce public visibility explicitly instead of using the optional owner-reading context. The frontend must not forward cookies or authorization to the backend feed endpoint. The feed sets no cookies and contains no private titles, summaries, IDs, category metadata, drafts, or private asset links. These rules extend [PRIV-01](requirements.md#priv-01-private-content).

Generate the feed from a fresh database snapshot on each request. There is no shared application cache, build-time feed snapshot, or stale-on-error fallback. This prevents application-controlled responses from continuing to serve an article after it becomes private or unpublished.

Successful responses use `Cache-Control: no-cache, max-age=0, must-revalidate` and a weak ETag derived from the generated feed bytes. Clients may store a copy, but must revalidate it before reuse. Conditional requests evaluate the current public selection before deciding whether to return `304 Not Modified`; a removal must not return `304` against the old feed. Changed pins, private-only edits, and unrelated owner state must not change the feed or its ETag. The feed does not use `Last-Modified` in this release. These semantics follow [HTTP caching rules](https://www.rfc-editor.org/rfc/rfc9111.html).

RSS readers and other third parties may retain content they previously downloaded. Making an article private stops future publication by this application; it cannot recall those external copies.

## Implementation approach

The Go backend owns article selection and XML generation. A Next.js route exposes the public subscription URL through the existing frontend origin. The backend produces one complete document before response headers are sent, so query or serialization errors cannot leave a partially successful feed.

| Area | Proposed change |
| --- | --- |
| Go publishing service | Add a dedicated public feed query and XML serializer; fetch current published metadata and category names in one consistent database snapshot, bounded to 20 entries |
| API routing | Add `/api/v1/feed.xml` to the public router and its explicit dispatch allowlist |
| Next.js | Add `/feed.xml` as a dynamic route handler that forwards the public feed without using owner-reading helpers or shared fetch caching |
| Canonical URLs | Reuse the backend's validated `SITE_ORIGIN` for all feed links, and pass the same public origin to the frontend for discovery metadata. Production requires HTTPS; local development uses its loopback origin. Do not trust request `Host` or forwarded-host headers |
| Public layout | Add feed discovery metadata using the canonical URL, separately from administration metadata |
| Header and footer | Add accessible RSS links and check the header breakpoint with the signed-in Admin item present |
| API contract | Document RSS response content, conditional requests, errors, and supported methods |

There is no database migration, new worker, scheduled job, XML file on disk, or third-party subscription service. Generate XML with Go's standard encoder rather than interpolating article text into an XML template. The proxy must preserve the public content type, ETag, cache policy, status, and relevant error headers while dropping upstream cookies.

### HTTP behavior

Both endpoints support `GET` and `HEAD`; unsupported methods return `405` with `Allow: GET, HEAD`.

| Condition | Response |
| --- | --- |
| Available articles, including an empty collection | `200`, `Content-Type: application/rss+xml; charset=utf-8`, valid RSS with zero to 20 items |
| Matching `If-None-Match` after querying current public data | `304`, no body, current ETag and cache policy |
| Database or feed-generation failure | `503`, public problem response, `Cache-Control: no-store`, `Retry-After: 60` |
| Backend unreachable or its response times out | Frontend returns `503` with the same public error policy |

`HEAD` returns the corresponding status and headers without a body. An outage must not masquerade as a successful empty feed. The endpoint does not redirect to login, reflect private error details, or return a successful HTML page. Existing canonical-domain redirects continue to apply.

## Acceptance criteria

1. XML parsing confirms RSS 2.0 structure, channel metadata, self reference, absolute URLs, UTC dates, and stable GUIDs. English, Chinese, emoji, XML-sensitive text, blank summaries, and invalid control characters are covered.
2. With more than 20 eligible articles, the newest 20 appear in deterministic publication order. Pins and manual pin order cannot promote older entries. Zero articles returns a valid empty feed.
3. Guest and signed-in owner requests produce identical feed content. Private publications, drafts, unpublished revisions, and private-only category metadata are absent from the XML and validators.
4. Publishing, public metadata changes, unpublishing, and visibility changes have the behavior in the action table. Existing entry IDs and original publication dates survive edits and republishing.
5. Repeated unchanged requests produce stable bytes and ETags. A matching conditional request returns `304`; a public-to-private change with the old ETag returns `200` with the entry removed, even if the owner is signed in.
6. `GET`, `HEAD`, method rejection, backend failure, and proxy failure satisfy the HTTP table. No response sets a session cookie; the proxy forwards no visitor credentials.
7. Header/footer links and feed discovery metadata point to the canonical subscription URL. Verify keyboard access, useful accessible names, 44px touch targets, light/dark themes, and no header overflow at 320, 390, 768, and 1440 CSS pixels, including the owner Admin link.
8. A real RSS reader can subscribe and show a newly published synthetic article after refresh. Its summary and article link work, and refreshes do not duplicate the entry after metadata edits. Record the reader and version used.

## Approved defaults

The approved defaults are **summaries**, **20 recent articles**, **one bilingual feed**, and **an RSS icon in the header plus a footer link**. Pushing and production rollout follow the owner's release instruction.

## Configuration and rollout

The backend already receives the validated canonical `SITE_ORIGIN`. The frontend now also needs that value in production for feed discovery. Both Compose files provide it: production uses `https://${BLOG_DOMAIN}`, and rehearsal uses `https://localhost`. The build uses an isolated `https://build.invalid` value; request-time metadata uses the deployment value, not the build value. Development defaults to a loopback origin. Set `APP_ENV=development` explicitly when serving a local production build over loopback HTTP; production mode continues to require HTTPS.

Before activating the new application images, the operator must install the reviewed `infra/compose.yml` on the host under the existing deployment lock. The normal image deployer does not upload Compose files. This configuration step passes `SITE_ORIGIN` to the frontend; deploying only the new image against the old Compose file would leave required production metadata configuration missing. Preserve the host's existing environment, credentials, certificate mounts, and persistent volumes. No new database migration or Caddy change is needed.

After release, verify trusted HTTPS for `/feed.xml`, its canonical self/article links, the public header/footer links, conditional `304` responses, and a public-to-private change. Do not activate a cloud release as part of local UI review.

## Local verification

- Go unit tests, race checks, and the full PostgreSQL integration suite passed using disposable synthetic data. RSS tests cover publication lifecycle, draft/private exclusion, pin independence, chronological limits/ties, blank summaries, XML escaping, stable identifiers, conditional GET/HEAD, unsupported methods, and outages.
- The frontend production build, lint, and all 40 tests passed. Proxy tests cover omitted credentials/query parameters, cookie removal, conditional responses, HEAD, rejected methods, sanitized outages, redirects, invalid content types, oversized/truncated upstream documents, runtime canonical configuration, and discovery metadata.
- Actual Go/Next browser acceptance verified publishing and reading, equal guest/owner RSS, canonical discovery, and keyboard subscription. Guest and owner headers had no horizontal overflow at 320, 390, 620, 768, and 1440 CSS pixels in both themes.
- Newsboat 2.21.0 subscribed to the actual local endpoint, received a newly published synthetic article on refresh, and updated an existing entry after a metadata edit without duplication. Feedparser 6.0.14 independently parsed the RSS and Atom self reference without errors. A visibility change returned fresh XML with the original entry removed even when the owner supplied its prior ETag.

The container smoke check also covers empty RSS, runtime canonical links, and conditional requests for the next container rehearsal. No remote CI, push, production mutation, or database migration is part of this local implementation.
