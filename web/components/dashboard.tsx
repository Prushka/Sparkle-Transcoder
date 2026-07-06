"use client";

import { AnimatePresence, motion } from "framer-motion";
import {
  BadgeCheck,
  Ban,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
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
  type TaskListResponse,
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
type CurrentMediaIndex = Map<string, MediaItem>;
type TaskDuplicateIndex = Map<string, number>;
type NewVersionInfo = {
  originalSize: number;
  currentSize: number;
};
type TaskReplaceCandidate = {
  task: TranscodeTask;
  item: MediaItem;
};
type TaskReplacePlan = {
  candidates: TaskReplaceCandidate[];
  runningCandidates: TaskReplaceCandidate[];
  runningTasks: TranscodeTask[];
  missingMediaTasks: TranscodeTask[];
  total: number;
};
type QueueCreateMode = "replace" | "incomplete";
type QueueMode = QueueCreateMode | "delete";
type LibrarySort = "title" | "recent";
type TriStateFilterState = "include" | "exclude";
type TriStateFilters = Record<string, TriStateFilterState>;

const IN_PROGRESS_FILTER = "In Progress";
const COMPLETED_FILTER = "Completed";
const NEW_VERSION_FILTER = "New Version";
const STORYBOARDS_FILTER = "Storyboards";
const DUPLICATE_FILTER = "Has Duplicate";
const LIVE_TASK_POLL_INTERVAL_MS = 30000;
const TASK_CANCEL_POLL_INTERVAL_MS = 1000;
const TASK_CANCEL_TIMEOUT_MS = 120000;
const LIBRARY_PAGE_SIZE = 300;

type TaskSelection = {
  title: string;
  description: string;
  items: MediaItem[];
  existingTasks: TranscodeTask[];
  bulk: boolean;
};

type QueuePlan = {
  items: MediaItem[];
  existingTasks: TranscodeTask[];
  runningTasks: TranscodeTask[];
  skippedCompleteItems: MediaItem[];
  incompleteItems: MediaItem[];
  nonExistingItems: MediaItem[];
};

type DialogMediaGroup = {
  key: string;
  label: string;
  items: MediaItem[];
  showHeader: boolean;
};

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
  const [librarySort, setLibrarySort] = React.useState<LibrarySort>("title");
  const [selected, setSelected] = React.useState<TaskSelection | null>(null);
  const [activeTab, setActiveTab] = React.useState("library");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [mediaLimit, setMediaLimit] = React.useState(LIBRARY_PAGE_SIZE);
  const [taskLimit, setTaskLimit] = React.useState(100);
  const [taskQuery, setTaskQuery] = React.useState("");
  const [taskStatusFilters, setTaskStatusFilters] = React.useState<TriStateFilters>({});
  const [codecFilters, setCodecFilters] = React.useState<TriStateFilters>({});
  const [subtitleFilters, setSubtitleFilters] = React.useState<TriStateFilters>({});
  const [deleteFilteredOpen, setDeleteFilteredOpen] = React.useState(false);
  const [replaceFilteredOpen, setReplaceFilteredOpen] = React.useState(false);
  const [taskRefreshing, setTaskRefreshing] = React.useState(false);
  const taskRefreshInFlight = React.useRef(false);
  const taskListInFlight = React.useRef(false);
  const scanWasRunning = React.useRef(false);

  const loadInitial = React.useCallback(async () => {
    try {
      const [config, toolsResponse] = await Promise.all([
        api.config(),
        api.tools()
      ]);
      setState((current) => ({
        ...current,
        config,
        tools: toolsResponse.tools
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to reach backend");
    }
  }, []);

  React.useEffect(() => {
    loadInitial();
  }, [loadInitial]);

  const loadScanStatus = React.useCallback(async () => {
    try {
      const scan = await api.scanStatus();
      setState((current) => ({ ...current, scan }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load scan status");
    }
  }, []);

  React.useEffect(() => {
    if (!state.scan?.running) return;
    const interval = window.setInterval(loadScanStatus, 5000);
    return () => window.clearInterval(interval);
  }, [loadScanStatus, state.scan?.running]);

  const loadLibrary = React.useCallback(async () => {
    try {
      const [mediaResponse, taskResponse] = await Promise.all([api.media(), api.tasks()]);
      setState((current) => ({
        ...current,
        media: mediaResponse.items,
        ...mergeTaskResponse(taskResponse, current.tasks),
        scan: mediaResponse.status
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load library");
    }
  }, []);

  const loadTasks = React.useCallback(async () => {
    try {
      const [taskResponse, mediaResponse] = await Promise.all([api.tasks(), api.media()]);
      setState((current) => ({
        ...current,
        ...mergeTaskResponse(taskResponse, current.tasks),
        media: mediaResponse.items,
        scan: mediaResponse.status
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load tasks");
    }
  }, []);

  const loadTaskList = React.useCallback(async () => {
    if (taskListInFlight.current) return;
    taskListInFlight.current = true;
    try {
      const taskResponse = await api.tasks();
      setState((current) => ({
        ...current,
        ...mergeTaskResponse(taskResponse, current.tasks)
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to load tasks");
    } finally {
      taskListInFlight.current = false;
    }
  }, []);

  const refreshTasks = React.useCallback(async () => {
    if (taskRefreshInFlight.current) return;
    taskRefreshInFlight.current = true;
    setTaskRefreshing(true);
    try {
      const [taskResponse, mediaResponse] = await Promise.all([api.refreshTasks(), api.media()]);
      setState((current) => ({
        ...current,
        ...mergeTaskResponse(taskResponse, current.tasks),
        media: mediaResponse.items,
        scan: mediaResponse.status
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
      void loadLibrary();
    }
  }, [activeTab, loadLibrary]);

  React.useEffect(() => {
    const running = Boolean(state.scan?.running);
    if (scanWasRunning.current && !running && activeTab === "library") {
      void loadLibrary();
    }
    scanWasRunning.current = running;
  }, [activeTab, loadLibrary, state.scan?.running]);

  React.useEffect(() => {
    if (activeTab === "tasks") {
      void loadTasks();
    }
  }, [activeTab, loadTasks]);

  const hasLiveTasks = React.useMemo(
    () => state.tasks.some(isInProgressTask) || Boolean(state.taskStatus?.activeTasks?.length),
    [state.taskStatus?.activeTasks?.length, state.tasks]
  );

  React.useEffect(() => {
    if (!hasLiveTasks) return;
    const interval = window.setInterval(() => {
      void loadTaskList();
    }, LIVE_TASK_POLL_INTERVAL_MS);
    return () => window.clearInterval(interval);
  }, [hasLiveTasks, loadTaskList]);

  const libraries = React.useMemo(() => Array.from(new Set(state.media.map((item) => item.library).filter(Boolean))).sort(), [state.media]);
  const queueScope = React.useMemo(() => {
    return state.media.filter((item) => {
      if (kind !== "all" && item.kind !== kind) return false;
      if (library !== "all" && item.library !== library) return false;
      return true;
    });
  }, [kind, library, state.media]);
  const filtered = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return queueScope;
    return queueScope.filter((item) => `${item.title} ${item.show ?? ""} ${item.fileName} ${item.library}`.toLowerCase().includes(q));
  }, [query, queueScope]);
  const sortedMedia = React.useMemo(() => sortLibraryItems(filtered, librarySort), [filtered, librarySort]);
  const visibleMedia = React.useMemo(() => limitLibraryItems(sortedMedia, mediaLimit, librarySort), [librarySort, mediaLimit, sortedMedia]);
  const hiddenMediaCount = sortedMedia.length - visibleMedia.length;
  const nextMediaLimit = visibleMedia.length + LIBRARY_PAGE_SIZE;
  const nextMediaCount = React.useMemo(() => {
    if (hiddenMediaCount <= 0) return 0;
    return Math.max(0, limitLibraryItems(sortedMedia, nextMediaLimit, librarySort).length - visibleMedia.length);
  }, [hiddenMediaCount, librarySort, nextMediaLimit, sortedMedia, visibleMedia.length]);
  const mediaTaskIndex = React.useMemo(() => buildMediaTaskIndex(state.tasks), [state.tasks]);
  const taskDuplicateIndex = React.useMemo(() => buildTaskDuplicateIndex(state.tasks), [state.tasks]);
  const currentMediaIndex = React.useMemo(() => buildCurrentMediaIndex(state.media), [state.media]);
  const taskCodecOptions = React.useMemo(() => taskOptions(state.tasks, taskCodecs), [state.tasks]);
  const taskSubtitleOptions = React.useMemo(() => taskOptions(state.tasks, taskSubtitleLanguages), [state.tasks]);
  const filteredTasks = React.useMemo(
    () => filterTasks(state.tasks, taskQuery, taskStatusFilters, codecFilters, subtitleFilters, currentMediaIndex, taskDuplicateIndex),
    [codecFilters, currentMediaIndex, state.tasks, subtitleFilters, taskDuplicateIndex, taskQuery, taskStatusFilters]
  );
  const visibleTasks = React.useMemo(() => filteredTasks.slice(0, taskLimit), [filteredTasks, taskLimit]);
  const runningFilteredTaskCount = React.useMemo(() => filteredTasks.filter(isInProgressTask).length, [filteredTasks]);
  const replaceFilteredPlan = React.useMemo(() => buildTaskReplacePlan(filteredTasks, currentMediaIndex), [currentMediaIndex, filteredTasks]);
  const replaceFilteredCount = taskReplaceActionCount(replaceFilteredPlan);
  const tasksRefreshing = taskRefreshing || Boolean(state.taskStatus?.refreshing);
  const activeRunnerTasks = state.taskStatus?.activeTasks ?? [];
  const taskStatDetail = activeRunnerTasks.length ? `Now ${activeTaskSummary(activeRunnerTasks)}` : `${countInProgressTasks(state.tasks)} in progress`;

  React.useEffect(() => {
    setMediaLimit(LIBRARY_PAGE_SIZE);
  }, [kind, library, librarySort, query]);

  React.useEffect(() => {
    setTaskLimit(100);
  }, [codecFilters, subtitleFilters, taskQuery, taskStatusFilters]);

  const startScan = async (force: boolean) => {
    setBusy(true);
    try {
      const scan = await api.startScan(force);
      setState((current) => ({ ...current, scan: { ...scan, running: true } }));
      if (activeTab === "library") {
        await loadLibrary();
        setState((current) => ({ ...current, scan: { ...(current.scan ?? scan), running: true } }));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Scan failed");
    } finally {
      setBusy(false);
    }
  };

  const openTaskDialog = React.useCallback((items: MediaItem[], title?: string, description?: string, bulk = false) => {
    const selectedItems = uniqueMediaItems(items);
    if (!selectedItems.length) return;
    const first = selectedItems[0];
    setSelected({
      title: title ?? first.title,
      description: description ?? first.fileName,
      items: selectedItems,
      existingTasks: existingTasksForMedia(selectedItems, state.tasks),
      bulk
    });
  }, [state.tasks]);

  const loadTaskSnapshot = React.useCallback(async () => {
    const taskResponse = await api.tasks();
    const snapshot = mergeTaskResponse(taskResponse, []);
    setState((current) => ({
      ...current,
      ...mergeTaskResponse(taskResponse, current.tasks)
    }));
    return snapshot.tasks;
  }, []);

  const cancelTasksAndWait = React.useCallback(async (tasksToCancel: TranscodeTask[]) => {
    const activeTasks = uniqueTasks(tasksToCancel.filter(isInProgressTask));
    if (!activeTasks.length) return loadTaskSnapshot();

    const ids = activeTasks.map((task) => task.id);
    const idSet = new Set(ids);
    setError(`Canceling ${ids.length.toLocaleString()} in-progress ${pluralize("task", ids.length)}...`);

    const canceledTasks = await Promise.all(ids.map((id) => api.cancelTask(id)));
    setState((current) => {
      let taskStatus = current.taskStatus;
      for (const task of canceledTasks) {
        taskStatus = updateTaskStatusTask(taskStatus, task);
      }
      return {
        ...current,
        tasks: canceledTasks.reduce((tasks, task) => upsertTask(tasks, task), current.tasks),
        taskStatus
      };
    });

    const deadline = Date.now() + TASK_CANCEL_TIMEOUT_MS;
    let latestTasks = await loadTaskSnapshot();
    while (latestTasks.some((task) => idSet.has(task.id) && isInProgressTask(task))) {
      if (Date.now() >= deadline) {
        throw new Error(`Timed out waiting for ${ids.length.toLocaleString()} ${pluralize("task", ids.length)} to cancel`);
      }
      await delay(TASK_CANCEL_POLL_INTERVAL_MS);
      latestTasks = await loadTaskSnapshot();
    }
    return latestTasks;
  }, [loadTaskSnapshot]);

  const createTasks = async (selection: TaskSelection, params: TaskParams, queueMode: QueueCreateMode) => {
    let taskSnapshot = state.tasks;
    let plan = buildQueuePlan(selection.items, taskSnapshot, selection.bulk ? queueMode : "replace");
    if (!plan.items.length) {
      setSelected(null);
      setError(plan.skippedCompleteItems.length ? `No tasks queued. ${plan.skippedCompleteItems.length.toLocaleString()} complete ${selectionMediaLabel(selection.items, plan.skippedCompleteItems.length)} skipped.` : "");
      return;
    }
    setBusy(true);
    try {
      if (plan.runningTasks.length) {
        taskSnapshot = await cancelTasksAndWait(plan.runningTasks);
        plan = buildQueuePlan(selection.items, taskSnapshot, selection.bulk ? queueMode : "replace");
        if (!plan.items.length) {
          setSelected(null);
          setError(plan.skippedCompleteItems.length ? `No tasks queued. ${plan.skippedCompleteItems.length.toLocaleString()} complete ${selectionMediaLabel(selection.items, plan.skippedCompleteItems.length)} skipped.` : "");
          return;
        }
      }
      const selectedItems = plan.items;
      const existingTasks = plan.existingTasks;

      let deletedTaskIds = new Set<string>();
      if (existingTasks.length) {
        const existingTaskIds = uniqueTasks(existingTasks).map((task) => task.id);
        const result = await api.deleteTasks(existingTaskIds);
        const failedIds = new Set(result.failures.map((failure) => failure.id));
        deletedTaskIds = new Set(existingTaskIds.filter((id) => !failedIds.has(id)));
        if (result.failures.length) {
          setState((current) => ({
            ...current,
            tasks: current.tasks.filter((task) => !deletedTaskIds.has(task.id))
          }));
          setSelected(null);
          setError(`${deletedTaskIds.size.toLocaleString()} existing task${deletedTaskIds.size === 1 ? "" : "s"} deleted, ${result.failures.length.toLocaleString()} could not be deleted. New replacements were not queued. ${result.failures[0].error}`);
          return;
        }
      }

      const created: TranscodeTask[] = [];
      const failures: string[] = [];
      for (const item of selectedItems) {
        try {
          const task = await api.createTask(item.id, { ...params, encoders: params.encoders ? [...params.encoders] : undefined });
          created.push(task);
        } catch (err) {
          const message = err instanceof Error ? err.message : "Task creation failed";
          failures.push(`${item.fileName}: ${message}`);
        }
      }
      setState((current) => ({
        ...current,
        tasks: created.reduce((tasks, task) => upsertTask(tasks, task), current.tasks.filter((task) => !deletedTaskIds.has(task.id)))
      }));
      setSelected(null);
      const skippedText = plan.skippedCompleteItems.length
        ? ` ${plan.skippedCompleteItems.length.toLocaleString()} complete ${selectionMediaLabel(selection.items, plan.skippedCompleteItems.length)} skipped.`
        : "";
      setError(failures.length ? `${created.length.toLocaleString()} queued, ${failures.length.toLocaleString()} failed.${skippedText} ${failures[0]}` : skippedText.trim());
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
        tasks: upsertTask(current.tasks, task),
        taskStatus: updateTaskStatusTask(current.taskStatus, task)
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
        tasks: upsertTask(current.tasks, task),
        taskStatus: updateTaskStatusTask(current.taskStatus, task)
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task retry failed");
    }
  };

  const deleteTask = async (id: string) => {
    setBusy(true);
    try {
      const task = state.tasks.find((candidate) => candidate.id === id);
      if (task && isInProgressTask(task)) {
        await cancelTasksAndWait([task]);
      }
      await api.deleteTask(id);
      setState((current) => ({
        ...current,
        tasks: current.tasks.filter((task) => task.id !== id),
        taskStatus: removeActiveTask(current.taskStatus, id)
      }));
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task deletion failed");
    } finally {
      setBusy(false);
    }
  };

  const deleteSelectionTasks = async (selection: TaskSelection) => {
    const existingTasks = existingTasksForMedia(selection.items, state.tasks);
    if (!existingTasks.length) return;

    setBusy(true);
    try {
      let tasksToDelete = existingTasks;
      if (tasksToDelete.some(isInProgressTask)) {
        const latestTasks = await cancelTasksAndWait(tasksToDelete.filter(isInProgressTask));
        tasksToDelete = latestTasksForIds(tasksToDelete, latestTasks);
      }
      const ids = tasksToDelete.map((task) => task.id);
      const result = await api.deleteTasks(ids);
      const failedIds = new Set(result.failures.map((failure) => failure.id));
      const deletedIds = new Set(ids.filter((id) => !failedIds.has(id)));
      setState((current) => ({
        ...current,
        tasks: current.tasks.filter((task) => !deletedIds.has(task.id)),
        taskStatus: removeActiveTasks(current.taskStatus, deletedIds)
      }));
      setSelected(null);
      setError(result.failures.length ? `${result.deleted.toLocaleString()} task ${pluralize("folder", result.deleted)} deleted, ${result.failures.length.toLocaleString()} could not be deleted. ${result.failures[0].error}` : "");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task deletion failed");
    } finally {
      setBusy(false);
    }
  };

  const deleteFilteredTasks = async () => {
    if (!filteredTasks.length) return;
    setBusy(true);
    try {
      let tasksToDelete = filteredTasks;
      if (tasksToDelete.some(isInProgressTask)) {
        const latestTasks = await cancelTasksAndWait(tasksToDelete.filter(isInProgressTask));
        tasksToDelete = latestTasksForIds(tasksToDelete, latestTasks);
      }
      const ids = tasksToDelete.map((task) => task.id);
      const result = await api.deleteTasks(ids);
      const failedIds = new Set(result.failures.map((failure) => failure.id));
      const deletedIds = new Set(ids.filter((id) => !failedIds.has(id)));
      setState((current) => ({
        ...current,
        tasks: current.tasks.filter((task) => !deletedIds.has(task.id)),
        taskStatus: removeActiveTasks(current.taskStatus, deletedIds)
      }));
      setDeleteFilteredOpen(false);
      setError(result.failures.length ? `${result.deleted} tasks deleted, ${result.failures.length} could not be deleted.` : "");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Filtered delete failed");
    } finally {
      setBusy(false);
    }
  };

  const queueReplaceTasks = async (tasksToReplace: TranscodeTask[], onDone?: () => void) => {
    let replaceTasks = uniqueTasks(tasksToReplace);
    let plan = buildTaskReplacePlan(replaceTasks, currentMediaIndex);
    if (!taskReplaceActionCount(plan)) {
      setError(taskReplaceBlockedMessage(plan));
      return;
    }

    setBusy(true);
    try {
      if (plan.runningTasks.length) {
        const latestTasks = await cancelTasksAndWait(plan.runningTasks);
        replaceTasks = latestTasksForIds(replaceTasks, latestTasks);
        plan = buildTaskReplacePlan(replaceTasks, currentMediaIndex);
        if (!taskReplaceActionCount(plan)) {
          setError(taskReplaceBlockedMessage(plan));
          return;
        }
      }
      const ids = plan.candidates.map(({ task }) => task.id);
      const result = await api.deleteTasks(ids);
      const failedDeleteIds = new Set(result.failures.map((failure) => failure.id));
      const deletedIds = new Set(ids.filter((id) => !failedDeleteIds.has(id)));
      const created: TranscodeTask[] = [];
      const createFailures: string[] = [];

      for (const candidate of plan.candidates) {
        if (!deletedIds.has(candidate.task.id)) continue;
        try {
          const task = await api.createTask(candidate.item.id, cloneTaskParams(candidate.task.params));
          created.push(task);
        } catch (err) {
          const message = err instanceof Error ? err.message : "Task creation failed";
          createFailures.push(`${candidate.task.input || candidate.task.id}: ${message}`);
        }
      }

      setState((current) => ({
        ...current,
        tasks: created.reduce((tasks, task) => upsertTask(tasks, task), current.tasks.filter((task) => !deletedIds.has(task.id))),
        taskStatus: removeActiveTasks(current.taskStatus, deletedIds)
      }));
      onDone?.();

      const skipped = plan.missingMediaTasks.length + result.failures.length;
      if (createFailures.length) {
        setError(`${created.length.toLocaleString()} replacement ${pluralize("task", created.length)} queued, ${createFailures.length.toLocaleString()} failed to queue.${skipped ? ` ${skipped.toLocaleString()} skipped.` : ""} ${createFailures[0]}`);
      } else if (result.failures.length) {
        setError(`${created.length.toLocaleString()} replacement ${pluralize("task", created.length)} queued, ${result.failures.length.toLocaleString()} could not be deleted. ${result.failures[0].error}`);
      } else {
        const skippedText = plan.missingMediaTasks.length
          ? ` ${plan.missingMediaTasks.length} skipped.`
          : "";
        setError(skippedText.trim());
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Retry failed");
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
              <div className="mt-2 flex min-w-0 flex-wrap gap-2 text-xs text-muted-foreground">
                <Badge variant="outline" title={state.config?.mediaRoot ?? "Media root"}>
                  <span className="min-w-0 truncate">{state.config?.mediaRoot ?? "Media root"}</span>
                </Badge>
                <Badge variant="outline" title={state.config?.output ?? "Output"}>
                  <span className="min-w-0 truncate">{state.config?.output ?? "Output"}</span>
                </Badge>
              </div>
            </div>
            <div className="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto sm:flex-wrap sm:items-center">
              <Tip content="Run the default incremental scan">
                <Button className="w-full sm:w-auto" variant="secondary" onClick={() => startScan(false)} disabled={busy || state.scan?.running}>
                  {state.scan?.running ? <Loader2 className="animate-spin" /> : <FolderSync />}
                  Scan
                </Button>
              </Tip>
              <Tip content="Ignore cache and rescan configured media folders">
                <Button className="w-full sm:w-auto" variant="outline" onClick={() => startScan(true)} disabled={busy || state.scan?.running}>
                  <RefreshCcw />
                  Full
                </Button>
              </Tip>
            </div>
          </header>

          {error ? <div className="mb-4 rounded-lg border border-rose-400/30 bg-rose-400/10 p-3 text-sm text-rose-100">{error}</div> : null}

          <section className="mb-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Stat icon={HardDrive} label="Media" value={state.media.length.toLocaleString()} detail={`${filtered.length.toLocaleString()} visible`} />
            <Stat icon={ListVideo} label="Tasks" value={state.tasks.length.toLocaleString()} detail={taskStatDetail} />
            <Stat icon={BadgeCheck} label="Complete" value={countByState(state.tasks, "complete").toLocaleString()} detail={`${countByState(state.tasks, "failed")} failed`} />
            <Stat icon={Gauge} label="Scan" value={state.scan?.running ? "Running" : "Idle"} detail={scanDetail(state.scan)} />
          </section>

          <ActiveTaskStrip tasks={activeRunnerTasks} />

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
                sort={librarySort}
                setSort={setLibrarySort}
              />
              <LibraryView items={visibleMedia} queueItems={queueScope} sort={librarySort} taskIndex={mediaTaskIndex} busy={busy} onTranscode={openTaskDialog} onDeleteTask={deleteTask} />
              {hiddenMediaCount > 0 ? (
                <div className="mt-5 flex justify-center">
                  <Tip content="Render the next batch of matching media">
                    <Button variant="outline" onClick={() => setMediaLimit(nextMediaLimit)}>
                      Show {Math.max(1, nextMediaCount).toLocaleString()} more
                    </Button>
                  </Tip>
                </div>
              ) : null}
            </TabsContent>
            <TabsContent value="tasks">
              <TaskToolbar
                query={taskQuery}
                onQueryChange={setTaskQuery}
                taskStatusFilters={taskStatusFilters}
                onTaskStatusFiltersChange={setTaskStatusFilters}
                codecOptions={taskCodecOptions}
                codecFilters={codecFilters}
                onCodecFiltersChange={setCodecFilters}
                subtitleOptions={taskSubtitleOptions}
                subtitleFilters={subtitleFilters}
                onSubtitleFiltersChange={setSubtitleFilters}
                filteredCount={filteredTasks.length}
                totalCount={state.tasks.length}
                deletableCount={filteredTasks.length}
                replaceableCount={replaceFilteredCount}
                onRefresh={refreshTasks}
                refreshing={tasksRefreshing}
                refreshedAt={state.taskStatus?.refreshedAt}
                onDeleteFiltered={() => setDeleteFilteredOpen(true)}
                onReplaceFiltered={() => setReplaceFilteredOpen(true)}
                disabled={busy || tasksRefreshing}
              />
              <TaskView tasks={visibleTasks} currentMediaIndex={currentMediaIndex} busy={busy} onCancel={cancelTask} onRetry={retryTask} onDelete={deleteTask} onQueueReplace={(task) => queueReplaceTasks([task])} />
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
      <TaskDialog
        selection={selected}
        config={state.config}
        busy={busy}
        onOpenChange={(open) => !open && setSelected(null)}
        onCreate={createTasks}
        onDeleteExisting={deleteSelectionTasks}
      />
      <DeleteFilteredDialog
        open={deleteFilteredOpen}
        totalCount={filteredTasks.length}
        deletableCount={filteredTasks.length}
        runningCount={runningFilteredTaskCount}
        busy={busy}
        onOpenChange={setDeleteFilteredOpen}
        onConfirm={deleteFilteredTasks}
      />
      <ReplaceFilteredDialog
        open={replaceFilteredOpen}
        plan={replaceFilteredPlan}
        busy={busy}
        onOpenChange={setReplaceFilteredOpen}
        onConfirm={() => queueReplaceTasks(filteredTasks, () => setReplaceFilteredOpen(false))}
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
  libraries,
  sort,
  setSort
}: {
  query: string;
  setQuery: (value: string) => void;
  kind: string;
  setKind: (value: string) => void;
  library: string;
  setLibrary: (value: string) => void;
  libraries: string[];
  sort: LibrarySort;
  setSort: (value: LibrarySort) => void;
}) {
  return (
    <div className="mb-4 grid gap-2 md:grid-cols-[minmax(0,1fr)_180px_220px_200px]">
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
      <Tip content="Sort library results">
        <div>
          <Select value={sort} onValueChange={(value) => setSort(asLibrarySort(value))}>
            <SelectTrigger>
              <span>{librarySortLabel(sort)}</span>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="title">Title</SelectItem>
              <SelectItem value="recent">Recently added</SelectItem>
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
            {readyCount}/{tools.length || 4} ready
          </Badge>
        </div>
      </div>
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {(tools.length ? tools : ["ffmpeg", "ffprobe", "mkvextract", "handbrake"]).map((tool) => {
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
                  <div className="mt-1 break-all text-xs leading-snug text-muted-foreground">{item.command}</div>
                </div>
                <Badge variant={item.ready ? "default" : "outline"}>{item.ready ? "Ready" : pending ? "Checking" : "Missing"}</Badge>
              </div>
              <div className="mt-3 min-h-8 text-xs text-muted-foreground">
                {item.ready ? (
                  <div className="break-words leading-snug" title={item.version || item.path}>
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

function ActiveTaskStrip({ tasks }: { tasks: TranscodeTask[] }) {
  if (!tasks.length) return null;
  return (
    <section className="mb-5 rounded-lg border bg-card/60 p-3">
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/15 text-primary">
            <Loader2 className="size-5 animate-spin" />
          </div>
          <div className="min-w-0">
            <div className="text-xs text-muted-foreground">Processing now</div>
            <div className="break-words text-sm font-medium leading-snug">{activeTaskSummary(tasks)}</div>
          </div>
        </div>
        <div className="flex min-w-0 flex-wrap gap-2 md:justify-end">
          {tasks.map((task) => (
            <Badge key={task.id} variant="secondary" className="max-w-full">
              <ListVideo className="mr-1 size-3 shrink-0" />
              <span className="min-w-0 break-words">
                {taskPhaseLabel(task.state)} / {task.id}
              </span>
            </Badge>
          ))}
        </div>
      </div>
    </section>
  );
}

function TaskToolbar({
  query,
  onQueryChange,
  taskStatusFilters,
  onTaskStatusFiltersChange,
  codecOptions,
  codecFilters,
  onCodecFiltersChange,
  subtitleOptions,
  subtitleFilters,
  onSubtitleFiltersChange,
  filteredCount,
  totalCount,
  deletableCount,
  replaceableCount,
  onRefresh,
  refreshing,
  refreshedAt,
  onDeleteFiltered,
  onReplaceFiltered,
  disabled
}: {
  query: string;
  onQueryChange: (value: string) => void;
  taskStatusFilters: TriStateFilters;
  onTaskStatusFiltersChange: (value: TriStateFilters) => void;
  codecOptions: string[];
  codecFilters: TriStateFilters;
  onCodecFiltersChange: (value: TriStateFilters) => void;
  subtitleOptions: string[];
  subtitleFilters: TriStateFilters;
  onSubtitleFiltersChange: (value: TriStateFilters) => void;
  filteredCount: number;
  totalCount: number;
  deletableCount: number;
  replaceableCount: number;
  onRefresh: () => void;
  refreshing: boolean;
  refreshedAt?: string;
  onDeleteFiltered: () => void;
  onReplaceFiltered: () => void;
  disabled: boolean;
}) {
  const hasFilters = query.trim() !== "" || hasTriStateFilters(taskStatusFilters) || hasTriStateFilters(codecFilters) || hasTriStateFilters(subtitleFilters);
  return (
    <div className="mb-4 rounded-lg border bg-card/60 p-3">
      <div className="mb-3 flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
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
                    onQueryChange("");
                    onTaskStatusFiltersChange({});
                    onCodecFiltersChange({});
                    onSubtitleFiltersChange({});
                  }}
                >
                  <X />
                  Clear
                </Button>
              </Tip>
            ) : null}
          </div>
          <div className="relative mt-3 max-w-xl">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => onQueryChange(event.target.value)}
              className="pl-9"
              placeholder="Search task, file, path, state, codec, subtitle"
            />
          </div>
        </div>
        <div className="grid w-full shrink-0 gap-2 sm:flex sm:flex-wrap xl:w-auto xl:justify-end">
          <Tip content="Scan the output folder and reload task metadata">
            <Button className="w-full sm:w-auto" type="button" variant="secondary" onClick={onRefresh} disabled={disabled || refreshing}>
              {refreshing ? <Loader2 className="animate-spin" /> : <RefreshCcw />}
              Refresh
            </Button>
          </Tip>
          <Tip content="Delete every retryable task matching the current filters and queue it again with the same parameters">
            <Button className="w-full sm:w-auto" variant="secondary" onClick={onReplaceFiltered} disabled={disabled || replaceableCount === 0}>
              <RotateCcw />
              Retry filtered
            </Button>
          </Tip>
          <Tip content="Delete every task matching the current filters. In-progress tasks will be canceled first.">
            <Button className="w-full sm:w-auto" variant="destructive" onClick={onDeleteFiltered} disabled={disabled || deletableCount === 0}>
              <Trash2 />
              Delete filtered
            </Button>
          </Tip>
        </div>
      </div>
      <div className="grid w-full gap-3 lg:grid-cols-[minmax(0,260px)_minmax(0,260px)_minmax(0,1fr)]">
        <TriStateFilterGroup
          title="Task Status"
          options={[IN_PROGRESS_FILTER, COMPLETED_FILTER, NEW_VERSION_FILTER, STORYBOARDS_FILTER, DUPLICATE_FILTER]}
          filters={taskStatusFilters}
          onChange={onTaskStatusFiltersChange}
          emptyLabel="No task status options"
        />
        <TriStateFilterGroup title="Encoded codecs" options={codecOptions} filters={codecFilters} onChange={onCodecFiltersChange} emptyLabel="No encoded codecs yet" />
        <TriStateFilterGroup
          title="Subtitle languages"
          options={subtitleOptions}
          filters={subtitleFilters}
          onChange={onSubtitleFiltersChange}
          emptyLabel="No subtitle languages yet"
        />
      </div>
    </div>
  );
}

function TriStateFilterGroup({
  title,
  options,
  filters,
  onChange,
  emptyLabel
}: {
  title: string;
  options: string[];
  filters: TriStateFilters;
  onChange: (value: TriStateFilters) => void;
  emptyLabel: string;
}) {
  return (
    <div className="min-w-0">
      <div className="mb-1 text-xs font-medium text-muted-foreground">{title}</div>
      <div className="flex min-h-7 flex-wrap gap-2">
        {options.length ? (
          options.map((option) => {
            const state = filters[option];
            return (
              <Tip content={triStateFilterTip(option, state)} key={option}>
                <Button
                  type="button"
                  variant={state === "include" ? "default" : "outline"}
                  size="sm"
                  className={cn("h-7 max-w-full px-2", state === "exclude" && "border-destructive/60 text-destructive hover:bg-destructive/10 hover:text-destructive")}
                  onClick={() => onChange(nextTriStateFilters(filters, option))}
                >
                  {state === "include" ? <CheckCircle2 /> : state === "exclude" ? <Ban /> : null}
                  <span className="truncate">{option}</span>
                </Button>
              </Tip>
            );
          })
        ) : (
          <span className="text-xs text-muted-foreground">{emptyLabel}</span>
        )}
      </div>
    </div>
  );
}

function LibraryView({
  items,
  queueItems,
  sort,
  taskIndex,
  busy,
  onTranscode,
  onDeleteTask
}: {
  items: MediaItem[];
  queueItems: MediaItem[];
  sort: LibrarySort;
  taskIndex: MediaTaskIndex;
  busy: boolean;
  onTranscode: (items: MediaItem[], title?: string, description?: string, bulk?: boolean) => void;
  onDeleteTask: (id: string) => void;
}) {
  const movies = items.filter((item) => item.kind === "movie");
  const shows = groupEpisodes(items.filter((item) => item.kind === "episode"), sort);
  const unknown = items.filter((item) => item.kind === "unknown");
  const queueEpisodes = queueItems.filter((item) => item.kind === "episode");
  const [collapsedShows, setCollapsedShows] = React.useState<string[]>([]);
  const [collapsedSeasons, setCollapsedSeasons] = React.useState<string[]>([]);

  return (
    <div className="space-y-6">
      {movies.length ? (
        <MediaSection title="Movies" icon={Film}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
            {movies.map((item) => (
              <MediaCard item={item} task={taskForMedia(item, taskIndex)} busy={busy} key={item.id} onTranscode={(media) => onTranscode([media])} onDeleteTask={onDeleteTask} />
            ))}
          </div>
        </MediaSection>
      ) : null}
      {shows.map((show) => {
        const showItems = itemsForShow(queueEpisodes, show.name);
        const showCollapsed = collapsedShows.includes(show.name);
        return (
          <MediaSection
            title={show.name}
            icon={Tv}
            key={show.name}
            collapsed={showCollapsed}
            onToggle={() => setCollapsedShows((current) => toggleID(current, show.name))}
            action={
              <Tip content={`Queue all ${showItems.length.toLocaleString()} episodes in this show`}>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => onTranscode(showItems, show.name, showSelectionDescription(show.name, showItems), true)}
                  disabled={!showItems.length}
                >
                  <Play />
                  Queue show
                </Button>
              </Tip>
            }
          >
            {showCollapsed ? null : (
              <div className="space-y-4">
                {show.seasons.map((season) => {
                  const seasonItems = itemsForSeason(showItems, season.number);
                  const seasonKey = `${show.name}:${season.number}`;
                  const seasonCollapsed = collapsedSeasons.includes(seasonKey);
                  return (
                    <div key={`${show.name}-${season.number}`} className="space-y-2">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="min-w-0">
                          <Tip content={`${seasonCollapsed ? "Expand" : "Collapse"} season ${season.number}`}>
                            <Button
                              type="button"
                              variant="ghost"
                              className="h-auto justify-start whitespace-normal px-1.5 py-1 text-left"
                              aria-expanded={!seasonCollapsed}
                              aria-label={`${seasonCollapsed ? "Expand" : "Collapse"} season ${season.number}`}
                              onClick={() => setCollapsedSeasons((current) => toggleID(current, seasonKey))}
                            >
                              {seasonCollapsed ? <ChevronRight /> : <ChevronDown />}
                              <span className="truncate text-sm font-medium text-muted-foreground">Season {season.number}</span>
                              <Badge variant="outline">{season.items.length}</Badge>
                            </Button>
                          </Tip>
                        </div>
                        <Tip content={`Queue all ${seasonItems.length.toLocaleString()} episodes in this season`}>
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() =>
                              onTranscode(
                                seasonItems,
                                `${show.name} / Season ${season.number}`,
                                selectionCountDescription(seasonItems.length, "episode"),
                                true
                              )
                            }
                            disabled={!seasonItems.length}
                          >
                            <Play />
                            Queue season
                          </Button>
                        </Tip>
                      </div>
                      {seasonCollapsed ? null : (
                        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                          {season.items.map((item) => (
                            <EpisodeRow item={item} task={taskForMedia(item, taskIndex)} busy={busy} key={item.id} onTranscode={(media) => onTranscode([media])} onDeleteTask={onDeleteTask} />
                          ))}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </MediaSection>
        );
      })}
      {unknown.length ? (
        <MediaSection title="Unmatched" icon={Video}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {unknown.map((item) => (
              <EpisodeRow item={item} task={taskForMedia(item, taskIndex)} busy={busy} key={item.id} onTranscode={(media) => onTranscode([media])} onDeleteTask={onDeleteTask} />
            ))}
          </div>
        </MediaSection>
      ) : null}
      {!items.length ? <EmptyState label="No media found" /> : null}
    </div>
  );
}

function MediaSection({
  title,
  icon: Icon,
  collapsed,
  onToggle,
  action,
  children
}: {
  title: string;
  icon: React.ElementType;
  collapsed?: boolean;
  onToggle?: () => void;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          {onToggle ? (
            <Tip content={`${collapsed ? "Expand" : "Collapse"} ${title}`}>
              <Button
                type="button"
                variant="ghost"
                className="h-auto justify-start whitespace-normal px-1.5 py-1 text-left"
                aria-expanded={!collapsed}
                aria-label={`${collapsed ? "Expand" : "Collapse"} ${title}`}
                onClick={onToggle}
              >
                {collapsed ? <ChevronRight /> : <ChevronDown />}
                <Icon className="size-4 shrink-0 text-primary" />
                <span className="break-words text-lg font-semibold leading-tight tracking-normal">{title}</span>
              </Button>
            </Tip>
          ) : (
            <div className="flex min-w-0 items-center gap-2">
              <Icon className="size-4 shrink-0 text-primary" />
              <h2 className="break-words text-lg font-semibold leading-tight tracking-normal">{title}</h2>
            </div>
          )}
        </div>
        {action ? <div className="shrink-0">{action}</div> : null}
      </div>
      {children}
    </section>
  );
}

function MediaCard({
  item,
  task,
  busy,
  onTranscode,
  onDeleteTask
}: {
  item: MediaItem;
  task?: TranscodeTask;
  busy: boolean;
  onTranscode: (item: MediaItem) => void;
  onDeleteTask: (id: string) => void;
}) {
  return (
    <motion.div className="h-full" layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Card className="h-full overflow-hidden">
        <div className="grid h-full min-h-[168px] grid-cols-[112px_minmax(0,1fr)] sm:min-h-[186px] sm:grid-cols-[124px_minmax(0,1fr)]">
          <Poster item={item} className="aspect-auto h-full min-h-[168px] w-full self-stretch rounded-none border-y-0 border-l-0 border-r sm:min-h-[186px]" />
          <div className="flex min-w-0 flex-col p-3">
            <div className="mb-2 flex items-start justify-between gap-2">
              <div className="min-w-0 flex-1">
                <h3 className="break-words text-sm font-semibold leading-snug">{item.title}</h3>
                <p className="mt-1 break-all text-xs leading-snug text-muted-foreground">{item.fileName}</p>
              </div>
              <MediaBadges item={item} />
            </div>
            <MetaLine item={item} />
            <MediaTaskIndicator item={item} task={task} />
            <div className="mt-auto flex flex-wrap items-center justify-between gap-2 pt-3">
              <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{formatBytes(item.size)}</span>
              <div className="flex shrink-0 items-center gap-2">
                <MediaTaskDeleteButton task={task} busy={busy} onDelete={onDeleteTask} />
                <Tip content="Create a transcoding task for this media file">
                  <Button size="sm" onClick={() => onTranscode(item)}>
                    <Play />
                    Queue
                  </Button>
                </Tip>
              </div>
            </div>
          </div>
        </div>
      </Card>
    </motion.div>
  );
}

function EpisodeRow({
  item,
  task,
  busy,
  onTranscode,
  onDeleteTask
}: {
  item: MediaItem;
  task?: TranscodeTask;
  busy: boolean;
  onTranscode: (item: MediaItem) => void;
  onDeleteTask: (id: string) => void;
}) {
  return (
    <motion.div className="h-full" layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.18 }}>
      <Card className="h-full">
        <CardContent className="flex h-full gap-3 p-3">
          <Poster item={item} className="size-20 shrink-0 self-start" />
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="flex items-start justify-between gap-2">
              <div className="min-w-0 flex-1">
                <h3 className="break-words text-sm font-semibold leading-snug">
                  {item.kind === "episode" ? `E${String(item.episode ?? 0).padStart(2, "0")} / ${item.title}` : item.title}
                </h3>
                <p className="mt-1 break-all text-xs leading-snug text-muted-foreground">{item.library}</p>
              </div>
              <MediaBadges item={item} />
            </div>
            <MetaLine item={item} />
            <MediaTaskIndicator item={item} task={task} />
            <div className="mt-auto flex flex-wrap items-center justify-between gap-2 pt-2">
              <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{item.ext.toUpperCase()} / {formatBytes(item.size)}</span>
              <div className="flex shrink-0 items-center gap-2">
                <MediaTaskDeleteButton task={task} busy={busy} onDelete={onDeleteTask} />
                <Tip content="Create a transcoding task for this episode">
                  <Button size="sm" variant="secondary" onClick={() => onTranscode(item)}>
                    <Play />
                    Queue
                  </Button>
                </Tip>
              </div>
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
    </div>
  );
}

function MediaTaskIndicator({ item, task }: { item: MediaItem; task?: TranscodeTask }) {
  if (!task) return <div className="mt-2 min-h-12" aria-hidden="true" />;
  const complete = task.state === "complete";
  const inProgress = isInProgressTask(task);
  const newVersion = newVersionInfoForMedia(item, task);
  const taskTip = newVersion
    ? `Task ${task.id} is ${task.state}. Current media is ${formatVersionBytes(newVersion.currentSize)}; task original was ${formatVersionBytes(newVersion.originalSize)}.`
    : `Task ${task.id} is ${task.state}`;
  return (
    <Tip content={taskTip}>
      <div className="mt-2 flex min-h-12 min-w-0 flex-wrap items-start gap-1.5">
        <Badge variant="outline" className="shrink-0">
          <ListVideo className="mr-1 size-3" />
          In task
        </Badge>
        <Badge variant={complete ? "default" : "warning"} className="shrink-0">
          {complete ? <CheckCircle2 className="mr-1 size-3" /> : inProgress ? <Loader2 className="mr-1 size-3 animate-spin" /> : <CircleAlert className="mr-1 size-3" />}
          {complete ? "Complete" : inProgress ? "In progress" : "Incomplete"}
        </Badge>
        {newVersion ? <NewVersionBadge info={newVersion} /> : null}
      </div>
    </Tip>
  );
}

function NewVersionBadge({ info }: { info: NewVersionInfo }) {
  const label = `New Version [${formatVersionBytes(info.originalSize)} / ${formatVersionBytes(info.currentSize)}]`;
  return (
    <Badge variant="warning" className="max-w-full" title={label}>
      <CircleAlert className="mr-1 size-3" />
      <span className="min-w-0 truncate">{label}</span>
    </Badge>
  );
}

function MediaTaskDeleteButton({ task, busy, onDelete }: { task?: TranscodeTask; busy: boolean; onDelete: (id: string) => void }) {
  const [confirmingDelete, setConfirmingDelete] = React.useState(false);

  React.useEffect(() => {
    setConfirmingDelete(false);
  }, [task?.id]);

  if (!task) return null;
  const deletable = canDeleteTask(task);
  return (
    <Tip content={deletable ? "Click once more to delete this task folder" : "Click once more to cancel this task and delete its folder"}>
      <Button
        type="button"
        variant={confirmingDelete ? "destructive" : "outline"}
        size="sm"
        disabled={busy}
        onClick={() => {
          if (confirmingDelete) {
            setConfirmingDelete(false);
            onDelete(task.id);
            return;
          }
          setConfirmingDelete(true);
        }}
      >
        <Trash2 />
        {confirmingDelete ? "Confirm" : "Delete"}
      </Button>
    </Tip>
  );
}

function MetaLine({ item }: { item: MediaItem }) {
  const parts = [item.show, item.season ? `S${String(item.season).padStart(2, "0")}` : "", formatDate(item.modTime)].filter(Boolean);
  return <p className="mt-2 break-words text-xs leading-snug text-muted-foreground">{parts.join(" / ")}</p>;
}

function TaskView({
  tasks,
  currentMediaIndex,
  busy,
  onCancel,
  onRetry,
  onDelete,
  onQueueReplace
}: {
  tasks: TranscodeTask[];
  currentMediaIndex: CurrentMediaIndex;
  busy: boolean;
  onCancel: (id: string) => void;
  onRetry: (id: string) => void;
  onDelete: (id: string) => void;
  onQueueReplace: (task: TranscodeTask) => void;
}) {
  const [confirmingDelete, setConfirmingDelete] = React.useState("");
  const [confirmingReplace, setConfirmingReplace] = React.useState("");

  return (
    <div className="space-y-3">
      <AnimatePresence initial={false}>
        {tasks.map((task) => {
          const newVersion = newVersionInfoForTask(task, currentMediaIndex);
          const canReplace = canQueueReplaceTask(task, currentMediaIndex);
          const canQueueReplacement = canQueueReplaceTaskAfterCancel(task, currentMediaIndex);
          return (
            <motion.div key={task.id} layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }}>
              <Card>
                <CardHeader className="pb-3">
                  <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                    <div className="min-w-0 md:flex-1">
                      <div className="mb-2 flex flex-wrap items-center gap-2">
                        <CardTitle className="min-w-0 break-words leading-tight">{task.input || task.inputRelPath || task.id}</CardTitle>
                        {newVersion ? <NewVersionBadge info={newVersion} /> : null}
                      </div>
                      <p className="break-all text-xs leading-snug text-muted-foreground">{task.outputDir}</p>
                    </div>
                    <div className="grid w-full gap-2 sm:flex sm:w-auto sm:flex-wrap sm:items-center md:justify-end">
                      {isInProgressTask(task) ? (
                        <Tip content="Cancel this in-progress task">
                          <Button className="w-full sm:w-auto" variant="destructive" size="sm" onClick={() => onCancel(task.id)}>
                            <Ban />
                            Cancel
                          </Button>
                        </Tip>
                      ) : null}
                      {(task.state === "failed" || task.state === "canceled") && !canReplace ? (
                        <Tip content="Queue this task again with the same parameters">
                          <Button className="w-full sm:w-auto" variant="secondary" size="sm" onClick={() => onRetry(task.id)}>
                            <RotateCcw />
                            Retry
                          </Button>
                        </Tip>
                      ) : null}
                      <Tip content={taskReplaceButtonTip(task, currentMediaIndex, confirmingReplace === task.id)}>
                        <Button
                          variant={confirmingReplace === task.id ? "destructive" : "secondary"}
                          size="sm"
                          className="w-full sm:w-auto"
                          disabled={busy || !canQueueReplacement}
                          onClick={() => {
                            if (confirmingReplace === task.id) {
                              setConfirmingReplace("");
                              setConfirmingDelete("");
                              onQueueReplace(task);
                              return;
                            }
                            setConfirmingReplace(task.id);
                            setConfirmingDelete("");
                          }}
                        >
                          <RotateCcw />
                          {confirmingReplace === task.id ? "Confirm retry" : "Retry"}
                        </Button>
                      </Tip>
                      <Tip content={canDeleteTask(task) ? "Click once more to delete this task folder" : "Click once more to cancel this task and delete its folder"}>
                        <Button
                          variant={confirmingDelete === task.id ? "destructive" : "outline"}
                          size="sm"
                          className="w-full sm:w-auto"
                          disabled={busy}
                          onClick={() => {
                            if (confirmingDelete === task.id) {
                              setConfirmingDelete("");
                              onDelete(task.id);
                              return;
                            }
                            setConfirmingDelete(task.id);
                            setConfirmingReplace("");
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
                    <span className="min-w-0 truncate">Updated {formatDate(task.updatedAt)}</span>
                    <span className="min-w-0 truncate">{task.encodedCodecs?.length ? task.encodedCodecs.join(", ") : "No encoded codec"}</span>
                    <span className="min-w-0 truncate">{taskSubtitleLanguages(task).length ? `${taskSubtitleLanguages(task).join(", ")} subtitles` : "No subtitles"}</span>
                    <span className="min-w-0 truncate">{task.duration ? `${Math.round(task.duration / 60)} min` : "Duration pending"}</span>
                    <span className="min-w-0 truncate">{task.files ? `${Object.keys(task.files).length} files` : "Files pending"}</span>
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
                            className="max-w-full rounded-md border border-border px-2 py-1 text-xs break-all text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
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
          );
        })}
      </AnimatePresence>
      {!tasks.length ? <EmptyState label="No tasks found" /> : null}
    </div>
  );
}

function DeleteFilteredDialog({
  open,
  totalCount,
  deletableCount,
  runningCount,
  busy,
  onOpenChange,
  onConfirm
}: {
  open: boolean;
  totalCount: number;
  deletableCount: number;
  runningCount: number;
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
          This removes generated files and job metadata for the matching tasks. {runningCount ? `${runningCount.toLocaleString()} in-progress ${pluralize("task", runningCount)} will be canceled first.` : "This cannot be undone."}
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

function ReplaceFilteredDialog({
  open,
  plan,
  busy,
  onOpenChange,
  onConfirm
}: {
  open: boolean;
  plan: TaskReplacePlan;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const actionCount = taskReplaceActionCount(plan);
  const skippedCount = plan.missingMediaTasks.length;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Retry filtered tasks</DialogTitle>
          <DialogDescription>
            {actionCount.toLocaleString()} of {plan.total.toLocaleString()} filtered task {pluralize("folder", plan.total)} will be deleted and queued again with the same task parameters.
          </DialogDescription>
        </DialogHeader>
        <div className="rounded-lg border border-amber-400/30 bg-amber-400/10 p-3 text-sm text-foreground">
          This removes generated files and job metadata before retrying tasks. {plan.runningTasks.length ? `${plan.runningTasks.length.toLocaleString()} in-progress ${pluralize("task", plan.runningTasks.length)} will be canceled first.` : skippedCount ? `${skippedCount.toLocaleString()} filtered ${pluralize("task", skippedCount)} will be skipped because ${taskReplaceSkipReason(plan)}.` : "Library media files are not touched."}
        </div>
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Keep tasks
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={busy || actionCount === 0}>
            {busy ? <Loader2 className="animate-spin" /> : <RotateCcw />}
            Retry {actionCount.toLocaleString()}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function TaskDialog({
  selection,
  config,
  busy,
  onOpenChange,
  onCreate,
  onDeleteExisting
}: {
  selection: TaskSelection | null;
  config?: PublicConfig;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (selection: TaskSelection, params: TaskParams, queueMode: QueueCreateMode) => void;
  onDeleteExisting: (selection: TaskSelection) => void;
}) {
  const [fast, setFast] = React.useState(false);
  const [enableEncode, setEnableEncode] = React.useState(true);
  const [enableSprites, setEnableSprites] = React.useState(true);
  const [requireSubtitles, setRequireSubtitles] = React.useState(true);
  const [extractStreams, setExtractStreams] = React.useState(true);
  const [copySubtitleSidecars, setCopySubtitleSidecars] = React.useState(true);
  const [encoders, setEncoders] = React.useState<string[]>(["av1"]);
  const [quality, setQuality] = React.useState(20);
  const [audioKbps, setAudioKbps] = React.useState(144);
  const [queueMode, setQueueMode] = React.useState<QueueMode>("replace");
  const [mediaFilter, setMediaFilter] = React.useState("");
  const [selectedMediaIds, setSelectedMediaIds] = React.useState<string[]>([]);
  const [collapsedMediaGroupKeys, setCollapsedMediaGroupKeys] = React.useState<string[]>([]);

  React.useEffect(() => {
    if (!config) return;
    setEnableEncode(config.enableEncode);
    setEnableSprites(config.enableSprite);
    setRequireSubtitles(true);
    setCopySubtitleSidecars(config.copySubtitleSidecars);
    setEncoders(config.encoders?.length ? config.encoders : ["av1"]);
    setQuality(Number(config.quality || 20));
    setAudioKbps(config.audioKbps || 144);
  }, [config, selection?.items[0]?.id]);

  React.useEffect(() => {
    setQueueMode("replace");
  }, [selection?.items[0]?.id]);

  const selectedTVEncoder = encoders.some(isTVEncoder);

  React.useEffect(() => {
    if (!selectedTVEncoder) return;
    setFast(false);
    setEnableEncode(true);
  }, [selectedTVEncoder]);

  const allEncoders = ["av1", "av1-tv", "hevc", "hevc-tv", "h264-10bit", "h264-10bit-tv", "h264-8bit", "h264-8bit-tv"];
  const isBulkSelection = Boolean(selection?.bulk);
  const effectiveQueueMode: QueueMode = selection?.bulk ? queueMode : "replace";
  const queueCreateMode: QueueCreateMode = effectiveQueueMode === "incomplete" ? "incomplete" : "replace";
  const actionItems = React.useMemo(() => (selection ? queueActionItems(selection, effectiveQueueMode) : []), [effectiveQueueMode, selection]);
  const actionItemIds = React.useMemo(() => actionItems.map((item) => item.id), [actionItems]);
  const actionItemIdsKey = actionItemIds.join("|");

  React.useEffect(() => {
    setSelectedMediaIds(actionItemIds);
    setMediaFilter("");
    setCollapsedMediaGroupKeys([]);
  }, [actionItemIdsKey, effectiveQueueMode]);

  const selectedMediaIdSet = React.useMemo(() => new Set(selectedMediaIds), [selectedMediaIds]);
  const selectedActionItems = React.useMemo(() => actionItems.filter((item) => selectedMediaIdSet.has(item.id)), [actionItems, selectedMediaIdSet]);
  const actionSelection = React.useMemo(() => {
    if (!selection) return null;
    return {
      ...selection,
      items: selection.bulk ? selectedActionItems : selection.items,
      existingTasks: existingTasksForMedia(selection.bulk ? selectedActionItems : selection.items, selection.existingTasks)
    };
  }, [selectedActionItems, selection]);
  const filteredActionItems = React.useMemo(() => filterDialogMediaItems(actionItems, mediaFilter), [actionItems, mediaFilter]);
  const actionGroups = React.useMemo(() => groupDialogMediaItems(filteredActionItems, selection), [filteredActionItems, selection]);
  const filteredActionIds = React.useMemo(() => filteredActionItems.map((item) => item.id), [filteredActionItems]);
  const filteredSelectedCount = filteredActionIds.filter((id) => selectedMediaIdSet.has(id)).length;
  const queuePlan = actionSelection ? buildQueuePlan(actionSelection.items, actionSelection.existingTasks, queueCreateMode) : emptyQueuePlan();
  const selectedExistingTasks = uniqueTasks(actionSelection?.existingTasks ?? []);
  const deleteActionItems = React.useMemo(() => (selection ? queueActionItems(selection, "delete") : []), [selection]);
  const deleteActionTasks = React.useMemo(() => existingTasksForMedia(deleteActionItems, selection?.existingTasks ?? []), [deleteActionItems, selection?.existingTasks]);
  const deleteActionTaskCount = deleteActionTasks.length;
  const selectedExistingTaskCount = selectedExistingTasks.length;
  const existingTaskCount = queuePlan.existingTasks.length;
  const queueCount = queuePlan.items.length;
  const hasActionSelection = !selection?.bulk || selectedActionItems.length > 0;
  const submit = () => {
    if (!selection) return;
    if (effectiveQueueMode === "delete") {
      if (actionSelection) onDeleteExisting(actionSelection);
      return;
    }
    if (!actionSelection) return;
    onCreate(actionSelection, {
      fast: selectedTVEncoder ? false : fast,
      enableEncode: selectedTVEncoder ? true : enableEncode,
      enableSprites,
      requireSubtitles,
      extractStreams,
      copySubtitleSidecars,
      encoders,
      quality: String(quality),
      audioKbps,
      videoExt: config?.videoExt ?? "mp4"
    }, queueCreateMode);
  };
  const actionDisabled = effectiveQueueMode === "delete"
    ? busy || !hasActionSelection || selectedExistingTaskCount === 0
    : busy || !hasActionSelection || queueCount === 0 || (!fast && !encoders.length);

  return (
    <Dialog open={!!selection} onOpenChange={onOpenChange}>
      <DialogContent
        className={cn(
          "flex h-[calc(100dvh-1rem)] max-h-[calc(100dvh-1rem)] flex-col overflow-hidden sm:max-h-[calc(100dvh-2rem)]",
          isBulkSelection
            ? "max-w-4xl sm:h-[calc(100dvh-2rem)]"
            : "max-w-xl sm:h-auto"
        )}
      >
        <DialogHeader className="min-w-0 pr-8">
          <DialogTitle>{selection?.title ?? "Transcode"}</DialogTitle>
          <DialogDescription>{selection?.description}</DialogDescription>
        </DialogHeader>
        <div className={cn("app-scrollbar grid min-h-0 min-w-0 flex-1 gap-4 overflow-x-hidden overflow-y-auto pr-1", !isBulkSelection && "content-start")}>
          {selection?.bulk ? (
            <div className="min-w-0">
              <div className="mb-2 text-sm font-medium">Queue mode</div>
              <div className="grid gap-2 sm:grid-cols-3">
                <Tip content="Delete matching task output and queue every selected episode again">
                  <Button type="button" className="min-w-0" variant={queueMode === "replace" ? "default" : "outline"} onClick={() => setQueueMode("replace")}>
                    <Trash2 />
                    Replace
                  </Button>
                </Tip>
                <Tip content="Skip media that already has a complete task and queue only missing or incomplete media">
                  <Button type="button" className="min-w-0" variant={queueMode === "incomplete" ? "default" : "outline"} onClick={() => setQueueMode("incomplete")}>
                    <ListVideo />
                    Incomplete or missing
                  </Button>
                </Tip>
                <Tip content="Delete selected media's existing task folders without queueing replacements. In-progress tasks will be canceled first. Library media files are not touched.">
                  <Button
                    type="button"
                    className="min-w-0"
                    variant={queueMode === "delete" ? "destructive" : "outline"}
                    onClick={() => setQueueMode("delete")}
                  >
                    <Trash2 />
                    Delete {deleteActionTaskCount.toLocaleString()}
                  </Button>
                </Tip>
              </div>
            </div>
          ) : null}
          {selection?.bulk ? (
            <div className="min-w-0 rounded-lg border bg-card/60 p-3">
              <div className="mb-3 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <ListVideo className="size-4 text-primary" />
                    <span className="text-sm font-medium">{queueActionLabel(effectiveQueueMode)}</span>
                    <Badge variant="outline">
                      {selectedActionItems.length.toLocaleString()} of {actionItems.length.toLocaleString()}
                    </Badge>
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">{queueActionHelp(effectiveQueueMode)}</div>
                </div>
                <div className="flex min-w-0 flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setSelectedMediaIds((current) => uniqueIDs([...current, ...filteredActionIds]))}
                    disabled={!filteredActionIds.length || filteredSelectedCount === filteredActionIds.length}
                  >
                    <BadgeCheck />
                    Select all filtered
                  </Button>
                  <Button type="button" variant="outline" size="sm" onClick={() => setSelectedMediaIds([])} disabled={!selectedMediaIds.length}>
                    <X />
                    Clear all selected
                  </Button>
                </div>
              </div>
              <div className="relative mb-3">
                <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input value={mediaFilter} onChange={(event) => setMediaFilter(event.target.value)} className="pl-9" placeholder="Filter selected media" />
              </div>
              <div className="app-scrollbar max-h-[46dvh] overflow-x-hidden overflow-y-auto rounded-lg border bg-background/30">
                {actionGroups.length ? (
                  actionGroups.map((group) => (
                    <div key={group.key} className="border-b last:border-b-0">
                      {(() => {
                        const groupIds = group.items.map((item) => item.id);
                        const groupSelectedCount = groupIds.filter((id) => selectedMediaIdSet.has(id)).length;
                        const groupFullySelected = group.items.length > 0 && groupSelectedCount === group.items.length;
                        const groupPartiallySelected = groupSelectedCount > 0 && !groupFullySelected;
                        const groupCollapsed = collapsedMediaGroupKeys.includes(group.key);
                        const toggleGroupSelection = () => {
                          setSelectedMediaIds((current) => {
                            const groupIdSet = new Set(groupIds);
                            return groupFullySelected ? current.filter((id) => !groupIdSet.has(id)) : uniqueIDs([...current, ...groupIds]);
                          });
                        };

                        return group.showHeader ? (
                          <div className="sticky top-0 z-10 flex min-w-0 items-center justify-between gap-3 border-b bg-background/95 px-3 py-2 backdrop-blur">
                            <div className="flex min-w-0 items-center gap-2">
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon"
                                className="size-7 shrink-0"
                                aria-label={`${groupCollapsed ? "Expand" : "Collapse"} ${group.label}`}
                                onClick={() => setCollapsedMediaGroupKeys((current) => toggleID(current, group.key))}
                              >
                                {groupCollapsed ? <ChevronRight /> : <ChevronDown />}
                              </Button>
                              <input
                                type="checkbox"
                                className="size-4 shrink-0 accent-primary"
                                aria-label={`${groupFullySelected ? "Deselect" : "Select"} ${group.label}`}
                                checked={groupFullySelected}
                                ref={(node) => {
                                  if (node) node.indeterminate = groupPartiallySelected;
                                }}
                                onChange={toggleGroupSelection}
                              />
                              <div className="min-w-0 truncate text-xs font-medium text-muted-foreground">{group.label}</div>
                            </div>
                            <Badge variant={groupSelectedCount ? "secondary" : "outline"}>
                              {groupSelectedCount.toLocaleString()} / {group.items.length.toLocaleString()}
                            </Badge>
                          </div>
                        ) : null;
                      })()}
                      <div className={cn("divide-y", collapsedMediaGroupKeys.includes(group.key) && "hidden")}>
                        {group.items.map((item) => {
                          const checked = selectedMediaIdSet.has(item.id);
                          const taskCount = tasksForReplacementMedia(item, selection.existingTasks).length;
                          const emptyTaskBadge = effectiveQueueMode === "replace" ? "No task" : "Missing";
                          return (
                            <label key={item.id} className="flex min-w-0 cursor-pointer items-start gap-3 px-3 py-2.5 transition-colors hover:bg-muted/50">
                              <input
                                type="checkbox"
                                className="mt-1 size-4 shrink-0 accent-primary"
                                checked={checked}
                                onChange={() => setSelectedMediaIds((current) => toggleID(current, item.id))}
                              />
                              <div className="min-w-0 flex-1">
                                <div className="flex min-w-0 flex-wrap items-center gap-2">
                                  <span className="min-w-0 truncate text-sm font-medium">{dialogMediaTitle(item)}</span>
                                  {taskCount ? <Badge variant="secondary">{taskCount} {pluralize("task", taskCount)}</Badge> : <Badge variant="outline">{emptyTaskBadge}</Badge>}
                                </div>
                                <div className="mt-1 truncate text-xs text-muted-foreground">{item.relPath || item.fileName}</div>
                              </div>
                            </label>
                          );
                        })}
                      </div>
                    </div>
                  ))
                ) : (
                  <div className="flex min-h-28 items-center justify-center px-3 text-sm text-muted-foreground">
                    {mediaFilter.trim() ? "No media matches the filter" : "No media for this action"}
                  </div>
                )}
              </div>
            </div>
          ) : null}
          <div className="grid gap-3 sm:grid-cols-2">
            <ToggleLine
              label="Fast path"
              checked={fast}
              onCheckedChange={setFast}
              disabled={selectedTVEncoder}
              tip={selectedTVEncoder ? "TV codec outputs require video encoding, so fast copy is disabled" : "Copy video and repackage audio through ffmpeg"}
            >
              <Gauge className="size-4 text-primary" />
            </ToggleLine>
            <ToggleLine
              label="Encode video"
              checked={enableEncode}
              onCheckedChange={setEnableEncode}
              disabled={selectedTVEncoder}
              tip={selectedTVEncoder ? "TV codec outputs require video encoding" : "Run HandBrake or ffmpeg output generation"}
            >
              <Video className="size-4 text-primary" />
            </ToggleLine>
            <ToggleLine label="Require subtitles" checked={requireSubtitles} onCheckedChange={setRequireSubtitles} tip="Fail the task if no subtitle tracks are found">
              <Subtitles className="size-4 text-primary" />
            </ToggleLine>
            <ToggleLine label="Extract streams" checked={extractStreams} onCheckedChange={setExtractStreams} tip="Extract subtitle, attachment, and audio streams">
              <Subtitles className="size-4 text-primary" />
            </ToggleLine>
            <ToggleLine label="Subtitle sidecars" checked={copySubtitleSidecars} onCheckedChange={setCopySubtitleSidecars} tip="Copy matching external subtitle files into the task folder">
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
                  <Tip content={isTVEncoder(encoder) ? `Toggle ${encoderLabel(encoder)} output with burned subtitles and 16:9 padding` : `Toggle ${encoderLabel(encoder)} output`} key={encoder}>
                    <Button
                      type="button"
                      variant={active ? "default" : "outline"}
                      size="sm"
                      onClick={() => setEncoders(active ? encoders.filter((value) => value !== encoder) : [...encoders, encoder])}
                      disabled={fast}
                    >
                      {encoderLabel(encoder)}
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
          {selection && (selection.bulk || existingTaskCount) ? (
            <div className={cn("rounded-lg border p-3 text-sm text-foreground", existingTaskCount || queuePlan.runningTasks.length || effectiveQueueMode === "delete" ? "border-destructive/30 bg-destructive/10" : "bg-card/60")}>
              <div className="mb-1 flex items-center gap-2 font-medium">
                {queueNoticeIcon(queuePlan, effectiveQueueMode)}
                {queueNoticeTitle(queuePlan, effectiveQueueMode)}
              </div>
              <p className="text-muted-foreground">{queueConfirmationText(actionSelection ?? selection, queuePlan, effectiveQueueMode)}</p>
            </div>
          ) : null}
        </div>
        <Separator />
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button className="w-full sm:w-auto" variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button className="w-full sm:w-auto" onClick={submit} disabled={actionDisabled} variant={effectiveQueueMode === "delete" ? "destructive" : "default"}>
            {busy ? <Loader2 className="animate-spin" /> : effectiveQueueMode === "delete" ? <Trash2 /> : <Play />}
            {effectiveQueueMode === "delete"
              ? `Delete ${selectedExistingTaskCount.toLocaleString()} ${pluralize("task", selectedExistingTaskCount)}`
              : `Queue ${queueCount === 1 ? "task" : `${queueCount.toLocaleString()} tasks`}`}
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
  disabled,
  tip,
  children
}: {
  label: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
  disabled?: boolean;
  tip: string;
  children: React.ReactNode;
}) {
  return (
    <Tip content={tip}>
      <label className={cn("flex items-center justify-between gap-3 rounded-lg border p-3", disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer")}>
        <span className="flex items-center gap-2 text-sm font-medium">
          {children}
          {label}
        </span>
        <Switch checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
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
      <CardContent className="flex min-w-0 items-start gap-3 p-4">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-primary">
          <Icon className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="break-words text-xl font-semibold leading-tight">{value}</div>
          <div className="break-words text-xs leading-snug text-muted-foreground">{detail}</div>
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

function countInProgressTasks(tasks: TranscodeTask[]) {
  return tasks.filter(isInProgressTask).length;
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

function buildCurrentMediaIndex(items: MediaItem[]) {
  const index: CurrentMediaIndex = new Map();
  for (const item of items) {
    for (const key of mediaKeys(item)) {
      if (!index.has(key)) {
        index.set(key, item);
      }
    }
  }
  return index;
}

function buildTaskDuplicateIndex(tasks: TranscodeTask[]) {
  const counts = new Map<string, number>();
  for (const task of tasks) {
    for (const key of duplicateTaskKeys(task)) {
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
  }
  return counts;
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

function mediaForTask(task: TranscodeTask, index: CurrentMediaIndex) {
  for (const key of taskMediaKeys(task)) {
    const item = index.get(key);
    if (item) return item;
  }
  return undefined;
}

function buildTaskReplacePlan(tasks: TranscodeTask[], index: CurrentMediaIndex): TaskReplacePlan {
  const plan: TaskReplacePlan = {
    candidates: [],
    runningCandidates: [],
    runningTasks: [],
    missingMediaTasks: [],
    total: 0
  };
  for (const task of uniqueTasks(tasks)) {
    plan.total += 1;
    const item = mediaForTask(task, index);
    if (!item) {
      plan.missingMediaTasks.push(task);
      continue;
    }
    if (!canDeleteTask(task)) {
      plan.runningTasks.push(task);
      plan.runningCandidates.push({ task, item });
      continue;
    }
    plan.candidates.push({ task, item });
  }
  return plan;
}

function canQueueReplaceTask(task: TranscodeTask, index: CurrentMediaIndex) {
  return canDeleteTask(task) && Boolean(mediaForTask(task, index));
}

function canQueueReplaceTaskAfterCancel(task: TranscodeTask, index: CurrentMediaIndex) {
  return Boolean(mediaForTask(task, index));
}

function taskReplaceButtonTip(task: TranscodeTask, index: CurrentMediaIndex, confirming: boolean) {
  if (!mediaForTask(task, index)) return "Current media is unavailable; run a scan before retrying";
  if (!canDeleteTask(task)) return confirming ? "Click once more to cancel this task, delete its folder, and retry it" : "Cancel this task, delete its folder, and queue it again";
  return confirming ? "Click once more to delete this task folder and retry it" : "Delete this task folder and queue it again with the same parameters";
}

function taskReplaceBlockedMessage(plan: TaskReplacePlan) {
  if (plan.missingMediaTasks.length && !plan.runningTasks.length) {
    return "No tasks retried. Current media could not be matched; run a scan first.";
  }
  return "No tasks retried. Matching tasks are either in progress or missing current media.";
}

function taskReplaceSkipReason(plan: TaskReplacePlan) {
  return "no current media match was found";
}

function taskReplaceActionCount(plan: TaskReplacePlan) {
  return plan.candidates.length + plan.runningCandidates.length;
}

function cloneTaskParams(params?: TaskParams): TaskParams {
  const source = params ?? { fast: false, extractStreams: true, copySubtitleSidecars: true };
  return {
    ...source,
    extractStreams: source.extractStreams ?? true,
    copySubtitleSidecars: source.copySubtitleSidecars ?? true,
    encoders: source.encoders ? [...source.encoders] : undefined
  };
}

function isTVEncoder(encoder: string) {
  return encoder.endsWith("-tv");
}

function encoderLabel(encoder: string) {
  return encoder.toUpperCase();
}

function originalMediaSize(task: TranscodeTask) {
  if (typeof task.oriSize === "number" && Number.isFinite(task.oriSize)) return task.oriSize;
  if (typeof task.media?.size === "number" && Number.isFinite(task.media.size)) return task.media.size;
  return undefined;
}

function newVersionInfoForMedia(item: MediaItem, task: TranscodeTask): NewVersionInfo | undefined {
  const originalSize = originalMediaSize(task);
  if (typeof originalSize !== "number" || item.size === originalSize) return undefined;
  return {
    originalSize,
    currentSize: item.size
  };
}

function newVersionInfoForTask(task: TranscodeTask, index: CurrentMediaIndex) {
  const item = mediaForTask(task, index);
  return item ? newVersionInfoForMedia(item, task) : undefined;
}

function taskHasNewVersion(task: TranscodeTask, index: CurrentMediaIndex) {
  return Boolean(newVersionInfoForTask(task, index));
}

function formatVersionBytes(bytes: number) {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  if (unit === 0) return `${bytes} B`;
  return `${value.toFixed(2)} ${units[unit]}`;
}

function canDeleteTask(task: TranscodeTask) {
  return !isInProgressTask(task);
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

function filterTasks(
  tasks: TranscodeTask[],
  query: string,
  taskStatusFilters: TriStateFilters,
  codecFilters: TriStateFilters,
  subtitleFilters: TriStateFilters,
  currentMediaIndex: CurrentMediaIndex,
  taskDuplicateIndex: TaskDuplicateIndex
) {
  return tasks.filter((task) => {
    if (!taskMatchesQuery(task, query)) return false;
    if (!taskMatchesStatusFilters(task, taskStatusFilters, currentMediaIndex, taskDuplicateIndex)) return false;
    const codecs = taskCodecs(task);
    if (!taskMatchesCodecFilters(codecs, codecFilters)) return false;
    const subtitles = taskSubtitleLanguages(task);
    if (!taskMatchesSubtitleFilters(subtitles, subtitleFilters)) return false;
    return true;
  });
}

function isInProgressTask(task: TranscodeTask) {
  return task.state === "queued" || Boolean(task.running);
}

function hasTriStateFilters(filters: TriStateFilters) {
  return Object.keys(filters).length > 0;
}

function triStateFiltersByState(filters: TriStateFilters, state: TriStateFilterState) {
  return Object.entries(filters)
    .filter(([, value]) => value === state)
    .map(([option]) => option);
}

function taskMatchesStatusFilters(
  task: TranscodeTask,
  filters: TriStateFilters,
  currentMediaIndex: CurrentMediaIndex,
  taskDuplicateIndex: TaskDuplicateIndex
) {
  const included = triStateFiltersByState(filters, "include");
  const excluded = triStateFiltersByState(filters, "exclude");
  const inProgress = isInProgressTask(task);
  const completed = task.state === "complete";
  const newVersion = taskHasNewVersion(task, currentMediaIndex);
  const hasStoryboards = taskHasStoryboards(task);
  const hasDuplicate = taskHasDuplicateJob(task, taskDuplicateIndex);
  if (included.includes(IN_PROGRESS_FILTER) && !inProgress) return false;
  if (excluded.includes(IN_PROGRESS_FILTER) && inProgress) return false;
  if (included.includes(COMPLETED_FILTER) && !completed) return false;
  if (excluded.includes(COMPLETED_FILTER) && completed) return false;
  if (included.includes(NEW_VERSION_FILTER) && !newVersion) return false;
  if (excluded.includes(NEW_VERSION_FILTER) && newVersion) return false;
  if (included.includes(STORYBOARDS_FILTER) && !hasStoryboards) return false;
  if (excluded.includes(STORYBOARDS_FILTER) && hasStoryboards) return false;
  if (included.includes(DUPLICATE_FILTER) && !hasDuplicate) return false;
  if (excluded.includes(DUPLICATE_FILTER) && hasDuplicate) return false;
  return true;
}

function taskHasStoryboards(task: TranscodeTask) {
  return Boolean(task.files?.["storyboard.vtt"]);
}

function taskHasDuplicateJob(task: TranscodeTask, index: TaskDuplicateIndex) {
  for (const key of duplicateTaskKeys(task)) {
    if ((index.get(key) ?? 0) > 1) return true;
  }
  return false;
}

function taskMatchesCodecFilters(codecs: string[], filters: TriStateFilters) {
  const included = triStateFiltersByState(filters, "include");
  const excluded = triStateFiltersByState(filters, "exclude");
  if (included.length && !included.some((codec) => codecs.includes(codec))) return false;
  if (excluded.some((codec) => codecs.includes(codec))) return false;
  return true;
}

function taskMatchesSubtitleFilters(subtitles: string[], filters: TriStateFilters) {
  const included = triStateFiltersByState(filters, "include");
  const excluded = triStateFiltersByState(filters, "exclude");
  if (included.length && !included.some((language) => subtitles.includes(language))) return false;
  if (excluded.some((language) => subtitles.includes(language))) return false;
  return true;
}

function nextTriStateFilters(filters: TriStateFilters, option: string) {
  const current = filters[option];
  const next = { ...filters };
  if (!current) {
    next[option] = "include";
  } else if (current === "include") {
    next[option] = "exclude";
  } else {
    delete next[option];
  }
  return next;
}

function triStateFilterTip(option: string, state?: TriStateFilterState) {
  if (state === "include") return `${option} included. Click to exclude it.`;
  if (state === "exclude") return `${option} excluded. Click to make it neutral.`;
  return `${option} neutral. Click to include it.`;
}

function taskMatchesQuery(task: TranscodeTask, query: string) {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (!terms.length) return true;
  const haystack = [
    task.id,
    task.mediaId,
    task.input,
    task.inputPath,
    task.inputRelPath,
    task.inputParent,
    task.outputDir,
    task.state,
    task.error,
    task.encodedCodecs?.join(" "),
    taskSubtitleLanguages(task).join(" "),
    task.streams?.map((stream) => `${stream.codecType ?? ""} ${stream.codecName ?? ""} ${stream.language ?? ""} ${stream.location ?? ""}`).join(" "),
    task.files ? Object.keys(task.files).join(" ") : ""
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return terms.every((term) => haystack.includes(term));
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

function taskPhaseLabel(state: string) {
  switch (state) {
    case "queued":
      return "Queued";
    case "running":
      return "Starting";
    case "incomplete":
      return "Preparing";
    case "streams_extracted":
      return "Transcoding";
    case "complete":
      return "Complete";
    case "failed":
      return "Failed";
    case "canceled":
      return "Canceled";
    default:
      return state || "Unknown";
  }
}

function activeTaskSummary(tasks: TranscodeTask[]) {
  const names = tasks.map(activeTaskName).filter(Boolean);
  if (!names.length) return `${tasks.length.toLocaleString()} active`;
  if (names.length === 1) return names[0];
  return `${names[0]} + ${names.length - 1}`;
}

function activeTaskName(task: TranscodeTask) {
  return task.input || task.inputRelPath || task.inputPath || task.id;
}

function scanDetail(scan?: ScanStatus) {
  if (!scan) return "Never";
  if (scan.running) {
    const visited = `${(scan.dirsScanned ?? 0).toLocaleString()} dirs / ${(scan.filesScanned ?? 0).toLocaleString()} files`;
    return scan.currentPath ? `${visited} / ${scan.currentPath}` : visited;
  }
  return formatDate(scan.lastFinishedAt);
}

function mergeTaskResponse(response: TaskListResponse, current: TranscodeTask[]) {
  const taskStatus = normalizeTaskStatus(response.status);
  let tasks = mergeTaskDetails(response.tasks, current);
  for (const activeTask of taskStatus.activeTasks ?? []) {
    tasks = upsertTask(tasks, activeTask);
  }
  return { tasks, taskStatus };
}

function mergeTaskDetails(next: TranscodeTask[], current: TranscodeTask[]) {
  const detailed = new Map(current.filter((task) => task.files || task.streams).map((task) => [task.id, task]));
  return next.map((task) => {
    const existing = detailed.get(task.id);
    const normalized = normalizeTaskUpdate(task);
    if (!existing) return normalized;
    if (!shouldPreserveTaskDetails(normalized, existing)) return normalized;
    return {
      ...normalized,
      files: normalized.files ?? existing.files,
      streams: normalized.streams ?? existing.streams,
      subtitleLanguages: normalized.subtitleLanguages ?? existing.subtitleLanguages
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
  return found ? updated : [normalizeTaskUpdate(next), ...tasks];
}

function normalizeTaskUpdate(task: TranscodeTask): TranscodeTask {
  return {
    ...task,
    running: Boolean(task.running),
    error: task.error,
    startedAt: task.startedAt,
    finishedAt: task.finishedAt,
    encodedCodecs: task.encodedCodecs,
    subtitleLanguages: task.subtitleLanguages,
    streams: task.streams,
    duration: task.duration,
    width: task.width,
    height: task.height,
    files: task.files
  };
}

function normalizeTaskStatus(status: TaskStatus): TaskStatus {
  return {
    ...status,
    activeTasks: status.activeTasks?.map(normalizeTaskUpdate)
  };
}

function shouldPreserveTaskDetails(next: TranscodeTask, existing: TranscodeTask) {
  return next.updatedAt === existing.updatedAt && next.state === existing.state;
}

function updateTaskStatusTask(status: TaskStatus | undefined, task: TranscodeTask) {
  if (!status) return status;
  const normalized = normalizeTaskUpdate(task);
  if (!status.activeTasks?.length) {
    return normalized.running ? { ...status, activeTasks: [normalized] } : status;
  }
  if (!normalized.running) {
    return removeActiveTask(status, normalized.id);
  }
  return {
    ...status,
    activeTasks: upsertTask(status.activeTasks, normalized)
  };
}

function removeActiveTask(status: TaskStatus | undefined, id: string) {
  if (!status?.activeTasks?.length) return status;
  return {
    ...status,
    activeTasks: status.activeTasks.filter((task) => task.id !== id)
  };
}

function removeActiveTasks(status: TaskStatus | undefined, ids: Set<string>) {
  if (!status?.activeTasks?.length) return status;
  return {
    ...status,
    activeTasks: status.activeTasks.filter((task) => !ids.has(task.id))
  };
}

function uniqueMediaItems(items: MediaItem[]) {
  const seen = new Set<string>();
  const unique: MediaItem[] = [];
  for (const item of items) {
    if (seen.has(item.id)) continue;
    seen.add(item.id);
    unique.push(item);
  }
  return unique;
}

function uniqueTasks(tasks: TranscodeTask[]) {
  const seen = new Set<string>();
  const unique: TranscodeTask[] = [];
  for (const task of tasks) {
    if (seen.has(task.id)) continue;
    seen.add(task.id);
    unique.push(task);
  }
  return unique;
}

function latestTasksForIds(tasks: TranscodeTask[], latestTasks: TranscodeTask[]) {
  const latestByID = new Map(latestTasks.map((task) => [task.id, task]));
  return uniqueTasks(tasks).map((task) => latestByID.get(task.id) ?? task);
}

function uniqueIDs(ids: string[]) {
  return Array.from(new Set(ids));
}

function delay(ms: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, ms));
}

function toggleID(ids: string[], id: string) {
  return ids.includes(id) ? ids.filter((value) => value !== id) : [...ids, id];
}

function emptyQueuePlan(): QueuePlan {
  return {
    items: [],
    existingTasks: [],
    runningTasks: [],
    skippedCompleteItems: [],
    incompleteItems: [],
    nonExistingItems: []
  };
}

function buildQueuePlan(items: MediaItem[], tasks: TranscodeTask[], queueMode: QueueCreateMode): QueuePlan {
  const plan = emptyQueuePlan();
  const selectedItems = uniqueMediaItems(items);
  for (const item of selectedItems) {
    const itemTasks = tasksForReplacementMedia(item, tasks);
    const hasCompleteTask = itemTasks.some(isCompleteTask);
    if (queueMode === "incomplete" && hasCompleteTask) {
      plan.skippedCompleteItems.push(item);
      continue;
    }

    plan.items.push(item);
    if (itemTasks.length) {
      plan.incompleteItems.push(item);
      plan.existingTasks.push(...itemTasks);
    } else {
      plan.nonExistingItems.push(item);
    }
  }
  plan.existingTasks = uniqueTasks(plan.existingTasks);
  plan.runningTasks = plan.existingTasks.filter((task) => !canDeleteTask(task));
  return plan;
}

function queueActionItems(selection: TaskSelection, queueMode: QueueMode) {
  if (queueMode === "delete") {
    return mediaItemsWithExistingTasks(selection.items, selection.existingTasks);
  }
  if (queueMode === "incomplete") {
    return buildQueuePlan(selection.items, selection.existingTasks, "incomplete").items;
  }
  return uniqueMediaItems(selection.items);
}

function mediaItemsWithExistingTasks(items: MediaItem[], tasks: TranscodeTask[]) {
  return uniqueMediaItems(items).filter((item) => tasksForReplacementMedia(item, tasks).length > 0);
}

function existingTasksForMedia(items: MediaItem[], tasks: TranscodeTask[]) {
  return uniqueTasks(uniqueMediaItems(items).flatMap((item) => tasksForReplacementMedia(item, tasks)));
}

function tasksForReplacementMedia(item: MediaItem, tasks: TranscodeTask[]) {
  const selectedKeys = new Set(replacementMediaKeys(item));
  return uniqueTasks(tasks.filter((task) => replacementTaskKeys(task).some((key) => selectedKeys.has(key))));
}

function asLibrarySort(value: string): LibrarySort {
  return value === "recent" ? "recent" : "title";
}

function librarySortLabel(sort: LibrarySort) {
  switch (sort) {
    case "recent":
      return "Recently added";
    default:
      return "Title";
  }
}

function sortLibraryItems(items: MediaItem[], sort: LibrarySort) {
  const next = [...items];
  if (sort === "recent") {
    return next.sort(compareMediaRecentlyAdded);
  }
  return next.sort(compareMediaTitle);
}

function limitLibraryItems(items: MediaItem[], limit: number, sort: LibrarySort) {
  if (sort !== "recent") return items.slice(0, limit);

  const visible: MediaItem[] = [];
  const visibleEpisodeShows = new Set<string>();
  const allEpisodesByShow = items.reduce((groups, item) => {
    if (item.kind !== "episode") return groups;
    const show = episodeShowName(item);
    if (!groups.has(show)) groups.set(show, []);
    groups.get(show)!.push(item);
    return groups;
  }, new Map<string, MediaItem[]>());

  for (const item of items) {
    if (visible.length >= limit) break;

    if (item.kind !== "episode") {
      visible.push(item);
      continue;
    }

    const show = episodeShowName(item);
    if (visibleEpisodeShows.has(show)) continue;
    visibleEpisodeShows.add(show);
    visible.push(...(allEpisodesByShow.get(show) ?? [item]));
  }

  return sortLibraryItems(visible, sort);
}

function compareMediaTitle(a: MediaItem, b: MediaItem) {
  return a.sortKey.localeCompare(b.sortKey) || a.fileName.localeCompare(b.fileName);
}

function compareMediaRecentlyAdded(a: MediaItem, b: MediaItem) {
  return mediaModTime(b) - mediaModTime(a) || compareMediaTitle(a, b);
}

function mediaModTime(item: MediaItem) {
  const value = Date.parse(item.modTime);
  return Number.isFinite(value) ? value : 0;
}

function newestMediaModTime(items: MediaItem[]) {
  return items.reduce((newest, item) => Math.max(newest, mediaModTime(item)), 0);
}

function newestShowModTime(seasons: Map<number, MediaItem[]>) {
  return Array.from(seasons.values()).reduce((newest, items) => Math.max(newest, newestMediaModTime(items)), 0);
}

function isCompleteTask(task: TranscodeTask) {
  return task.state === "complete";
}

function replacementMediaKeys(item: MediaItem) {
  const keys = [taskKey("media", item.id), taskKey("path", item.path), taskKey("rel", item.relPath)];
  if (item.kind === "episode" && item.season != null && item.episode != null) {
    keys.push(episodeKey(item.show || item.title, item.season, item.episode));
  }
  return uniqueKeys(keys);
}

function replacementTaskKeys(task: TranscodeTask) {
  const parentPath = task.inputParent && task.input ? `${task.inputParent}/${task.input}` : "";
  const taskFile = task.input || fileNameFromPath(task.inputPath) || fileNameFromPath(task.inputRelPath);
  const keys = [taskKey("media", task.mediaId), taskKey("path", task.inputPath), taskKey("path", parentPath), taskKey("rel", task.inputRelPath)];
  const episode = parseEpisodeIdentity(taskFile);
  if (episode) {
    keys.push(episodeKey(episode.show, episode.season, episode.episode));
  }
  return uniqueKeys(keys);
}

function duplicateTaskKeys(task: TranscodeTask) {
  const taskFile = task.input || fileNameFromPath(task.inputPath) || fileNameFromPath(task.inputRelPath);
  const keys = [...replacementTaskKeys(task), mediaDuplicateTaskKey(task.media), fileDuplicateTaskKey(task, taskFile)];
  return uniqueKeys(keys);
}

function mediaDuplicateTaskKey(item?: MediaItem) {
  if (!item) return "";
  if (item.kind === "episode" && item.season != null && item.episode != null) {
    return episodeKey(item.show || showFromEpisodeFile(item.fileName) || item.title, item.season, item.episode);
  }
  if (item.kind === "movie") {
    const title = movieDuplicateTitleKey(item.title || item.fileName);
    return title ? `movie:${title}` : "";
  }
  return "";
}

function fileDuplicateTaskKey(task: TranscodeTask, file?: string) {
  if (!file) return "";
  const base = stripMediaExtension(fileNameFromPath(file) || file);
  const episode = parseEpisodeIdentity(base);
  if (episode) return episodeKey(episode.show, episode.season, episode.episode);
  const title = movieDuplicateTitleKey(movieTitleCandidate(task, base));
  return title ? `movie:${title}` : "";
}

function showFromEpisodeFile(value?: string) {
  const base = stripMediaExtension(fileNameFromPath(value) || value);
  const match = base.match(/^(.*?)\s*[- ]*\s*S\d{1,2}E\d{1,3}\b/i);
  return match ? match[1].trim().replace(/[-._\s]+$/g, "") : "";
}

function movieTitleCandidate(task: TranscodeTask, base: string) {
  const candidates = [fileNameFromPath(task.inputParent), parentNameFromPath(task.inputRelPath), base];
  for (const candidate of candidates) {
    const trimmed = candidate.trim();
    if (!trimmed || /S\d{1,2}E\d{1,3}\b/i.test(trimmed)) continue;
    if (/\b(?:480|576|720|1080|2160|4320)p\b/i.test(trimmed) && !/\b(?:19|20)\d{2}\b/.test(trimmed)) continue;
    return trimmed;
  }
  return base;
}

function parentNameFromPath(value?: string) {
  const normalized = value?.replace(/\\/g, "/") ?? "";
  const parts = normalized.split("/").filter(Boolean);
  return parts.length >= 2 ? parts[parts.length - 2] : "";
}

function movieDuplicateTitleKey(title: string) {
  let normalized = normalizeTitle(title);
  if (!normalized) return "";
  const yearMatch = normalized.match(/^(.*\b(?:19|20)\d{2}\b)/);
  if (yearMatch) normalized = yearMatch[1].trim();
  const fields = normalized.split(/\s+/).filter(Boolean);
  while (fields.length && duplicateMovieSuffix(fields[fields.length - 1])) {
    fields.pop();
  }
  return fields.join(" ");
}

function duplicateMovieSuffix(value: string) {
  return (
    [
      "remux",
      "proper",
      "repack",
      "extended",
      "unrated",
      "theatrical",
      "directors",
      "director",
      "cut",
      "bluray",
      "blu",
      "ray",
      "bdrip",
      "webdl",
      "web",
      "webrip",
      "hdtv",
      "hdr",
      "dv",
      "uhd"
    ].includes(value) || /^(?:480|576|720|1080|2160|4320)p?$/.test(value)
  );
}

function itemsForShow(items: MediaItem[], showName: string) {
  return sortMediaItems(items.filter((item) => episodeShowName(item) === showName));
}

function itemsForSeason(items: MediaItem[], seasonNumber: number) {
  return sortMediaItems(items.filter((item) => (item.season || 0) === seasonNumber));
}

function filterDialogMediaItems(items: MediaItem[], query: string) {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (!terms.length) return sortMediaItems(items);
  return sortMediaItems(items).filter((item) => {
    const haystack = [
      item.title,
      item.show,
      item.episodeTitle,
      item.fileName,
      item.relPath,
      item.library,
      item.season ? `season ${item.season}` : "",
      item.episode ? `episode ${item.episode}` : ""
    ].filter(Boolean).join(" ").toLowerCase();
    return terms.every((term) => haystack.includes(term));
  });
}

function groupDialogMediaItems(items: MediaItem[], selection: TaskSelection | null): DialogMediaGroup[] {
  if (!selection || !shouldGroupDialogBySeason(selection)) {
    return items.length ? [{ key: "media", label: "Media", items, showHeader: false }] : [];
  }
  return Array.from(
    items.reduce((groups, item) => {
      const season = item.season || 0;
      if (!groups.has(season)) groups.set(season, []);
      groups.get(season)!.push(item);
      return groups;
    }, new Map<number, MediaItem[]>())
  )
    .sort(([a], [b]) => a - b)
    .map(([season, seasonItems]) => ({
      key: `season:${season}`,
      label: season ? `Season ${season}` : "Specials",
      items: sortMediaItems(seasonItems),
      showHeader: true
    }));
}

function shouldGroupDialogBySeason(selection: TaskSelection) {
  if (!selection.bulk || !selection.items.every((item) => item.kind === "episode")) return false;
  return new Set(selection.items.map((item) => item.season || 0)).size > 1;
}

function sortMediaItems(items: MediaItem[]) {
  return [...items].sort(
    (a, b) =>
      (a.season || 0) - (b.season || 0) ||
      (a.episode || 0) - (b.episode || 0) ||
      a.sortKey.localeCompare(b.sortKey) ||
      a.fileName.localeCompare(b.fileName)
  );
}

function episodeShowName(item: MediaItem) {
  return item.show || "Unknown Show";
}

function selectionCountDescription(count: number, singular: string) {
  const label = count === 1 ? singular : `${singular}s`;
  return `${count.toLocaleString()} ${label} selected`;
}

function showSelectionDescription(showName: string, items: MediaItem[]) {
  const seasonCount = new Set(items.map((item) => item.season || 0)).size;
  const seasonLabel = seasonCount === 1 ? "season" : "seasons";
  return `${selectionCountDescription(items.length, "episode")} from ${showName} across ${seasonCount.toLocaleString()} ${seasonLabel}`;
}

function queueActionLabel(queueMode: QueueMode) {
  switch (queueMode) {
    case "delete":
      return "Task folders to delete";
    case "incomplete":
      return "Incomplete or missing media";
    default:
      return "Media to replace";
  }
}

function queueActionHelp(queueMode: QueueMode) {
  switch (queueMode) {
    case "delete":
      return "Only media with existing task output is listed.";
    case "incomplete":
      return "Complete media is skipped before selection.";
    default:
      return "Selected media will be queued after matching task output is removed.";
  }
}

function dialogMediaTitle(item: MediaItem) {
  if (item.kind === "episode") {
    const season = item.season ? `S${String(item.season).padStart(2, "0")}` : "";
    const episode = item.episode ? `E${String(item.episode).padStart(2, "0")}` : "";
    const prefix = `${season}${episode}`;
    return [prefix, item.episodeTitle || item.title || item.fileName].filter(Boolean).join(" / ");
  }
  return item.title || item.fileName;
}

function queueNoticeIcon(plan: QueuePlan, queueMode: QueueMode) {
  if (plan.runningTasks.length) return <CircleAlert className="size-4 text-destructive" />;
  if (queueMode === "delete" && plan.existingTasks.length) return <Trash2 className="size-4 text-destructive" />;
  if (queueMode === "delete") return <CircleAlert className="size-4 text-destructive" />;
  if (plan.existingTasks.length) return <Trash2 className="size-4 text-destructive" />;
  if (queueMode === "incomplete" && plan.skippedCompleteItems.length) return <CheckCircle2 className="size-4 text-primary" />;
  return <ListVideo className="size-4 text-primary" />;
}

function queueNoticeTitle(plan: QueuePlan, queueMode: QueueMode) {
  if (plan.runningTasks.length) return "In-progress tasks will be canceled first";
  if (queueMode === "delete" && plan.existingTasks.length) return "Existing task output will be removed";
  if (queueMode === "delete") return "No existing tasks to delete";
  if (plan.existingTasks.length) return "Existing task output will be removed";
  if (queueMode === "incomplete" && plan.skippedCompleteItems.length) return "Completed media will be skipped";
  return "Ready to queue";
}

function queueConfirmationText(selection: TaskSelection, plan: QueuePlan, queueMode: QueueMode) {
  if (plan.runningTasks.length) {
    const action = queueMode === "delete" ? "deleted" : "deleted before replacements are queued";
    return `${plan.runningTasks.length.toLocaleString()} in-progress existing ${pluralize("task", plan.runningTasks.length)} will be canceled, then ${action}.`;
  }
  if (queueMode === "delete") {
    if (plan.existingTasks.length) {
      return `${plan.existingTasks.length.toLocaleString()} existing task ${pluralize("folder", plan.existingTasks.length)} for selected ${selectionMediaLabel(selection.items)} will be deleted from the output directory. Library media files will not be deleted.`;
    }
    return `No existing task output matched the selected ${selectionMediaLabel(selection.items)}. Library media files will not be deleted.`;
  }
  if (!selection.bulk) {
    return "This media file is already in a task. Queueing it will delete the existing task output and job metadata before creating the replacement.";
  }
  if (queueMode === "replace") {
    if (plan.existingTasks.length) {
      return `${plan.existingTasks.length.toLocaleString()} existing task ${pluralize("folder", plan.existingTasks.length)} for selected ${selectionMediaLabel(selection.items)} will be deleted. All ${plan.items.length.toLocaleString()} selected ${selectionMediaLabel(selection.items, plan.items.length)} will be queued as replacements.`;
    }
    return `All ${plan.items.length.toLocaleString()} selected ${selectionMediaLabel(selection.items, plan.items.length)} will be queued. No existing task output matched this selection.`;
  }
  if (!plan.items.length) {
    return `All ${selection.items.length.toLocaleString()} selected ${selectionMediaLabel(selection.items)} already have complete tasks, so nothing will be queued.`;
  }

  const parts = [
    `${plan.items.length.toLocaleString()} of ${selection.items.length.toLocaleString()} selected ${selectionMediaLabel(selection.items)} will be queued.`
  ];
  if (plan.skippedCompleteItems.length) {
    parts.push(`${plan.skippedCompleteItems.length.toLocaleString()} complete ${selectionMediaLabel(selection.items, plan.skippedCompleteItems.length)} will be skipped.`);
  }
  if (plan.existingTasks.length) {
    parts.push(`${plan.existingTasks.length.toLocaleString()} existing incomplete task ${pluralize("folder", plan.existingTasks.length)} will be deleted before replacements are queued.`);
  }
  if (plan.nonExistingItems.length) {
    parts.push(`${plan.nonExistingItems.length.toLocaleString()} ${selectionMediaLabel(selection.items, plan.nonExistingItems.length)} without tasks will be queued.`);
  }
  return parts.join(" ");
}

function pluralize(value: string, count: number) {
  return count === 1 ? value : `${value}s`;
}

function selectionMediaLabel(items: MediaItem[], count = items.length) {
  if (items.every((item) => item.kind === "episode")) return count === 1 ? "episode" : "episodes";
  if (items.every((item) => item.kind === "movie")) return count === 1 ? "movie" : "movies";
  return count === 1 ? "media file" : "media files";
}

function groupEpisodes(items: MediaItem[], sort: LibrarySort = "title") {
  const shows = new Map<string, Map<number, MediaItem[]>>();
  for (const item of items) {
    const show = episodeShowName(item);
    const season = item.season || 0;
    if (!shows.has(show)) shows.set(show, new Map());
    const seasons = shows.get(show)!;
    if (!seasons.has(season)) seasons.set(season, []);
    seasons.get(season)!.push(item);
  }
  return Array.from(shows.entries())
    .sort(([aName, aSeasons], [bName, bSeasons]) =>
      sort === "recent"
        ? newestShowModTime(bSeasons) - newestShowModTime(aSeasons) || aName.localeCompare(bName)
        : aName.localeCompare(bName)
    )
    .map(([name, seasons]) => ({
      name,
      seasons: Array.from(seasons.entries())
        .sort(([aNumber], [bNumber]) => aNumber - bNumber)
        .map(([number, seasonItems]) => ({
          number,
          items: sortMediaItems(seasonItems)
        }))
    }));
}
