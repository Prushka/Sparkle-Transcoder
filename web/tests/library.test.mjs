import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

// Use the existing compiler so tests also run on supported Node 20 versions,
// without a separate TypeScript loader or generated files in the source tree.
const source = readFileSync(new URL("../lib/library.ts", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { groupEpisodes, indexEpisodes, limitLibraryItems, sortLibraryItems, sortMediaItems } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

function episode(show, season, number, createdAt = "2024-01-01T00:00:00Z") {
  const id = `${show}-${season}-${number}`;
  return {
    id, show, season, episode: number, createdAt,
    kind: "episode", library: "TV", title: id, fileName: `${id}.mkv`,
    sortKey: `tv/${show}/${String(season).padStart(3, "0")}/${String(number).padStart(3, "0")}`
  };
}

const ids = (items) => items.map((item) => item.id);

test("seasons and episodes descend numerically in both library sorts, without mutating input", () => {
  const items = [episode("Show", 1, 1), episode("Show", 3, 2), episode("Show", 3, 10), episode("Show", 2, 1), episode("Show", 0, 1)];
  const original = ids(items);
  for (const sort of ["title", "recent"]) {
    const [show] = groupEpisodes(items, sort);
    assert.deepEqual(show.seasons.map((season) => season.number), [3, 2, 1, 0]);
    assert.deepEqual(show.seasons[0].items.map((item) => item.episode), [10, 2]);
  }
  assert.deepEqual(ids(sortMediaItems(items)), ["Show-3-10", "Show-3-2", "Show-2-1", "Show-1-1", "Show-0-1"]);
  assert.deepEqual(ids(items), original);
});

test("missing season and episode numbers remain visible after numbered episodes", () => {
  const missing = { ...episode("Show", 0, 0), season: undefined, episode: undefined };
  const [show] = groupEpisodes([missing, episode("Show", 1, 1)]);
  assert.deepEqual(show.seasons.map((season) => season.number), [1, 0]);
  assert.equal(show.seasons[1].items[0], missing);
  assert.equal(groupEpisodes([{ ...missing, show: undefined }])[0].name, "Unknown Show");
});

test("recent show ordering uses its newest episode, with stable title ties and invalid date fallback", () => {
  const items = [
    episode("A", 1, 1, "2024-01-01"), episode("A", 2, 1, "2024-06-01"),
    episode("B", 1, 1, "2025-01-01"), episode("C", 1, 1, "2025-01-01"),
    episode("Future", 1, 1, "9999-01-01"), episode("Invalid", 1, 1, "invalid")
  ];
  assert.deepEqual(groupEpisodes(items, "recent").map((show) => show.name), ["B", "C", "A", "Future", "Invalid"]);
  assert.deepEqual(groupEpisodes(items, "title").map((show) => show.name), ["A", "B", "C", "Future", "Invalid"]);
  assert.deepEqual(ids(sortLibraryItems(items, "recent")), ["B-1-1", "C-1-1", "A-2-1", "A-1-1", "Future-1-1", "Invalid-1-1"]);
});

test("recent pagination retains the entire show and the existing 300-item batch size", () => {
  const largeShow = Array.from({ length: 425 }, (_, i) => episode("Large", Math.floor(i / 25) + 1, i % 25 + 1, "2025-01-01"));
  const otherShow = Array.from({ length: 400 }, (_, i) => episode("Other", 1, i + 1, "2024-01-01"));
  const movie = { ...episode("Movie", 0, 0, "2025-02-01"), kind: "movie" };
  const items = sortLibraryItems([...largeShow, ...otherShow, movie], "recent");
  const first = limitLibraryItems(items, 300, "recent");
  assert.equal(first.length, 426);
  assert.equal(first[0], movie);
  assert.equal(first.filter((item) => item.show === "Large").length, 425);
  const next = limitLibraryItems(items, first.length + 300, "recent");
  assert.deepEqual(ids(next), ids(items));
  assert.equal(new Set(ids(next)).size, 826);
});

test("title pagination includes the latest season and episode before a partial-show boundary", () => {
  const items = sortLibraryItems([episode("Show", 1, 1), episode("Show", 10, 1), episode("Show", 10, 12), episode("Show", 2, 1)], "title");
  assert.deepEqual(ids(limitLibraryItems(items, 2, "title")), ["Show-10-12", "Show-10-1"]);
  assert.equal(limitLibraryItems(items, 300, "title").length, 4);
});

test("queue index keeps complete show and season selections independently of search results", () => {
  const items = [episode("A", 1, 1), episode("A", 2, 1), episode("A", 2, 2), episode("B", 1, 1)];
  const index = indexEpisodes(items);
  const visible = groupEpisodes(items.filter((item) => item.id === "A-2-1"));
  assert.equal(visible[0].seasons[0].items.length, 1);
  assert.deepEqual(ids(index.get("A").items), ["A-2-2", "A-2-1", "A-1-1"]);
  assert.deepEqual(ids(index.get("A").seasons.get(2)), ["A-2-2", "A-2-1"]);
});

test("recent sorting and grouping each read timestamps once per episode on a large library", () => {
  let dateReads = 0;
  const items = Array.from({ length: 10000 }, (_, i) => ({
    ...episode(`Show ${i % 500}`, Math.floor(i / 500) + 1, 1),
    get createdAt() { dateReads++; return "2024-01-01"; }
  }));
  sortLibraryItems(items, "recent");
  assert.equal(dateReads, items.length);
  dateReads = 0;
  assert.equal(groupEpisodes(items, "recent").length, 500);
  assert.equal(dateReads, items.length);
});
