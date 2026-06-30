import {
  Activity,
  Camera,
  Clapperboard,
  LayoutDashboard,
  ListFilter,
  MonitorPlay,
  RefreshCw,
  Settings,
} from "lucide-react";
import { NavLink, Outlet, useLocation } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { useCameras, useHealth, useStreamStatus } from "../app/queries";
import { Button } from "../components/ui/button";
import { StatusDot } from "../components/StatusDot";
import { cn, formatDate } from "../lib/utils";

const navItems = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/live", label: "Live", icon: MonitorPlay },
  { to: "/cameras", label: "Cameras", icon: Camera },
  { to: "/recordings", label: "Recordings", icon: Clapperboard },
  { to: "/logs", label: "Logs", icon: ListFilter },
  { to: "/system", label: "System", icon: Settings },
];

const titles: Record<string, string> = {
  "/": "Dashboard",
  "/live": "Live",
  "/cameras": "Cameras",
  "/recordings": "Recordings",
  "/logs": "Logs",
  "/system": "System",
};

export function ConsoleLayout() {
  const location = useLocation();
  const queryClient = useQueryClient();
  const health = useHealth();
  const cameras = useCameras();
  const streams = useStreamStatus();
  const online = cameras.data?.filter((camera) => camera.state === "streaming").length ?? 0;
  const title = titles[location.pathname] ?? "CamStation";

  return (
    <div className="min-h-svh bg-slate-950 text-slate-100">
      <aside className="fixed inset-y-0 left-0 z-20 hidden w-64 border-r border-slate-800 bg-slate-950 lg:block">
        <div className="flex h-16 items-center gap-3 border-b border-slate-800 px-5">
          <div className="flex size-9 items-center justify-center rounded-md bg-sky-500 text-slate-950">
            <Activity size={20} />
          </div>
          <div>
            <div className="text-sm font-semibold">CamStation</div>
            <div className="text-xs text-slate-500">2.0 console</div>
          </div>
        </div>
        <nav className="space-y-1 px-3 py-4">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === "/"}
              className={({ isActive }) =>
                cn(
                  "flex h-10 items-center gap-3 rounded-md px-3 text-sm text-slate-400 transition hover:bg-slate-900 hover:text-slate-100",
                  isActive && "bg-slate-900 text-sky-200",
                )
              }
            >
              <item.icon size={18} />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>

      <div className="lg:pl-64">
        <header className="sticky top-0 z-10 border-b border-slate-800 bg-slate-950/90 backdrop-blur">
          <div className="flex min-h-16 flex-wrap items-center justify-between gap-3 px-4 py-3 lg:px-6">
            <div>
              <h1 className="text-xl font-semibold tracking-normal">{title}</h1>
              <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-slate-500">
                <span className="inline-flex items-center gap-1.5">
                  <StatusDot status={health.data?.ok ? "ok" : "unknown"} />
                  API {health.data?.mode ?? "checking"}
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <StatusDot status={streams.data?.running ? "running" : "offline"} />
                  go2rtc {streams.data?.running ? "running" : "stopped"}
                </span>
                <span>{online} streaming</span>
                <span>{formatDate(new Date().toISOString())}</span>
              </div>
            </div>
            <Button
              type="button"
              variant="secondary"
              onClick={() => queryClient.invalidateQueries()}
            >
              <RefreshCw size={16} />
              Refresh
            </Button>
          </div>
          <nav className="flex gap-1 overflow-x-auto border-t border-slate-900 px-3 py-2 lg:hidden">
            {navItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.to === "/"}
                className={({ isActive }) =>
                  cn(
                    "inline-flex h-9 shrink-0 items-center gap-2 rounded-md px-3 text-sm text-slate-400",
                    isActive && "bg-slate-900 text-sky-200",
                  )
                }
              >
                <item.icon size={16} />
                {item.label}
              </NavLink>
            ))}
          </nav>
        </header>
        <main className="px-4 py-5 lg:px-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

