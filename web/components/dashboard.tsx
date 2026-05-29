"use client";

import { AnimatePresence, motion } from "framer-motion";
import {
  BadgeCheck,
  Ban,
  CheckCircle2,
  CircleAlert,
  Clapperboard,
  Film,
  Filter,
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
  Trash2,
  Tv,
  Video,
  Wrench,
  X
} from "lucide-react";
import * as React from "react";
import {
  api,
  outputUrl,
  posterUrl,
  type MediaItem,
  type PublicConfig,
  type ScanStatus,
  type TaskParams,
  type TaskStatus,
  type ToolReadiness,
  type TranscodeTask
} from "@/lib/api";
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
  taskStatus?: TaskStatus;
  tools: ToolReadiness[];
  media: MediaItem[];
  tasks: TranscodeTask[];
};

type MediaTaskIndex = Map<string, TranscodeTask>;

const initialState: LoadState = {
  tools: [],
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
  const [taskCompletion, setTaskCompletion] = React.useState("all");
  const [codecFilters, setCodecFilters] = React.useState<string[]>([]);
  const [subtitleFilters, setSubtitleFilters] = React.useState<string[]>([]);
  const [deleteFilteredOpen, setDeleteFilteredOpen] = React.useState(false);
  const [taskRefreshing, setTaskRefreshing] = React.useState(false);
  const taskRefreshInFlight = React.useRef(false);

  const loadScanStatus = React.useCallback(async () => {
    try {
      const scan = await api.scanStatus();
      setState((current) => ({ ...current, scan }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load scan status");
    }
  }, []);

  const loadInitial = React.useCallback(async () => {
    try {
      const [config, scan, toolsResponse] = await Promise.all([
        api.config(),
        api.scanStatus(),
        api.tools()
      ]);
      setState((current) => ({
        ...current,
        config,
        tools: toolsResponse.tools,
        scan
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to reach backend");
    }
  }, []);

  React.useEffect(() => {
    loadInitial();
    const interval = window.setInterval(loadScanStatus, 5000);
    return () => window.clearInterval(interval);
  }, [loadInitial, loadScanStatus]);

  const loadMedia = React.useCallback(async () => {
    try {
      const mediaResponse = await api.media();
      setState((current) => ({
        ...current,
        media: mediaResponse.items,
        scan: mediaResponse.status
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load library");
    }
  }, []);

  const loadTasks = React.useCallback(async () => {
    try {
      const taskResponse = await api.tasks();
      setState((current) => ({
        ...current,
        tasks: mergeTaskDetails(taskResponse.tasks, current.tasks),
        taskStatus: taskResponse.status
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load tasks");
    }
  }, []);

  const refreshTasks = React.useCallback(async () => {
    if (taskRefreshInFlight.current) return;
    taskRefreshInFlight.current = true;
    setTaskRefreshing(true);
    try {
      const taskResponse = await api.refreshTasks();
      setState((current) => ({
        ...current,
        tasks: mergeTaskDetails(taskResponse.tasks, current.tasks),
        taskStatus: taskResponse.status
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task refresh failed");
    } finally {
      taskRefreshInFlight.current = false;
      setTaskRefreshing(false);
    }
  }, []);

  React.useEffect(() => {
    if (activeTab === "library") {
      void loadMedia();
    }
  }, [activeTab, loadMedia]);

  React.useEffect(() => {
    if (activeTab === "tasks") {
      void refreshTasks();
    }
  }, [activeTab, refreshTasks]);

  React.useEffect(() => {
    if (activeTab !== "tasks") return;
    const interval = window.setInterval(loadTasks, 5000);
    return () => window.clearInterval(interval);
  }, [activeTab, loadTasks]);

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
  const mediaTaskIndex = React.useMemo(() => buildMediaTaskIndex(state.tasks), [state.tasks]);
  const taskCodecOptions = React.useMemo(() => taskOptions(state.tasks, taskCodecs), [state.tasks]);
  const taskSubtitleOptions = React.useMemo(() => taskOptions(state.tasks, taskSubtitleLanguages), [state.tasks]);
  const filteredTasks = React.useMemo(
    () => filterTasks(state.tasks, taskCompletion, codecFilters, subtitleFilters),
    [codecFilters, state.tasks, subtitleFilters, taskCompletion]
  );
  const visibleTasks = React.useMemo(() => filteredTasks.slice(0, taskLimit), [filteredTasks, taskLimit]);
  const deletableFilteredTasks = React.useMemo(() => filteredTasks.filter(canDeleteTask), [filteredTasks]);
  const tasksRefreshing = taskRefreshing || Boolean(state.taskStatus?.refreshing);

  React.useEffect(() => {
    setMediaLimit(150);
  }, [kind, library, query]);

  React.useEffect(() => {
    setTaskLimit(100);
  }, [codecFilters, subtitleFilters, taskCompletion]);

  const startScan = async (force: boolean) => {
    setBusy(true);
    try {
      const scan = await api.startScan(force);
      setState((current) => ({ ...current, scan }));
      if (activeTab === "library") {
        await loadMedia();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Scan failed");
    } finally {
      setBusy(false);
    }
  };

  const createTask = async (item: MediaItem, params: TaskParams) => {
    setBusy(true);
    try {
      const task = await api.createTask(item.id, params);
      setState((current) => ({
        ...current,
        tasks: upsertTask(current.tasks, task)
      }));
      setSelected(null);
      setActiveTab("tasks");
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task creation failed");
    } finally {
      setBusy(false);
    }
  };

  const cancelTask = async (id: string) => {
    try {
      const task = await api.cancelTask(id);
      setState((current) => ({
        ...current,
        tasks: upsertTask(current.tasks, task)
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task cancellation failed");
    }
  };

  const retryTask = async (id: string) => {
    try {
      const task = await api.retryTask(id);
      setState((current) => ({
        ...current,
        tasks: upsertTask(current.tasks, task)
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task retry failed");
    }
  };

  const deleteTask = async (id: string) => {
    setBusy(true);
    try {
      await api.deleteTask(id);
      setState((current) => ({
        ...current,
        tasks: current.tasks.filter((task) => task.id !== id)
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task deletion failed");
    } finally {
      setBusy(false);
    }
  };

  const deleteFilteredTasks = async () => {
    const ids = deletableFilteredTasks.map((task) => task.id);
    if (!ids.length) return;
    setBusy(true);
    try {
      const result = await api.deleteTasks(ids);
      const failedIds = new Set(result.failures.map((failure) => failure.id));
      setState((current) => ({
        ...current,
        tasks: current.tasks.filter((task) => !ids.includes(task.id) || failedIds.has(task.id))
      }));
      setDeleteFilteredOpen(false);
      setError(result.failures.length ? `${result.deleted} tasks deleted, ${result.failures.length} could not be deleted.` : "");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Filtered delete failed");
    } finally {
      setBusy(false);
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

          <ToolReadinessSection tools={state.tools} />

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
              <LibraryView items={visibleMedia} taskIndex={mediaTaskIndex} onTranscode={setSelected} />
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
              <TaskToolbar
                completion={taskCompletion}
                onCompletionChange={setTaskCompletion}
                codecOptions={taskCodecOptions}
                codecFilters={codecFilters}
                onCodecFiltersChange={setCodecFilters}
                subtitleOptions={taskSubtitleOptions}
                subtitleFilters={subtitleFilters}
                onSubtitleFiltersChange={setSubtitleFilters}
                filteredCount={filteredTasks.length}
                totalCount={state.tasks.length}
                deletableCount={deletableFilteredTasks.length}
                onRefresh={refreshTasks}
                refreshing={tasksRefreshing}
                refreshedAt={state.taskStatus?.refreshedAt}
                onDeleteFiltered={() => setDeleteFilteredOpen(true)}
                disabled={busy || tasksRefreshing}
              />
              <TaskView tasks={visibleTasks} busy={busy} onCancel={cancelTask} onRetry={retryTask} onDelete={deleteTask} />
              {filteredTasks.length > visibleTasks.length ? (
                <div className="mt-5 flex justify-center">
                  <Tip content="Render the next batch of tasks">
                    <Button variant="outline" onClick={() => setTaskLimit((value) => value + 100)}>
                      Show {Math.min(100, filteredTasks.length - visibleTasks.length).toLocaleString()} more
                    </Button>
                  </Tip>
                </div>
              ) : null}
            </TabsContent>
          </Tabs>
        </div>
      </main>
      <TaskDialog item={selected} config={state.config} busy={busy} onOpenChange={(open) => !open && setSelected(null)} onCreate={createTask} />
      <DeleteFilteredDialog
        open={deleteFilteredOpen}
        totalCount={filteredTasks.length}
        deletableCount={deletableFilteredTasks.length}
        skippedCount={filteredTasks.length - deletableFilteredTasks.length}
        busy={busy}
        onOpenChange={setDeleteFilteredOpen}
        onConfirm={deleteFilteredTasks}
      />
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

function ToolReadinessSection({ tools }: { tools: ToolReadiness[] }) {
  const readyCount = tools.filter((tool) => tool.ready).length;
  return (
    <section className="mb-5">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Wrench className="size-4 text-primary" />
          <h2 className="text-sm font-semibold tracking-normal">Backend tools</h2>
          <Badge variant={readyCount === tools.length && tools.length ? "default" : "warning"}>
            {readyCount}/{tools.length || 3} ready
          </Badge>
        </div>
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        {(tools.length ? tools : ["ffmpeg", "ffprobe", "handbrake"]).map((tool) => {
          const pending = typeof tool === "string";
          const item = pending
            ? ({ id: tool, name: tool, command: tool, ready: false, required: true } satisfies ToolReadiness)
            : tool;
          return (
            <div key={item.id} className="min-w-0 rounded-lg border bg-card/60 p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    {item.ready ? <CheckCircle2 className="size-4 shrink-0 text-primary" /> : <CircleAlert className="size-4 shrink-0 text-amber-300" />}
                    <div className="truncate text-sm font-medium">{item.name}</div>
                  </div>
                  <div className="mt-1 truncate text-xs text-muted-foreground">{item.command}</div>
                </div>
                <Badge variant={item.ready ? "default" : "outline"}>{item.ready ? "Ready" : pending ? "Checking" : "Missing"}</Badge>
              </div>
              <div className="mt-3 min-h-8 text-xs text-muted-foreground">
                {item.ready ? (
                  <div className="truncate" title={item.version || item.path}>
                    {item.version || item.path}
                  </div>
                ) : (
                  <div className="line-clamp-2" title={item.error}>
                    {pending ? "Waiting for backend status" : item.error || "Not available on backend PATH"}
                  </div>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}

function TaskToolbar({
  completion,
  onCompletionChange,
  codecOptions,
  codecFilters,
  onCodecFiltersChange,
  subtitleOptions,
  subtitleFilters,
  onSubtitleFiltersChange,
  filteredCount,
  totalCount,
  deletableCount,
  onRefresh,
  refreshing,
  refreshedAt,
  onDeleteFiltered,
  disabled
}: {
  completion: string;
  onCompletionChange: (value: string) => void;
  codecOptions: string[];
  codecFilters: string[];
  onCodecFiltersChange: (value: string[]) => void;
  subtitleOptions: string[];
  subtitleFilters: string[];
  onSubtitleFiltersChange: (value: string[]) => void;
  filteredCount: number;
  totalCount: number;
  deletableCount: number;
  onRefresh: () => void;
  refreshing: boolean;
  refreshedAt?: string;
  onDeleteFiltered: () => void;
  disabled: boolean;
}) {
  const hasFilters = completion !== "all" || codecFilters.length > 0 || subtitleFilters.length > 0;
  return (
    <div className="mb-4 rounded-lg border bg-card/60 p-3">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
        <div className="min-w-0 flex-1">
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Filter className="size-4 text-primary" />
            <span className="text-sm font-medium">Task filters</span>
            <Badge variant="outline">
              {filteredCount.toLocaleString()} of {totalCount.toLocaleString()}
            </Badge>
            {refreshedAt ? <Badge variant="outline">Updated {formatDate(refreshedAt)}</Badge> : null}
            {hasFilters ? (
              <Tip content="Clear task filters">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2"
                  onClick={() => {
                    onCompletionChange("all");
                    onCodecFiltersChange([]);
                    onSubtitleFiltersChange([]);
                  }}
                >
                  <X />
                  Clear
                </Button>
              </Tip>
            ) : null}
          </div>
          <div className="grid gap-3 lg:grid-cols-[minmax(0,280px)_minmax(0,1fr)_minmax(0,1fr)]">
            <div>
              <div className="mb-1 text-xs font-medium text-muted-foreground">Completion</div>
              <div className="flex flex-wrap gap-2">
                {[
                  ["all", "All"],
                  ["incomplete", "Incomplete"],
                  ["complete", "Completed"]
                ].map(([value, label]) => (
                  <Button
                    key={value}
                    type="button"
                    variant={completion === value ? "default" : "outline"}
                    size="sm"
                    className="h-7"
                    onClick={() => onCompletionChange(value)}
                  >
                    {label}
                  </Button>
                ))}
              </div>
            </div>
            <FilterGroup title="Encoded codecs" options={codecOptions} selected={codecFilters} onChange={onCodecFiltersChange} emptyLabel="No encoded codecs yet" />
            <FilterGroup
              title="Subtitle languages"
              options={subtitleOptions}
              selected={subtitleFilters}
              onChange={onSubtitleFiltersChange}
              emptyLabel="No subtitle languages yet"
            />
          </div>
        </div>
        <div className="flex w-full shrink-0 flex-col gap-2 sm:flex-row xl:w-auto">
          <Tip content="Scan the output folder and reload task metadata">
            <Button type="button" variant="secondary" onClick={onRefresh} disabled={disabled || refreshing} className="w-full sm:w-auto">
              {refreshing ? <Loader2 className="animate-spin" /> : <RefreshCcw />}
              Refresh
            </Button>
          </Tip>
          <Tip content="Delete every deletable task matching the current filters">
            <Button variant="destructive" onClick={onDeleteFiltered} disabled={disabled || deletableCount === 0} className="w-full sm:w-auto">
              <Trash2 />
              Delete filtered
            </Button>
          </Tip>
        </div>
      </div>
    </div>
  );
}

function FilterGroup({
  title,
  options,
  selected,
  onChange,
  emptyLabel
}: {
  title: string;
  options: string[];
  selected: string[];
  onChange: (value: string[]) => void;
  emptyLabel: string;
}) {
  return (
    <div className="min-w-0">
      <div className="mb-1 text-xs font-medium text-muted-foreground">{title}</div>
      <div className="flex min-h-7 flex-wrap gap-2">
        {options.length ? (
          options.map((option) => {
            const active = selected.includes(option);
            return (
              <Button
                key={option}
                type="button"
                variant={active ? "default" : "outline"}
                size="sm"
                className="h-7 max-w-full px-2"
                onClick={() => onChange(active ? selected.filter((value) => value !== option) : [...selected, option])}
              >
                <span className="truncate">{option}</span>
              </Button>
            );
          })
        ) : (
          <span className="text-xs text-muted-foreground">{emptyLabel}</span>
        )}
      </div>
    </div>
  );
}

function LibraryView({ items, taskIndex, onTranscode }: { items: MediaItem[]; taskIndex: MediaTaskIndex; onTranscode: (item: MediaItem) => void }) {
  const movies = items.filter((item) => item.kind === "movie");
  const shows = groupEpisodes(items.filter((item) => item.kind === "episode"));
  const unknown = items.filter((item) => item.kind === "unknown");

  return (
    <div className="space-y-6">
      {movies.length ? (
        <MediaSection title="Movies" icon={Film}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
            {movies.map((item) => (
              <MediaCard item={item} task={taskForMedia(item, taskIndex)} key={item.id} onTranscode={onTranscode} />
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
                    <EpisodeRow item={item} task={taskForMedia(item, taskIndex)} key={item.id} onTranscode={onTranscode} />
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
              <EpisodeRow item={item} task={taskForMedia(item, taskIndex)} key={item.id} onTranscode={onTranscode} />
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

function MediaCard({ item, task, onTranscode }: { item: MediaItem; task?: TranscodeTask; onTranscode: (item: MediaItem) => void }) {
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
            <MediaTaskIndicator task={task} />
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

function EpisodeRow({ item, task, onTranscode }: { item: MediaItem; task?: TranscodeTask; onTranscode: (item: MediaItem) => void }) {
  return (
    <motion.div layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Card>
        <CardContent className="flex gap-3 p-3">
          <Poster item={item} className="size-20 shrink-0" />
          <div className="min-w-0 flex-1">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0">
                <h3 className="truncate text-sm font-semibold">
                  {item.kind === "episode" ? `E${String(item.episode ?? 0).padStart(2, "0")} / ${item.title}` : item.title}
                </h3>
                <p className="mt-1 truncate text-xs text-muted-foreground">{item.library}</p>
              </div>
              <MediaBadges item={item} />
            </div>
            <MetaLine item={item} />
            <MediaTaskIndicator task={task} />
            <div className="mt-2 flex items-center justify-between gap-2">
              <span className="truncate text-xs text-muted-foreground">{item.ext.toUpperCase()} / {formatBytes(item.size)}</span>
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
    <div className={cn("flex aspect-2/3 items-center justify-center overflow-hidden rounded-md border bg-muted", className)}>
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

function MediaTaskIndicator({ task }: { task?: TranscodeTask }) {
  if (!task) return <div className="mt-2 min-h-6" aria-hidden="true" />;
  const complete = task.state === "complete";
  return (
    <Tip content={`Task ${task.id} is ${task.state}`}>
      <div className="mt-2 flex min-h-6 min-w-0 flex-wrap items-center gap-1.5">
        <Badge variant="outline" className="shrink-0">
          <ListVideo className="mr-1 size-3" />
          In task
        </Badge>
        <Badge variant={complete ? "default" : "warning"} className="shrink-0">
          {complete ? <CheckCircle2 className="mr-1 size-3" /> : <CircleAlert className="mr-1 size-3" />}
          {complete ? "Complete" : "Incomplete"}
        </Badge>
      </div>
    </Tip>
  );
}

function MetaLine({ item }: { item: MediaItem }) {
  const parts = [item.show, item.season ? `S${String(item.season).padStart(2, "0")}` : "", formatDate(item.modTime)].filter(Boolean);
  return <p className="mt-2 truncate text-xs text-muted-foreground">{parts.join(" / ")}</p>;
}

function TaskView({
  tasks,
  busy,
  onCancel,
  onRetry,
  onDelete
}: {
  tasks: TranscodeTask[];
  busy: boolean;
  onCancel: (id: string) => void;
  onRetry: (id: string) => void;
  onDelete: (id: string) => void;
}) {
  const [confirmingDelete, setConfirmingDelete] = React.useState("");

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
                      <CardTitle className="min-w-0 truncate">{task.input || task.inputRelPath || task.id}</CardTitle>
                    </div>
                    <p className="truncate text-xs text-muted-foreground">{task.outputDir}</p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 md:justify-end">
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
                    <Tip content={canDeleteTask(task) ? "Click once more to delete this task folder" : "Cancel running tasks before deleting"}>
                      <Button
                        variant={confirmingDelete === task.id ? "destructive" : "outline"}
                        size="sm"
                        disabled={busy || !canDeleteTask(task)}
                        onClick={() => {
                          if (confirmingDelete === task.id) {
                            setConfirmingDelete("");
                            onDelete(task.id);
                            return;
                          }
                          setConfirmingDelete(task.id);
                        }}
                      >
                        <Trash2 />
                        {confirmingDelete === task.id ? "Confirm" : "Delete"}
                      </Button>
                    </Tip>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <Progress value={stateProgress(task.state)} />
                <div className="mt-3 grid gap-2 text-xs text-muted-foreground sm:grid-cols-2 lg:grid-cols-5">
                  <span>Updated {formatDate(task.updatedAt)}</span>
                  <span>{task.encodedCodecs?.length ? task.encodedCodecs.join(", ") : "No encoded codec"}</span>
                  <span>{taskSubtitleLanguages(task).length ? `${taskSubtitleLanguages(task).join(", ")} subtitles` : "No subtitles"}</span>
                  <span>{task.duration ? `${Math.round(task.duration / 60)} min` : "Duration pending"}</span>
                  <span>{task.files ? `${Object.keys(task.files).length} files` : "Files pending"}</span>
                </div>
                {task.error ? (
                  <div className="mt-3 flex gap-2 rounded-md border border-rose-400/30 bg-rose-400/10 p-2 text-xs text-rose-100">
                    <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
                    <span className="min-w-0 break-words">{task.error}</span>
                  </div>
                ) : null}
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
                          {name} / {formatBytes(size)}
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

function DeleteFilteredDialog({
  open,
  totalCount,
  deletableCount,
  skippedCount,
  busy,
  onOpenChange,
  onConfirm
}: {
  open: boolean;
  totalCount: number;
  deletableCount: number;
  skippedCount: number;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete filtered tasks</DialogTitle>
          <DialogDescription>
            {deletableCount.toLocaleString()} of {totalCount.toLocaleString()} filtered task folders will be deleted from the output directory.
          </DialogDescription>
        </DialogHeader>
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-foreground">
          This removes generated files and job metadata for the matching tasks. {skippedCount ? `${skippedCount.toLocaleString()} running task${skippedCount === 1 ? "" : "s"} will be skipped.` : "This cannot be undone."}
        </div>
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Keep tasks
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={busy || deletableCount === 0}>
            {busy ? <Loader2 className="animate-spin" /> : <Trash2 />}
            Delete {deletableCount.toLocaleString()}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
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
      <CardContent className="flex min-w-0 items-center gap-3 p-4">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-primary">
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

function buildMediaTaskIndex(tasks: TranscodeTask[]) {
  const index: MediaTaskIndex = new Map();
  const newestFirst = [...tasks].sort((a, b) => new Date(b.updatedAt || b.createdAt).getTime() - new Date(a.updatedAt || a.createdAt).getTime());
  for (const task of newestFirst) {
    for (const key of taskMediaKeys(task)) {
      if (!index.has(key)) {
        index.set(key, task);
      }
    }
  }
  return index;
}

function taskForMedia(item: MediaItem, index: MediaTaskIndex) {
  for (const key of mediaKeys(item)) {
    const task = index.get(key);
    if (task) return task;
  }
  return undefined;
}

function mediaKeys(item: MediaItem) {
  const keys = [
    taskKey("media", item.id),
    taskKey("path", item.path),
    taskKey("rel", item.relPath),
    taskKey("file", item.fileName),
    taskKey("base", stripMediaExtension(item.fileName))
  ];
  if (item.kind === "episode" && item.season != null && item.episode != null) {
    keys.push(episodeKey(item.show || item.title, item.season, item.episode));
  }
  return uniqueKeys(keys);
}

function taskMediaKeys(task: TranscodeTask) {
  const parentPath = task.inputParent && task.input ? `${task.inputParent}/${task.input}` : "";
  const taskFile = task.input || fileNameFromPath(task.inputPath) || fileNameFromPath(task.inputRelPath);
  const keys = [
    taskKey("media", task.mediaId),
    taskKey("path", task.inputPath),
    taskKey("path", parentPath),
    taskKey("rel", task.inputRelPath),
    taskKey("file", taskFile),
    taskKey("base", stripMediaExtension(taskFile))
  ];
  const episode = parseEpisodeIdentity(taskFile);
  if (episode) {
    keys.push(episodeKey(episode.show, episode.season, episode.episode));
  }
  return uniqueKeys(keys);
}

function taskKey(kind: string, value?: string) {
  const normalized = normalizeMediaPath(value);
  return normalized ? `${kind}:${normalized}` : "";
}

function normalizeMediaPath(value?: string) {
  return value?.trim().replace(/\\/g, "/").replace(/\/+/g, "/").toLowerCase() ?? "";
}

function fileNameFromPath(value?: string) {
  const normalized = value?.replace(/\\/g, "/") ?? "";
  return normalized.split("/").filter(Boolean).pop() ?? "";
}

function stripMediaExtension(value?: string) {
  return (value ?? "").replace(/\.[a-z0-9]{2,5}$/i, "");
}

function parseEpisodeIdentity(value?: string) {
  const name = stripMediaExtension(fileNameFromPath(value) || value).replace(/[._]+/g, " ");
  const match = name.match(/^(.*?)\s*[- ]*\s*S(\d{1,2})E(\d{1,3})\b/i);
  if (!match) return undefined;
  return {
    show: match[1],
    season: Number(match[2]),
    episode: Number(match[3])
  };
}

function episodeKey(show: string, season: number, episode: number) {
  const normalizedShow = normalizeTitle(show);
  return normalizedShow ? `episode:${normalizedShow}:${season}:${episode}` : "";
}

function normalizeTitle(value?: string) {
  return (value ?? "")
    .toLowerCase()
    .replace(/&/g, " and ")
    .replace(/[^a-z0-9]+/g, " ")
    .trim()
    .replace(/\s+/g, " ");
}

function uniqueKeys(keys: string[]) {
  return Array.from(new Set(keys.filter(Boolean)));
}

function canDeleteTask(task: TranscodeTask) {
  return task.state !== "running";
}

function taskCodecs(task: TranscodeTask) {
  return (task.encodedCodecs ?? []).filter(Boolean);
}

function taskSubtitleLanguages(task: TranscodeTask) {
  const values = task.subtitleLanguages?.length
    ? task.subtitleLanguages
    : (task.streams ?? []).filter((stream) => stream.codecType === "subtitle").map((stream) => stream.language || "und");
  return Array.from(new Set(values.filter(Boolean))).sort((a, b) => a.localeCompare(b));
}

function taskOptions(tasks: TranscodeTask[], getValues: (task: TranscodeTask) => string[]) {
  return Array.from(new Set(tasks.flatMap((task) => getValues(task)))).sort((a, b) => a.localeCompare(b));
}

function filterTasks(tasks: TranscodeTask[], completion: string, codecFilters: string[], subtitleFilters: string[]) {
  return tasks.filter((task) => {
    if (completion === "complete" && task.state !== "complete") return false;
    if (completion === "incomplete" && task.state === "complete") return false;
    const codecs = taskCodecs(task);
    if (codecFilters.length && !codecFilters.some((codec) => codecs.includes(codec))) return false;
    const subtitles = taskSubtitleLanguages(task);
    if (subtitleFilters.length && !subtitleFilters.some((language) => subtitles.includes(language))) return false;
    return true;
  });
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
    const visited = `${(scan.dirsScanned ?? 0).toLocaleString()} dirs / ${(scan.filesScanned ?? 0).toLocaleString()} files`;
    return scan.currentPath ? `${visited} / ${scan.currentPath}` : visited;
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
      streams: task.streams ?? existing.streams,
      subtitleLanguages: task.subtitleLanguages ?? existing.subtitleLanguages
    };
  });
}

function upsertTask(tasks: TranscodeTask[], next: TranscodeTask) {
  let found = false;
  const updated = tasks.map((task) => {
    if (task.id !== next.id) return task;
    found = true;
    return mergeTaskDetails([next], [task])[0];
  });
  return found ? updated : [next, ...tasks];
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
