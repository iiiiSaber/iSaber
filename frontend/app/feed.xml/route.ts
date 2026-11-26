import { backendOrigin } from "../../lib/public-api";

export const dynamic = "force-dynamic";

function unavailable(head = false) {
  const requestID = crypto.randomUUID();
  return new Response(head ? null : JSON.stringify({ type: "about:blank", title: "Service Unavailable", status: 503, code: "feed_unavailable", request_id: requestID }), {
    status: 503, headers: { "Content-Type": "application/problem+json", "Cache-Control": "no-store", "Retry-After": "60", "X-Content-Type-Options": "nosniff", "X-Request-ID": requestID },
  });
}

async function feed(request: Request) {
  const head = request.method === "HEAD";
  try {
    const headers = new Headers();
    const validator = request.headers.get("if-none-match");
    if (validator) headers.set("If-None-Match", validator);
    const upstream = await fetch(`${backendOrigin()}/api/v1/feed.xml`, {
      method: head ? "HEAD" : "GET", headers, cache: "no-store", credentials: "omit", redirect: "error", signal: AbortSignal.timeout(5000),
    });
    if (![200, 304].includes(upstream.status) ||
        (upstream.status === 200 && !/^application\/rss\+xml(?:;|$)/i.test(upstream.headers.get("content-type") ?? "")) ||
        !upstream.headers.get("etag")) {
      await upstream.body?.cancel();
      return unavailable(head);
    }
    // Copy only public feed headers. Never forward upstream cookies or private error bodies.
    const responseHeaders = new Headers({ "Cache-Control": "no-cache, max-age=0, must-revalidate", "X-Content-Type-Options": "nosniff" });
    responseHeaders.set("Content-Type", "application/rss+xml; charset=utf-8");
    responseHeaders.set("ETag", upstream.headers.get("etag") ?? "");
    if (head || upstream.status === 304) return new Response(null, { status: upstream.status, headers: responseHeaders });
    // Buffer a bounded document so interrupted upstream bodies become a 503, not partial XML.
    const reader = upstream.body?.getReader();
    if (!reader) return unavailable();
    const chunks: Uint8Array[] = [];
    let size = 0;
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > 1024 * 1024) { await reader.cancel(); return unavailable(); }
        chunks.push(value);
      }
    } finally { reader.releaseLock(); }
    const body = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) { body.set(chunk, offset); offset += chunk.byteLength; }
    return new Response(body, { status: 200, headers: responseHeaders });
  } catch { return unavailable(head); }
}

export { feed as GET, feed as HEAD };

function unsupported() {
  const requestID = crypto.randomUUID();
  return new Response(JSON.stringify({ type: "about:blank", title: "Method Not Allowed", status: 405, code: "method_not_allowed", request_id: requestID }), {
    status: 405, headers: { Allow: "GET, HEAD", "Content-Type": "application/problem+json", "Cache-Control": "no-store", "X-Request-ID": requestID },
  });
}
export { unsupported as POST, unsupported as PUT, unsupported as PATCH, unsupported as DELETE, unsupported as OPTIONS };
