"use client";

import { useEffect, useState } from "react";
import { useAdminConfirmation } from "./admin-confirmation";
import { AdminBadge, AdminNote } from "./admin-ui";

type Pin = { id: string; slug: string; title: string; visibility: "public" | "private"; version: number };
type PinList = { items: Pin[] };
type Request = <T>(path: string, init?: RequestInit, signature?: string) => Promise<T>;

export default function PinnedArticles({ busy, request, run, onDirty }: {
  busy: boolean; request: Request; run: (action: () => Promise<void>) => Promise<void>; onDirty: (dirty: boolean) => void;
}) {
  const confirm = useAdminConfirmation();
  const [items, setItems] = useState<Pin[] | null>(null);
  const [saved, setSaved] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [needsReload, setNeedsReload] = useState(false);
  const [message, setMessage] = useState("");
  const dirty = items?.some((item, index) => saved[index] !== item.id) ?? false;
  const locked = busy || loading || needsReload;
  useEffect(() => {
    let active = true;
    request<PinList>("/pins").then(result => {
      if (!active) return;
      setItems(result.items); setSaved(result.items.map(item => item.id));
    }).catch(() => { if (active) setMessage("Pinned articles could not be loaded. Try Reload pins."); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [request]);

  async function reload() {
    if (dirty && !await confirm("Discard this unsaved order and load the current pinned articles?", { title: "Reload pinned articles?", action: "Reload pins" })) return;
    setLoading(true);
    try {
      const result = await request<PinList>("/pins");
      setItems(result.items); setSaved(result.items.map(item => item.id));
      setNeedsReload(false); onDirty(false); setMessage("Pinned articles reloaded.");
    } catch { setMessage("Pinned articles could not be loaded. Try Reload pins."); }
    finally { setLoading(false); }
  }
  function move(index: number, direction: number) {
    if (!items || locked) return;
    const next = [...items];
    [next[index], next[index + direction]] = [next[index + direction], next[index]];
    setItems(next); onDirty(next.some((item, position) => saved[position] !== item.id));
    setMessage(`Moved “${items[index].title}” to position ${index + direction + 1}. Save order to apply.`);
  }
  async function save() {
    if (!items || !dirty || locked) return;
    await run(async () => {
      try {
        await request("/pins/order", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ items: items.map(item => ({ id: item.id, expected_version: item.version })) }) });
      } catch (error) {
        setNeedsReload(true);
        setMessage((error as { code?: string }).code === "pin_order_conflict"
          ? "Pinned articles changed in another tab. Your proposed order is still shown. Reload pins before reordering again."
          : "The save could not be confirmed. Reload pins to check the current order before making more changes.");
        return;
      }
      onDirty(false); setSaved(items.map(item => item.id));
      try {
        const result = await request<PinList>("/pins");
        setItems(result.items); setSaved(result.items.map(item => item.id));
        setMessage("Pin order saved. Home and Archive now use this order.");
      } catch { setNeedsReload(true); setMessage("Pin order saved. Reload pins to continue editing."); }
    });
  }

  return <>
    <section className="card" aria-label="Global pin order">
      <div className="card-head admin-pin-heading"><div><h2>Display order</h2><p>First in this list appears first on Home and Archive.</p></div><button className="button" type="button" disabled={busy || loading} onClick={() => void run(reload)}>Reload pins</button></div>
      {message && <p className="admin-inline-note" role="status">{message}</p>}
      {loading ? <p className="empty" role="status">Loading pinned articles…</p> : items === null ? <p className="empty">Use Reload pins to try again.</p> : items.length === 0 ? <div className="empty"><h3>No globally pinned articles yet</h3><p>Open a published article and turn on Pin globally to add it here.</p></div> : <ol className="admin-pin-list">{items.map((item, index) => <li key={item.id}>
        <span className="admin-pin-number" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
        <div className="admin-pin-copy"><h3>{item.title}</h3><p>/articles/{item.slug}</p><AdminBadge tone={item.visibility === "public" ? "blue" : ""}>{item.visibility === "public" ? "Public" : "Private · Only you"}</AdminBadge></div>
        <div className="admin-pin-actions"><button className="button" type="button" disabled={locked || index === 0} onClick={() => move(index, -1)} aria-label={`Move ${item.title} up`}>Move up ↑</button><button className="button" type="button" disabled={locked || index === items.length - 1} onClick={() => move(index, 1)} aria-label={`Move ${item.title} down`}>Move down ↓</button></div>
      </li>)}</ol>}
      <div className="table-foot"><span>{dirty ? "Unsaved order changes" : "No unsaved order changes"}</span><button className="button primary" type="button" disabled={locked || !dirty} onClick={() => void save()}>Save order</button></div>
    </section>
    <AdminNote>Date sorting applies after these pins. Private pins are visible only to you; public readers keep the same relative order of public pins. New pins are added at the end. Category pins and search have their own ordering.</AdminNote>
  </>;
}
