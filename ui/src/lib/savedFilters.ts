import type { EventFilters } from "@/lib/queries";

// Saved timeline filters, persisted client-side (v1 has no server-side user
// state). Keyed by name; saving an existing name overwrites it.
const KEY = "wtc.savedFilters";

export interface SavedFilter {
  name: string;
  filters: EventFilters;
}

export function loadSavedFilters(): SavedFilter[] {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as SavedFilter[]) : [];
  } catch {
    return [];
  }
}

function persist(list: SavedFilter[]): SavedFilter[] {
  localStorage.setItem(KEY, JSON.stringify(list));
  return list;
}

export function saveFilter(name: string, filters: EventFilters): SavedFilter[] {
  const list = loadSavedFilters().filter((f) => f.name !== name);
  return persist([...list, { name, filters }]);
}

export function deleteFilter(name: string): SavedFilter[] {
  return persist(loadSavedFilters().filter((f) => f.name !== name));
}
