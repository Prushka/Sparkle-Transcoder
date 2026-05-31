const API_BASE = "/api";
const STATIC_BASE = "";

export function apiBase() {
  return API_BASE;
}

export function staticBase() {
  return STATIC_BASE;
}

export type RelatedFile = {
  kind: string;
  path: string;
  relPath: string;
  name: string;
  ext: string;
  size: number;
  modTime: string;
};

export type MediaItem = {
  id: string;
  kind: "movie" | "episode" | "unknown";
  library: string;
  title: string;
  show?: string;
  season?: number;
  episode?: number;
  episodeTitle?: string;
  year?: string;
  path: string;
  relPath: string;
  parent: string;
  fileName: string;
  ext: string;
  size: number;
  modTime: string;
  poster?: RelatedFile;
  fanart?: RelatedFile;
  subtitles?: RelatedFile[];
  nfo?: RelatedFile[];
  attachments?: RelatedFile[];
  sortKey: string;
};

export type ScanStatus = {
  running: boolean;
  incremental: boolean;
  lastStartedAt?: string;
  lastFinishedAt?: string;
  error?: string;
  items: number;
  changed: number;
  currentPath?: string;
  dirsScanned: number;
  filesScanned: number;
};

export type PublicConfig = {
  mediaRoot: string;
  output: string;
  incrementalScan: boolean;
  scanInterval: string;
  encoders: string[];
  videoExt: string;
  quality: string;
  audioKbps: number;
  enableEncode: boolean;
  enableSprite: boolean;
};

export type ToolReadiness = {
  id: string;
  name: string;
  command: string;
  path?: string;
  ready: boolean;
  version?: string;
  error?: string;
  required: boolean;
};

export type TaskParams = {
  fast: boolean;
  enableEncode?: boolean;
  enableSprites?: boolean;
  encoders?: string[];
  videoExt?: string;
  quality?: string;
  audioKbps?: number;
  extractStreams: boolean;
};

export type TranscodeTask = {
  id: string;
  mediaId?: string;
  inputPath: string;
  inputRelPath?: string;
  inputParent?: string;
  input: string;
  outputDir: string;
  state: string;
  running?: boolean;
  error?: string;
  params: TaskParams;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  finishedAt?: string;
  encodedCodecs?: string[];
  subtitleLanguages?: string[];
  streams?: { codecType?: string; codecName?: string; location?: string; language?: string; index: number }[];
  duration?: number;
  width?: number;
  height?: number;
  files?: Record<string, number>;
  oriSize?: number;
  oriModTime?: number;
  legacy?: boolean;
  media?: MediaItem;
};

export type TaskStatus = {
  refreshing: boolean;
  refreshedAt?: string;
  error?: string;
  activeTasks?: TranscodeTask[];
};

export type TaskListResponse = {
  tasks: TranscodeTask[];
  count: number;
  status: TaskStatus;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      "content-type": "application/json",
      ...(init?.headers ?? {})
    },
    cache: "no-store"
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || response.statusText);
  }
  return response.json() as Promise<T>;
}

export const api = {
  config: () => request<PublicConfig>("/config"),
  scanStatus: () => request<ScanStatus>("/scan"),
  startScan: (force: boolean) => request<ScanStatus>("/scan", { method: "POST", body: JSON.stringify({ force }) }),
  media: () => request<{ items: MediaItem[]; count: number; status: ScanStatus }>("/media"),
  tasks: () => request<TaskListResponse>("/tasks"),
  refreshTasks: () => request<TaskListResponse>("/tasks/refresh", { method: "POST", body: "{}" }),
  task: (id: string) => request<TranscodeTask>(`/tasks/${id}`),
  createTask: (mediaId: string, params: TaskParams) =>
    request<TranscodeTask>("/tasks", { method: "POST", body: JSON.stringify({ mediaId, params }) }),
  cancelTask: (id: string) => request<TranscodeTask>(`/tasks/${id}/cancel`, { method: "POST", body: "{}" }),
  retryTask: (id: string) => request<TranscodeTask>(`/tasks/${id}/retry`, { method: "POST", body: "{}" }),
  tools: () => request<{ tools: ToolReadiness[] }>("/tools"),
  deleteTask: (id: string) => request<{ deleted: number }>(`/tasks/${id}`, { method: "DELETE" }),
  deleteTasks: (ids: string[]) =>
    request<{ requested: number; deleted: number; failures: { id: string; error: string }[] }>("/tasks/delete", {
      method: "POST",
      body: JSON.stringify({ ids })
    })
};

export function posterUrl(item: MediaItem) {
  return item.poster ? `${apiBase()}/media/${item.id}/poster` : "";
}

export function outputUrl(task: TranscodeTask, file: string) {
  return `${staticBase()}/output/${encodeURIComponent(task.id)}/${encodeURIComponent(file)}`;
}
