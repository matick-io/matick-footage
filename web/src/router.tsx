import { useCallback } from "react";
import {
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  useNavigate,
  useRouterState,
} from "@tanstack/react-router";
import { Activity, Clapperboard, Film, ListVideo, Search, type LucideIcon } from "lucide-react";
import { AppLayout, AppSidebar, AppSidebarItem, useSidebarExpanded } from "@matick-io/ui-shell";
import { useQuery } from "@tanstack/react-query";
import { api, type SearchParams } from "./api";
import { formatVersion } from "./format";
import { SearchPage } from "./routes/SearchPage";
import { RecordingsPage } from "./routes/RecordingsPage";
import { DropZone } from "./components/upload/DropZone";
import { UploadButton } from "./components/upload/UploadButton";
import { useUploads } from "./components/upload/useUploads";
import { RecordingPage } from "./routes/RecordingPage";
import { SelectsPage } from "./routes/SelectsPage";
import { JobsPage } from "./routes/JobsPage";

interface NavEntry {
  to: "/" | "/recordings" | "/selects" | "/jobs";
  label: string;
  icon: LucideIcon;
}

const nav: NavEntry[] = [
  { to: "/", label: "Search", icon: Search },
  { to: "/recordings", label: "Recordings", icon: Film },
  { to: "/selects", label: "Selects", icon: ListVideo },
  { to: "/jobs", label: "Jobs", icon: Activity },
];

function current(pathname: string): NavEntry | undefined {
  return nav.find((n) => (n.to === "/" ? pathname === "/" : pathname.startsWith(n.to)));
}

function Root() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const navigate = useNavigate();
  const expanded = useSidebarExpanded();
  const here = current(pathname);
  // Uploads run from here, so leaving the page doesn't stop one; only the
  // Recordings page offers them.
  const upload = useUploads();
  const onFiles = useCallback((files: File[]) => void upload(files), [upload]);
  const uploads = pathname === "/recordings";
  const config = useQuery({ queryKey: ["config"], queryFn: api.config });
  const version = config.data ? formatVersion(config.data.version, config.data.commit) : "";
  return (
    <AppLayout
      chrome="sidebar"
      sidebarHeader={
        <span className="flex items-center gap-2 text-sm font-medium">
          <Clapperboard className="h-4 w-4 shrink-0" />
          {expanded ? "Footage" : null}
        </span>
      }
      sidebar={
        <AppSidebar>
          {nav.map(({ to, label, icon: Icon }) => (
            <AppSidebarItem
              key={to}
              ariaLabel={label}
              selected={here?.to === to}
              onClick={() => void navigate({ to })}
            >
              <Icon className="h-4 w-4" />
            </AppSidebarItem>
          ))}
          {version ? (
            <li
              title={`Footage ${version}`}
              className={`mt-auto truncate pb-1 text-[10px] text-text-muted ${expanded ? "px-2.5" : "text-center"}`}
            >
              {/* Collapsed, only the release or branch fits. */}
              {expanded ? version : version.split(" · ")[0]}
            </li>
          ) : null}
        </AppSidebar>
      }
      titleBar={<span className="text-sm font-medium">{here?.label}</span>}
      titleBarEnd={uploads ? <UploadButton onFiles={onFiles} /> : null}
    >
      <Outlet />
      {uploads ? (
        <DropZone
          onFiles={onFiles}
          title="Drop to upload to the library"
          detail="Videos, each with its .events.jsonl sidecar if it has one."
        />
      ) : null}
    </AppLayout>
  );
}

function list(v: unknown): string[] | undefined {
  if (Array.isArray(v)) {
    const out = v.filter((x): x is string => typeof x === "string" && x !== "");
    return out.length ? out : undefined;
  }
  return typeof v === "string" && v ? [v] : undefined;
}

const rootRoute = createRootRoute({ component: Root });

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  validateSearch: (s: Record<string, unknown>): SearchParams => ({
    q: typeof s.q === "string" && s.q ? s.q : undefined,
    kind: list(s.kind),
    tag: list(s.tag),
    map: list(s.map),
    build: list(s.build),
    tester: list(s.tester),
    importance: typeof s.importance === "number" && s.importance > 0 ? s.importance : undefined,
  }),
  component: SearchPage,
});

const recordingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/recordings",
  component: RecordingsPage,
});

export interface RecordingSearch {
  in?: number;
  out?: number;
}

const recordingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/recordings/$id",
  validateSearch: (s: Record<string, unknown>): RecordingSearch => ({
    in: typeof s.in === "number" ? s.in : undefined,
    out: typeof s.out === "number" ? s.out : undefined,
  }),
  component: RecordingPage,
});

export interface SelectsSearch {
  id?: number;
}

const selectsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/selects",
  validateSearch: (s: Record<string, unknown>): SelectsSearch => ({
    id: typeof s.id === "number" ? s.id : undefined,
  }),
  component: SelectsPage,
});

const jobsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/jobs", component: JobsPage });

export const router = createRouter({
  routeTree: rootRoute.addChildren([searchRoute, recordingsRoute, recordingRoute, selectsRoute, jobsRoute]),
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
