"use client";

import { AnimatePresence, motion } from "framer-motion";
import {
  BadgeCheck,
  Ban,
  Clapperboard,
  FileSearch,
  Film,
  FolderSync,
  Gauge,
  HardDrive,
  ListVideo,
  Loader2,
  Play,
  RefreshCcw,
  RotateCcw,
  Search,
  Settings2,
  Sparkles,
  Subtitles,
  Tv,
  Video
} from "lucide-react";
import * as React from "react";
import { api, outputUrl, posterUrl, type MediaItem, type PublicConfig, type ScanStatus, type TaskParams, type TranscodeTask } from "@/lib/api";
import { cn, formatBytes, formatDate } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

type LoadState = {
  config?: PublicConfig;
  scan?: ScanStatus;
  media: MediaItem[];
  tasks: TranscodeTask[];
};

const initialState: LoadState = {
  media: [],
  tasks: []
};

export function Dashboard() {
  const [state, setState] = React.useState<LoadState>(initialState);
  const [query, setQuery] = React.useState("");
  const [kind, setKind] = React.useState("all");
  const [library, setLibrary] = React.useState("all");
  const [selected, setSelected] = React.useState<MediaItem | null>(null);
  const [activeTab, setActiveTab] = React.useState("library");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [mediaLimit, setMediaLimit] = React.useState(150);
  const [taskLimit, setTaskLimit] = React.useState(100);

  const refresh = React.useCallback(async () => {
    try {
      const [config, mediaResponse, taskResponse, scan] = await Promise.all([api.config(), api.media(), api.tasks(), api.scanStatus()]);
      setState((current) => ({
        config,
        media: mediaResponse.items,
        tasks: mergeTaskDetails(taskResponse.tasks, current.tasks),
        scan
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to reach backend");
    }
  }, []);

  React.useEffect(() => {
    refresh();
    const interval = window.setInterval(refresh, 5000);
    return () => window.clearInterval(interval);
  }, [refresh]);

  const libraries = React.useMemo(() => Array.from(new Set(state.media.map((item) => item.library).filter(Boolean))).sort(), [state.media]);
  const filtered = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    return state.media.filter((item) => {
      if (kind !== "all" && item.kind !== kind) return false;
      if (library !== "all" && item.library !== library) return false;
      if (!q) return true;
      return `${item.title} ${item.show ?? ""} ${item.fileName} ${item.library}`.toLowerCase().includes(q);
    });
  }, [kind, library, query, state.media]);
  const visibleMedia = React.useMemo(() => filtered.slice(0, mediaLimit), [filtered, mediaLimit]);
  const visibleTasks = React.useMemo(() => state.tasks.slice(0, taskLimit), [state.tasks, taskLimit]);

  React.useEffect(() => {
    setMediaLimit(150);
  }, [kind, library, query]);

  const startScan = async (force: boolean) => {
    setBusy(true);
    try {
      await api.startScan(force);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Scan failed");
    } finally {
      setBusy(false);
    }
  };

  const createTask = async (item: MediaItem, params: TaskParams) => {
    setBusy(true);
    try {
      await api.createTask(item.id, params);
      setSelected(null);
      setActiveTab("tasks");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task creation failed");
    } finally {
      setBusy(false);
    }
  };

  const cancelTask = async (id: string) => {
    await api.cancelTask(id);
    await refresh();
  };

  const retryTask = async (id: string) => {
    await api.retryTask(id);
    await refresh();
  };

  const loadTaskDetails = async (id: string) => {
    try {
      const details = await api.task(id);
      setState((current) => ({
        ...current,
        tasks: current.tasks.map((task) => (task.id === id ? details : task))
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load task details");
    }
  };

  return (
    <TooltipProvider delayDuration={250}>
      <main className="min-h-screen">
        <div className="container py-5 md:py-7">
          <header className="mb-5 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <div className="flex size-9 items-center justify-center rounded-lg bg-primary/15 text-primary">
                  <Sparkles className="size-5" />
                </div>
                <h1 className="truncate text-2xl font-semibold tracking-normal">Sparkle Transcoder</h1>
              </div>
              <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
                <Badge variant="outline">{state.config?.mediaRoot ?? "Media root"}</Badge>
                <Badge variant="outline">{state.config?.output ?? "Output"}</Badge>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Tip content="Run the default incremental scan">
                <Button variant="secondary" onClick={() => startScan(false)} disabled={busy || state.scan?.running}>
                  {state.scan?.running ? <Loader2 className="animate-spin" /> : <FolderSync />}
                  Scan
                </Button>
              </Tip>
              <Tip content="Ignore cache and rescan configured media folders">
                <Button variant="outline" onClick={() => startScan(true)} disabled={busy || state.scan?.running}>
                  <RefreshCcw />
                  Full
                </Button>
              </Tip>
            </div>
          </header>

          {error ? <div className="mb-4 rounded-lg border border-rose-400/30 bg-rose-400/10 p-3 text-sm text-rose-100">{error}</div> : null}

          <section className="mb-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Stat icon={HardDrive} label="Media" value={state.media.length.toLocaleString()} detail={`${filtered.length.toLocaleString()} visible`} />
            <Stat icon={ListVideo} label="Tasks" value={state.tasks.length.toLocaleString()} detail={`${countByState(state.tasks, "running")} running`} />
            <Stat icon={BadgeCheck} label="Complete" value={countByState(state.tasks, "complete").toLocaleString()} detail={`${countByState(state.tasks, "failed")} failed`} />
            <Stat icon={Gauge} label="Scan" value={state.scan?.running ? "Running" : "Idle"} detail={scanDetail(state.scan)} />
          </section>

          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList>
              <TabsTrigger value="library">
                <Clapperboard />
                Library
              </TabsTrigger>
              <TabsTrigger value="tasks">
                <Settings2 />
                Tasks
              </TabsTrigger>
            </TabsList>
            <TabsContent value="library">
              <LibraryToolbar
                query={query}
                setQuery={setQuery}
                kind={kind}
                setKind={setKind}
                library={library}
                setLibrary={setLibrary}
                libraries={libraries}
              />
              <LibraryView items={visibleMedia} onTranscode={setSelected} />
              {filtered.length > visibleMedia.length ? (
                <div className="mt-5 flex justify-center">
                  <Tip content="Render the next batch of matching media">
                    <Button variant="outline" onClick={() => setMediaLimit((value) => value + 150)}>
                      Show {Math.min(150, filtered.length - visibleMedia.length).toLocaleString()} more
                    </Button>
                  </Tip>
                </div>
              ) : null}
            </TabsContent>
            <TabsContent value="tasks">
              <TaskView tasks={visibleTasks} onCancel={cancelTask} onRetry={retryTask} onDetails={loadTaskDetails} />
              {state.tasks.length > visibleTasks.length ? (
                <div className="mt-5 flex justify-center">
                  <Tip content="Render the next batch of tasks">
                    <Button variant="outline" onClick={() => setTaskLimit((value) => value + 100)}>
                      Show {Math.min(100, state.tasks.length - visibleTasks.length).toLocaleString()} more
                    </Button>
                  </Tip>
                </div>
              ) : null}
            </TabsContent>
          </Tabs>
        </div>
      </main>
      <TaskDialog item={selected} config={state.config} busy={busy} onOpenChange={(open) => !open && setSelected(null)} onCreate={createTask} />
    </TooltipProvider>
  );
}

function LibraryToolbar({
  query,
  setQuery,
  kind,
  setKind,
  library,
  setLibrary,
  libraries
}: {
  query: string;
  setQuery: (value: string) => void;
  kind: string;
  setKind: (value: string) => void;
  library: string;
  setLibrary: (value: string) => void;
  libraries: string[];
}) {
  return (
    <div className="mb-4 grid gap-2 md:grid-cols-[minmax(0,1fr)_180px_220px]">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input value={query} onChange={(event) => setQuery(event.target.value)} className="pl-9" placeholder="Search title, show, file, library" />
      </div>
      <Tip content="Filter by media type">
        <div>
          <Select value={kind} onValueChange={setKind}>
            <SelectTrigger>
              <span>{kindLabel(kind)}</span>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All types</SelectItem>
              <SelectItem value="movie">Movies</SelectItem>
              <SelectItem value="episode">Episodes</SelectItem>
              <SelectItem value="unknown">Unknown</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </Tip>
      <Tip content="Filter by configured library folder">
        <div>
          <Select value={library} onValueChange={setLibrary}>
            <SelectTrigger>
              <span>{library === "all" ? "All libraries" : library}</span>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All libraries</SelectItem>
              {libraries.map((value) => (
                <SelectItem value={value} key={value}>
                  {value}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </Tip>
    </div>
  );
}

function LibraryView({ items, onTranscode }: { items: MediaItem[]; onTranscode: (item: MediaItem) => void }) {
  const movies = items.filter((item) => item.kind === "movie");
  const shows = groupEpisodes(items.filter((item) => item.kind === "episode"));
  const unknown = items.filter((item) => item.kind === "unknown");

  return (
    <div className="space-y-6">
      {movies.length ? (
        <MediaSection title="Movies" icon={Film}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
            {movies.map((item) => (
              <MediaCard item={item} key={item.id} onTranscode={onTranscode} />
            ))}
          </div>
        </MediaSection>
      ) : null}
      {shows.map((show) => (
        <MediaSection title={show.name} icon={Tv} key={show.name}>
          <div className="space-y-4">
            {show.seasons.map((season) => (
              <div key={`${show.name}-${season.number}`} className="space-y-2">
                <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
                  <span>Season {season.number}</span>
                  <Badge variant="outline">{season.items.length}</Badge>
                </div>
                <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                  {season.items.map((item) => (
                    <EpisodeRow item={item} key={item.id} onTranscode={onTranscode} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </MediaSection>
      ))}
      {unknown.length ? (
        <MediaSection title="Unmatched" icon={Video}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {unknown.map((item) => (
              <EpisodeRow item={item} key={item.id} onTranscode={onTranscode} />
            ))}
          </div>
        </MediaSection>
      ) : null}
      {!items.length ? <EmptyState label="No media found" /> : null}
    </div>
  );
}

function MediaSection({ title, icon: Icon, children }: { title: string; icon: React.ElementType; children: React.ReactNode }) {
  return (
    <section>
      <div className="mb-3 flex items-center gap-2">
        <Icon className="size-4 text-primary" />
        <h2 className="text-lg font-semibold tracking-normal">{title}</h2>
      </div>
      {children}
    </section>
  );
}

function MediaCard({ item, onTranscode }: { item: MediaItem; onTranscode: (item: MediaItem) => void }) {
  return (
    <motion.div layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Card className="overflow-hidden">
        <div className="grid grid-cols-[92px_minmax(0,1fr)]">
          <Poster item={item} className="h-full min-h-36 rounded-none border-r" />
          <div className="min-w-0 p-3">
            <div className="mb-2 flex items-start justify-between gap-2">
              <div className="min-w-0">
                <h3 className="truncate text-sm font-semibold">{item.title}</h3>
                <p className="mt-1 truncate text-xs text-muted-foreground">{item.fileName}</p>
              </div>
              <MediaBadges item={item} />
            </div>
            <MetaLine item={item} />
            <div className="mt-3 flex items-center justify-between gap-2">
              <span className="text-xs text-muted-foreground">{formatBytes(item.size)}</span>
              <Tip content="Create a transcoding task for this media file">
                <Button size="sm" onClick={() => onTranscode(item)}>
                  <Play />
                  Queue
                </Button>
              </Tip>
            </div>
          </div>
        </div>
      </Card>
    </motion.div>
  );
}

function EpisodeRow({ item, onTranscode }: { item: MediaItem; onTranscode: (item: MediaItem) => void }) {
  return (
    <motion.div layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Card>
        <CardContent className="flex gap-3 p-3">
          <Poster item={item} className="size-20 shrink-0" />
          <div className="min-w-0 flex-1">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0">
                <h3 className="truncate text-sm font-semibold">
                  {item.kind === "episode" ? `E${String(item.episode ?? 0).padStart(2, "0")} · ${item.title}` : item.title}
                </h3>
                <p className="mt-1 truncate text-xs text-muted-foreground">{item.library}</p>
              </div>
              <MediaBadges item={item} />
            </div>
            <MetaLine item={item} />
            <div className="mt-2 flex items-center justify-between gap-2">
              <span className="truncate text-xs text-muted-foreground">{item.ext.toUpperCase()} · {formatBytes(item.size)}</span>
              <Tip content="Create a transcoding task for this episode">
                <Button size="sm" variant="secondary" onClick={() => onTranscode(item)}>
                  <Play />
                  Queue
                </Button>
              </Tip>
            </div>
          </div>
        </CardContent>
      </Card>
    </motion.div>
  );
}

function Poster({ item, className }: { item: MediaItem; className?: string }) {
  const src = posterUrl(item);
  return (
    <div className={cn("flex aspect-[2/3] items-center justify-center overflow-hidden rounded-md border bg-muted", className)}>
      {src ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={src} alt="" className="h-full w-full object-cover" loading="lazy" />
      ) : (
        <Film className="size-7 text-muted-foreground" />
      )}
    </div>
  );
}

function MediaBadges({ item }: { item: MediaItem }) {
  return (
    <div className="flex shrink-0 gap-1">
      {item.subtitles?.length ? (
        <Tip content={`${item.subtitles.length} subtitle sidecar${item.subtitles.length === 1 ? "" : "s"}`}>
          <Badge variant="secondary">
            <Subtitles className="mr-1 size-3" />
            {item.subtitles.length}
          </Badge>
        </Tip>
      ) : null}
      {item.poster ? <Badge variant="default">Poster</Badge> : null}
    </div>
  );
}

function MetaLine({ item }: { item: MediaItem }) {
  const parts = [item.show, item.season ? `S${String(item.season).padStart(2, "0")}` : "", formatDate(item.modTime)].filter(Boolean);
  return <p className="mt-2 truncate text-xs text-muted-foreground">{parts.join(" · ")}</p>;
}

function TaskView({
  tasks,
  onCancel,
  onRetry,
  onDetails
}: {
  tasks: TranscodeTask[];
  onCancel: (id: string) => void;
  onRetry: (id: string) => void;
  onDetails: (id: string) => void;
}) {
  return (
    <div className="space-y-3">
      <AnimatePresence initial={false}>
        {tasks.map((task) => (
          <motion.div key={task.id} layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }}>
            <Card>
              <CardHeader className="pb-3">
                <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                  <div className="min-w-0">
                    <div className="mb-2 flex flex-wrap items-center gap-2">
                      <CardTitle className="truncate">{task.input || task.inputRelPath || task.id}</CardTitle>
                      <StateBadge state={task.state} />
                      {task.legacy ? <Badge variant="outline">Legacy</Badge> : null}
                    </div>
                    <p className="truncate text-xs text-muted-foreground">{task.outputDir}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Tip content="Load full task details and output file links">
                      <Button variant="outline" size="sm" onClick={() => onDetails(task.id)}>
                        <FileSearch />
                        Details
                      </Button>
                    </Tip>
                    {task.state === "running" || task.state === "queued" ? (
                      <Tip content="Cancel this queued or running task">
                        <Button variant="destructive" size="sm" onClick={() => onCancel(task.id)}>
                          <Ban />
                          Cancel
                        </Button>
                      </Tip>
                    ) : null}
                    {task.state === "failed" || task.state === "canceled" ? (
                      <Tip content="Queue this task again with the same parameters">
                        <Button variant="secondary" size="sm" onClick={() => onRetry(task.id)}>
                          <RotateCcw />
                          Retry
                        </Button>
                      </Tip>
                    ) : null}
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <Progress value={stateProgress(task.state)} />
                <div className="mt-3 grid gap-2 text-xs text-muted-foreground sm:grid-cols-2 lg:grid-cols-4">
                  <span>Updated {formatDate(task.updatedAt)}</span>
                  <span>{task.encodedCodecs?.length ? task.encodedCodecs.join(", ") : "No encoded codec"}</span>
                  <span>{task.duration ? `${Math.round(task.duration / 60)} min` : "Duration pending"}</span>
                  <span>{task.files ? `${Object.keys(task.files).length} files` : task.state === "complete" ? "Details needed" : "Files pending"}</span>
                </div>
                {task.error ? <div className="mt-3 rounded-md border border-rose-400/30 bg-rose-400/10 p-2 text-xs text-rose-100">{task.error}</div> : null}
                {task.files && Object.keys(task.files).length ? (
                  <div className="mt-3 flex flex-wrap gap-2">
                    {Object.entries(task.files)
                      .slice(0, 8)
                      .map(([name, size]) => (
                        <a
                          className="rounded-md border border-border px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                          href={outputUrl(task, name)}
                          target="_blank"
                          rel="noreferrer"
                          key={name}
                        >
                          {name} · {formatBytes(size)}
                        </a>
                      ))}
                  </div>
                ) : null}
              </CardContent>
            </Card>
          </motion.div>
        ))}
      </AnimatePresence>
      {!tasks.length ? <EmptyState label="No tasks found" /> : null}
    </div>
  );
}

function TaskDialog({
  item,
  config,
  busy,
  onOpenChange,
  onCreate
}: {
  item: MediaItem | null;
  config?: PublicConfig;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (item: MediaItem, params: TaskParams) => void;
}) {
  const [fast, setFast] = React.useState(false);
  const [enableEncode, setEnableEncode] = React.useState(true);
  const [enableSprites, setEnableSprites] = React.useState(true);
  const [extractStreams, setExtractStreams] = React.useState(true);
  const [encoders, setEncoders] = React.useState<string[]>(["hevc"]);
  const [quality, setQuality] = React.useState(18);
  const [audioKbps, setAudioKbps] = React.useState(144);

  React.useEffect(() => {
    if (!config) return;
    setEnableEncode(config.enableEncode);
    setEnableSprites(config.enableSprite);
    setEncoders(config.encoders?.length ? config.encoders : ["hevc"]);
    setQuality(Number(config.quality || 18));
    setAudioKbps(config.audioKbps || 144);
  }, [config, item?.id]);

  const allEncoders = ["hevc", "av1", "h264-10bit", "h264-8bit"];
  const submit = () => {
    if (!item) return;
    onCreate(item, {
      fast,
      enableEncode,
      enableSprites,
      extractStreams,
      encoders,
      quality: String(quality),
      audioKbps,
      videoExt: config?.videoExt ?? "mp4"
    });
  };

  return (
    <Dialog open={!!item} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{item?.title ?? "Transcode"}</DialogTitle>
          <DialogDescription>{item?.fileName}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <ToggleLine label="Fast path" checked={fast} onCheckedChange={setFast} tip="Copy video and repackage audio through ffmpeg">
              <Gauge className="size-4 text-primary" />
            </ToggleLine>
            <ToggleLine label="Encode video" checked={enableEncode} onCheckedChange={setEnableEncode} tip="Run HandBrake or ffmpeg output generation">
              <Video className="size-4 text-primary" />
            </ToggleLine>
            <ToggleLine label="Extract streams" checked={extractStreams} onCheckedChange={setExtractStreams} tip="Extract subtitle, attachment, and audio streams">
              <Subtitles className="size-4 text-primary" />
            </ToggleLine>
            <ToggleLine label="Sprites" checked={enableSprites} onCheckedChange={setEnableSprites} tip="Generate storyboard thumbnail sheets">
              <Clapperboard className="size-4 text-primary" />
            </ToggleLine>
          </div>

          <div>
            <div className="mb-2 text-sm font-medium">Encoders</div>
            <div className="flex flex-wrap gap-2">
              {allEncoders.map((encoder) => {
                const active = encoders.includes(encoder);
                return (
                  <Tip content={`Toggle ${encoder} output`} key={encoder}>
                    <Button
                      type="button"
                      variant={active ? "default" : "outline"}
                      size="sm"
                      onClick={() => setEncoders(active ? encoders.filter((value) => value !== encoder) : [...encoders, encoder])}
                      disabled={fast}
                    >
                      {encoder}
                    </Button>
                  </Tip>
                );
              })}
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <LabeledSlider label={`Quality ${quality}`} value={quality} min={12} max={30} step={1} onChange={setQuality} tip="HandBrake constant quality value" />
            <LabeledSlider label={`Audio ${audioKbps} kbps`} value={audioKbps} min={96} max={320} step={8} onChange={setAudioKbps} tip="Opus audio bitrate per output" />
          </div>
        </div>
        <Separator />
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || (!fast && !encoders.length)}>
            {busy ? <Loader2 className="animate-spin" /> : <Play />}
            Queue task
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ToggleLine({
  label,
  checked,
  onCheckedChange,
  tip,
  children
}: {
  label: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
  tip: string;
  children: React.ReactNode;
}) {
  return (
    <Tip content={tip}>
      <label className="flex cursor-pointer items-center justify-between gap-3 rounded-lg border p-3">
        <span className="flex items-center gap-2 text-sm font-medium">
          {children}
          {label}
        </span>
        <Switch checked={checked} onCheckedChange={onCheckedChange} />
      </label>
    </Tip>
  );
}

function LabeledSlider({
  label,
  value,
  min,
  max,
  step,
  onChange,
  tip
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  onChange: (value: number) => void;
  tip: string;
}) {
  return (
    <Tip content={tip}>
      <div className="rounded-lg border p-3">
        <div className="mb-3 text-sm font-medium">{label}</div>
        <Slider value={[value]} min={min} max={max} step={step} onValueChange={(next) => onChange(next[0])} />
      </div>
    </Tip>
  );
}

function Stat({ icon: Icon, label, value, detail }: { icon: React.ElementType; label: string; value: string; detail: string }) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3 p-4">
        <div className="flex size-10 items-center justify-center rounded-lg bg-muted text-primary">
          <Icon className="size-5" />
        </div>
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="truncate text-xl font-semibold">{value}</div>
          <div className="truncate text-xs text-muted-foreground">{detail}</div>
        </div>
      </CardContent>
    </Card>
  );
}

function StateBadge({ state }: { state: string }) {
  const variant = state === "complete" ? "default" : state === "failed" || state === "canceled" ? "danger" : state === "running" ? "warning" : "outline";
  return <Badge variant={variant as "default"}>{state}</Badge>;
}

function EmptyState({ label }: { label: string }) {
  return (
    <div className="flex min-h-40 items-center justify-center rounded-lg border border-dashed text-sm text-muted-foreground">
      {label}
    </div>
  );
}

function Tip({ content, children }: { content: string; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent>{content}</TooltipContent>
    </Tooltip>
  );
}

function countByState(tasks: TranscodeTask[], state: string) {
  return tasks.filter((task) => task.state === state).length;
}

function kindLabel(value: string) {
  switch (value) {
    case "movie":
      return "Movies";
    case "episode":
      return "Episodes";
    case "unknown":
      return "Unknown";
    default:
      return "All types";
  }
}

function stateProgress(state: string) {
  switch (state) {
    case "queued":
      return 10;
    case "incomplete":
      return 35;
    case "running":
      return 55;
    case "streams_extracted":
      return 72;
    case "complete":
      return 100;
    case "failed":
    case "canceled":
      return 100;
    default:
      return 15;
  }
}

function scanDetail(scan?: ScanStatus) {
  if (!scan) return "Never";
  if (scan.running) {
    const visited = `${(scan.dirsScanned ?? 0).toLocaleString()} dirs · ${(scan.filesScanned ?? 0).toLocaleString()} files`;
    return scan.currentPath ? `${visited} · ${scan.currentPath}` : visited;
  }
  return formatDate(scan.lastFinishedAt);
}

function mergeTaskDetails(next: TranscodeTask[], current: TranscodeTask[]) {
  const detailed = new Map(current.filter((task) => task.files || task.streams).map((task) => [task.id, task]));
  return next.map((task) => {
    const existing = detailed.get(task.id);
    if (!existing) return task;
    return {
      ...existing,
      ...task,
      files: task.files ?? existing.files,
      streams: task.streams ?? existing.streams
    };
  });
}

function groupEpisodes(items: MediaItem[]) {
  const shows = new Map<string, Map<number, MediaItem[]>>();
  for (const item of items) {
    const show = item.show || "Unknown Show";
    const season = item.season || 0;
    if (!shows.has(show)) shows.set(show, new Map());
    const seasons = shows.get(show)!;
    if (!seasons.has(season)) seasons.set(season, []);
    seasons.get(season)!.push(item);
  }
  return Array.from(shows.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, seasons]) => ({
      name,
      seasons: Array.from(seasons.entries())
        .sort(([a], [b]) => a - b)
        .map(([number, seasonItems]) => ({
          number,
          items: seasonItems.sort((a, b) => (a.episode || 0) - (b.episode || 0) || a.fileName.localeCompare(b.fileName))
        }))
    }));
}
