import type { MediaItem } from "./api";

export type LibrarySort = "title" | "recent";
export type EpisodeSeason = { number: number; items: MediaItem[] };
export type EpisodeShow = { name: string; seasons: EpisodeSeason[] };
export type ShowSummary = { seasonCount: number; episodeCount: number; updatedAt?: string };

const FALLBACK_MEDIA_CREATION_TIME = Date.UTC(2019, 0, 1);

export function episodeShowName(item: MediaItem) {
  return item.show || "Unknown Show";
}

function compareMediaTitle(a: MediaItem, b: MediaItem) {
  return a.sortKey.localeCompare(b.sortKey) || a.fileName.localeCompare(b.fileName);
}

export function sortMediaItems(items: MediaItem[]) {
  return [...items].sort(
    (a, b) => (b.season || 0) - (a.season || 0) || (b.episode || 0) - (a.episode || 0) || compareMediaTitle(a, b)
  );
}

function mediaCreationTime(item: MediaItem, now: number) {
  const value = Date.parse(item.createdAt);
  return Number.isFinite(value) && value <= now ? value : FALLBACK_MEDIA_CREATION_TIME;
}

export function sortLibraryItems(items: MediaItem[], sort: LibrarySort) {
  if (sort === "title") {
    // Keep the existing library/title order, but select newest episodes first
    // even when the page boundary falls partway through a show.
    const byTitle = [...items].sort(compareMediaTitle);
    const episodes = indexEpisodes(byTitle);
    const positions = new Map<string, number>();
    return byTitle.map((item) => {
      if (item.kind !== "episode") return item;
      const name = episodeShowName(item);
      const position = positions.get(name) ?? 0;
      positions.set(name, position + 1);
      return episodes.get(name)!.items[position];
    });
  }

  const now = Date.now();
  // Parse each timestamp once, rather than on every sort comparison.
  const times = new Map(items.map((item) => [item, mediaCreationTime(item, now)]));
  return [...items].sort((a, b) => times.get(b)! - times.get(a)! || compareMediaTitle(a, b));
}

export function indexEpisodes(items: MediaItem[]) {
  const shows = new Map<string, { items: MediaItem[]; seasons: Map<number, MediaItem[]>; summary: ShowSummary }>();
  for (const item of items) {
    if (item.kind !== "episode") continue;
    const name = episodeShowName(item);
    let show = shows.get(name);
    if (!show) {
      show = { items: [], seasons: new Map(), summary: { seasonCount: 0, episodeCount: 0 } };
      shows.set(name, show);
    }
    show.items.push(item);
  }
  for (const show of shows.values()) {
    show.items = sortMediaItems(show.items);
    let latestUpdate = -Infinity;
    for (const item of show.items) {
      const number = item.season || 0;
      const season = show.seasons.get(number);
      if (season) season.push(item);
      else show.seasons.set(number, [item]);
      const modified = Date.parse(item.modTime);
      if (Number.isFinite(modified) && modified > latestUpdate) {
        latestUpdate = modified;
        show.summary.updatedAt = item.modTime;
      }
    }
    show.summary.seasonCount = show.seasons.size;
    show.summary.episodeCount = show.items.length;
  }
  return shows;
}

export function limitLibraryItems(items: MediaItem[], limit: number, sort: LibrarySort) {
  if (sort !== "recent") return items.slice(0, limit);

  const episodesByShow = new Map<string, MediaItem[]>();
  for (const item of items) {
    if (item.kind !== "episode") continue;
    const name = episodeShowName(item);
    const episodes = episodesByShow.get(name);
    if (episodes) episodes.push(item);
    else episodesByShow.set(name, [item]);
  }

  const visible = new Set<MediaItem>();
  const visibleShows = new Set<string>();
  for (const item of items) {
    if (visible.size >= limit) break;
    if (item.kind !== "episode") {
      visible.add(item);
      continue;
    }
    const name = episodeShowName(item);
    if (visibleShows.has(name)) continue;
    visibleShows.add(name);
    for (const episode of episodesByShow.get(name)!) visible.add(episode);
  }
  // Input is already sorted; filtering preserves that order without re-sorting.
  return items.filter((item) => visible.has(item));
}

export function groupEpisodes(items: MediaItem[], sort: LibrarySort = "title"): EpisodeShow[] {
  const now = Date.now();
  const shows = Array.from(indexEpisodes(items), ([name, show]) => ({
    name,
    newest: sort === "recent"
      ? show.items.reduce((newest, item) => Math.max(newest, mediaCreationTime(item, now)), FALLBACK_MEDIA_CREATION_TIME)
      : 0,
    seasons: Array.from(show.seasons, ([number, seasonItems]) => ({ number, items: seasonItems }))
  }));
  // Each show's newest timestamp is calculated once, outside the comparator.
  shows.sort((a, b) => (sort === "recent" ? b.newest - a.newest : 0) || a.name.localeCompare(b.name));
  return shows;
}
