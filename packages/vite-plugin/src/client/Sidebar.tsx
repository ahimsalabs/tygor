import { Show, For, createSignal, createEffect, onCleanup, onMount, createResource } from "solid-js";
import type { GetStatusResponse } from "../devserver/types";
import type { TygorRpcError, DiscoverySchema, TypeRef, TypeDescriptor } from "./types";
import type { SidebarSide } from "./DevTools";
import { Pane, type DropPosition } from "./Pane";

declare const __TYGOR_VERSION__: string;

// Check if a type reference is "empty" (tygor.Empty, Any, or Empty primitive)
function isEmptyType(ref: TypeRef | undefined): boolean {
  if (!ref) return true;
  if (ref.kind === "primitive") {
    const pk = ref.primitiveKind?.toLowerCase();
    return pk === "any" || pk === "empty";
  }
  if (ref.kind === "reference" && ref.name === "Empty") return true;
  return false;
}

// Format a type reference for display (simplified: elide nullability and empty)
function formatTypeRef(ref: TypeRef | undefined): string {
  if (!ref || isEmptyType(ref)) return "";
  switch (ref.kind) {
    case "reference": return ref.name || "";
    case "primitive": return ref.primitiveKind?.toLowerCase() || "";
    case "array": {
      const el = formatTypeRef(ref.element);
      return el ? `${el}[]` : "";
    }
    case "ptr": return formatTypeRef(ref.element); // unwrap nullable
    default: return "";
  }
}

// Format request parameters as field names
function formatRequestParams(ref: TypeRef | undefined, types: TypeDescriptor[] | undefined): string {
  if (!ref || isEmptyType(ref) || ref.kind !== "reference" || !types) return "";
  const typeDef = types.find(t => t.Name.name === ref.name);
  if (!typeDef?.Fields?.length) return "";
  const fields = typeDef.Fields;
  if (fields.length <= 3) {
    return fields.map(f => f.jsonName).join(", ");
  }
  return fields.slice(0, 2).map(f => f.jsonName).join(", ") + ", ...";
}

// Fetch discovery schema from tygor plugin endpoint
const fetchDiscovery = async (): Promise<DiscoverySchema | null> => {
  try {
    const res = await fetch("/__tygor/discovery");
    if (!res.ok) return null;
    const data = await res.json();
    // Unwrap RPC response format: { result: { discovery: {...} } }
    return data.result?.discovery ?? data;
  } catch {
    return null;
  }
};

interface DevToolsState {
  status: GetStatusResponse | null;
  rpcError: TygorRpcError | null;
  disconnectedSince: number | null;
  errorSince: number | null;
}

interface SidebarProps {
  state: DevToolsState;
  side: SidebarSide;
  docked: boolean;
  onCollapse: () => void;
  onToggleDocked: () => void;
  onDismissRpcError: () => void;
}

const PHASE_LABELS: Record<string, string> = {
  prebuild: "Prebuild",
  build: "Build",
  runtime: "Runtime",
};

function formatDuration(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.floor(minutes / 60)}h`;
}

interface Rawr { color: string; weight: string; size: number; text: string }
let _r: Rawr[] = [];
let _i = 0;

export function Sidebar(props: SidebarProps) {
  const [copied, setCopied] = createSignal(false);
  const [expandedServices, setExpandedServices] = createSignal<Record<string, boolean>>({});

  // Track when server becomes ready to refetch discovery schema
  const [schemaVersion, setSchemaVersion] = createSignal(0);
  let wasReloading = false;
  createEffect(() => {
    const status = props.state.status?.status;
    if (wasReloading && status === "ok") {
      // Server just finished reloading - refetch schema
      setSchemaVersion(v => v + 1);
    }
    wasReloading = status === "reloading" || status === "starting";
  });

  // Refetch discovery when schemaVersion changes
  const [discovery] = createResource(schemaVersion, fetchDiscovery);

  const toggleService = (name: string) => {
    setExpandedServices(prev => ({ ...prev, [name]: !prev[name] }));
  };

  // Tick for duration updates
  const [tick, setTick] = createSignal(0);
  const interval = setInterval(() => setTick((t) => t + 1), 1000);
  onCleanup(() => clearInterval(interval));

  // Pane state management
  const STORAGE_KEY_COLLAPSED = "tygor-panes-collapsed";
  const STORAGE_KEY_ORDER = "tygor-panes-order";
  const DEFAULT_ORDER = ["system", "services"];

  const loadPaneState = () => {
    try {
      return {
        collapsed: JSON.parse(sessionStorage.getItem(STORAGE_KEY_COLLAPSED) || "{}") as Record<string, boolean>,
        order: JSON.parse(sessionStorage.getItem(STORAGE_KEY_ORDER) || "null") as string[] | null,
      };
    } catch {
      return { collapsed: {}, order: null };
    }
  };

  const initial = loadPaneState();
  // Merge stored order with DEFAULT_ORDER to include any new panes
  const mergedOrder = initial.order
    ? [...initial.order, ...DEFAULT_ORDER.filter((id) => !initial.order!.includes(id))]
    : DEFAULT_ORDER;
  const [paneCollapsed, setPaneCollapsed] = createSignal<Record<string, boolean>>(initial.collapsed);
  const [paneOrder, setPaneOrder] = createSignal<string[]>(mergedOrder);
  const [draggedPane, setDraggedPane] = createSignal<string | null>(null);
  let dragStartOrder: string[] | null = null;

  // Persist pane state to sessionStorage
  createEffect(() => {
    sessionStorage.setItem(STORAGE_KEY_COLLAPSED, JSON.stringify(paneCollapsed()));
    sessionStorage.setItem(STORAGE_KEY_ORDER, JSON.stringify(paneOrder()));
  });

  const togglePane = (id: string) => {
    setPaneCollapsed((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  const handleDragStart = (paneId: string) => {
    dragStartOrder = [...paneOrder()];
    setDraggedPane(paneId);
  };

  const movePane = (targetId: string, position: DropPosition) => {
    const fromId = draggedPane();
    if (!fromId || fromId === targetId) return;

    const order = [...paneOrder()];
    const fromIndex = order.indexOf(fromId);

    // Special case: dropping at the start
    if (targetId === "__start__") {
      if (fromIndex === 0) return;
      order.splice(fromIndex, 1);
      order.unshift(fromId);
    } else if (targetId === "__end__") {
      // Special case: dropping at the end
      if (fromIndex === order.length - 1) return;
      order.splice(fromIndex, 1);
      order.push(fromId);
    } else {
      const toIndex = order.indexOf(targetId);
      if (fromIndex === -1 || toIndex === -1) return;

      // Calculate insert position based on drop position indicator
      let insertIndex: number;
      if (position === "before") {
        insertIndex = fromIndex < toIndex ? toIndex - 1 : toIndex;
      } else {
        insertIndex = fromIndex < toIndex ? toIndex : toIndex + 1;
      }
      // Skip if already in position
      if (insertIndex === fromIndex) return;

      order.splice(fromIndex, 1);
      order.splice(insertIndex, 0, fromId);
    }

    setPaneOrder(order);
  };

  const handleDragEnd = (e: DragEvent) => {
    // dropEffect is "none" when dropped outside valid targets (cancelled)
    if (e.dataTransfer?.dropEffect === "none" && dragStartOrder) {
      setPaneOrder(dragStartOrder);
    }
    dragStartOrder = null;
    setDraggedPane(null);
  };

  // Vite status: connected unless vite_disconnected
  const viteStatus = (): { label: string; state: "ok" | "disconnected" } => {
    const s = props.state.status;
    if (!s || s.status === "vite_disconnected") {
      return { label: "Disconnected", state: "disconnected" };
    }
    return { label: "✓", state: "ok" };
  };

  // API status: derived from the status
  const apiStatus = (): { label: string; state: "ok" | "error" | "building" | "disconnected" } => {
    const s = props.state.status;
    if (!s) return { label: "Connecting...", state: "disconnected" };

    switch (s.status) {
      case "ok":
        return { label: "✓", state: "ok" };
      case "error":
        return { label: `${PHASE_LABELS[s.phase] || "Build"} Error`, state: "error" };
      case "reloading":
        return { label: "Reloading...", state: "building" };
      case "starting":
        return { label: "Starting...", state: "building" };
      case "disconnected":
        return { label: "Disconnected", state: "disconnected" };
      case "vite_disconnected":
        return { label: "—", state: "disconnected" };
    }
  };

  const duration = () => {
    tick();
    const timestamp = props.state.errorSince ?? props.state.disconnectedSince;
    if (!timestamp) return null;
    return formatDuration(Date.now() - timestamp);
  };

  const errorStatus = () => {
    const s = props.state.status;
    return s?.status === "error" ? s : null;
  };

  // Auto-expand system pane and move to top on mount if there's an error
  onMount(() => {
    if (errorStatus()) {
      // Expand system pane
      setPaneCollapsed((prev) => ({ ...prev, system: false }));
      // Move system to top
      const order = paneOrder();
      const systemIndex = order.indexOf("system");
      if (systemIndex > 0) {
        const newOrder = ["system", ...order.filter((id) => id !== "system")];
        setPaneOrder(newOrder);
      }
    }
  });

  const copyError = async () => {
    const err = errorStatus();
    if (!err) return;
    const text = (err.command ? `${err.cwd}$ ${err.command}\n\n` : "") + err.error.trim();
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const onTigerClick = () => {
    const s = props.state.status;
    if (s?.status === "ok" && s.rawrData?.length && !_r.length) {
      for (const b of s.rawrData) {
        try {
          const [color, weight, size, text] = atob(b).split("|");
          _r.push({ color, weight, size: parseInt(size, 10), text });
        } catch {}
      }
    }
    if (!_r.length) return;
    const m = _r[_i];
    const style = `color:${m.color};font-weight:${m.weight};font-size:${m.size}px`;
    console.log(`%c🐯 ${m.text}`, style);
    _i = (_i + 1) % _r.length;
  };

  return (
    <div class="tygor-sidebar" classList={{
      "tygor-sidebar--left": props.side === "left",
      "tygor-sidebar--docked": props.docked
    }}>
      {/* Header */}
      <div class="tygor-sidebar-header">
        <div class="tygor-sidebar-title">
          <span class="tygor-sidebar-icon" onClick={onTigerClick}>🐯</span>
          <span class="tygor-sidebar-name">tygor dev tools</span>
          <span class="tygor-sidebar-version">v{__TYGOR_VERSION__}</span>
        </div>
        <div class="tygor-sidebar-header-actions">
          <button
            class="tygor-sidebar-dock-btn"
            classList={{ "tygor-sidebar-dock-btn--active": props.docked }}
            onClick={props.onToggleDocked}
            title={props.docked ? "Undock (overlay mode)" : "Dock (shift page content)"}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="18" height="18" rx="2" />
              <line x1="15" y1="3" x2="15" y2="21" />
            </svg>
          </button>
          <button class="tygor-sidebar-collapse" onClick={props.onCollapse} title="Collapse">
            {props.side === "right" ? "→" : "←"}
          </button>
        </div>
      </div>

      {/* Panes */}
      <div class="tygor-sidebar-panes" ondragend={handleDragEnd}>
        {/* Drop zone for moving to start of list */}
        <Show when={draggedPane()}>
          <div
            class="tygor-pane-drop-end"
            ondragover={(e) => {
              e.preventDefault();
              e.stopPropagation();
              movePane("__start__", "before");
            }}
          />
        </Show>
        <For each={paneOrder()}>
          {(paneId) => {
            if (paneId === "system") {
              const systemCollapsedStatus = () => {
                const vite = viteStatus();
                const api = apiStatus();
                if (vite.state !== "ok") return vite.label;
                if (api.state !== "ok") return api.label;
                return "OK";
              };

              return (
                <Pane
                  id="system"
                  title="System Status"
                  collapsed={paneCollapsed().system ?? false}
                  collapsedStatus={systemCollapsedStatus}
                  onToggle={() => togglePane("system")}
                  onDragStart={() => handleDragStart("system")}
                  onDragOver={(pos) => movePane("system", pos)}
                  isDragging={draggedPane() === "system"}
                >
                  <div class="tygor-sidebar-status-grid">
                    <div class="tygor-sidebar-status-item">
                      <span class="tygor-sidebar-status-name">Vite</span>
                      <span class={`tygor-sidebar-status-value tygor-sidebar-status-value--${viteStatus().state}`}>
                        {viteStatus().label}
                      </span>
                      <Show when={duration() && viteStatus().state === "disconnected"}>
                        <span class="tygor-sidebar-status-duration">{duration()}</span>
                      </Show>
                    </div>
                    <div class="tygor-sidebar-status-item">
                      <span class="tygor-sidebar-status-name">Go</span>
                      <span class={`tygor-sidebar-status-value tygor-sidebar-status-value--${apiStatus().state}`}>
                        {apiStatus().label}
                      </span>
                      <Show when={duration() && viteStatus().state === "ok" && apiStatus().state !== "ok"}>
                        <span class="tygor-sidebar-status-duration">{duration()}</span>
                      </Show>
                      <Show when={props.state.status?.status === "ok" && props.state.status?.port}>
                        <span class="tygor-sidebar-status-port">port {props.state.status?.port}</span>
                      </Show>
                    </div>
                  </div>
                  <Show when={errorStatus()}>
                    {(err) => (
                      <div class="tygor-sidebar-error">
                        <div class="tygor-sidebar-error-header">
                          <span class="tygor-sidebar-error-title">Error Output</span>
                          <button
                            class="tygor-sidebar-error-copy"
                            classList={{ "tygor-sidebar-error-copy--copied": copied() }}
                            onClick={copyError}
                          >
                            {copied() ? "Copied" : "Copy"}
                          </button>
                        </div>
                        <Show when={err().command}>
                          <p class="tygor-sidebar-error-cmd">$ {err().command}</p>
                        </Show>
                        <pre class="tygor-sidebar-error-output">{err().error}</pre>
                        <p class="tygor-sidebar-error-hint">Fix the error and save — auto-reloads when fixed.</p>
                      </div>
                    )}
                  </Show>
                </Pane>
              );
            }

            if (paneId === "services") {
              const services = () => discovery()?.Services ?? [];

              return (
                <Pane
                  id="services"
                  title="Services"
                  collapsed={paneCollapsed().services ?? false}
                  collapsedStatus={() => {
                    const count = services().length;
                    return count > 0 ? `${count}` : null;
                  }}
                  onToggle={() => togglePane("services")}
                  onDragStart={() => handleDragStart("services")}
                  onDragOver={(pos) => movePane("services", pos)}
                  isDragging={draggedPane() === "services"}
                >
                  <Show when={services().length > 0} fallback={
                    <div class="tygor-pane-empty">{discovery.loading ? "Loading..." : "No services available"}</div>
                  }>
                    <ul class="tygor-sidebar-services">
                      <For each={services()}>
                        {(svc) => (
                          <li class="tygor-sidebar-service">
                            <div
                              class="tygor-sidebar-service-header"
                              onClick={() => toggleService(svc.name)}
                            >
                              <span class="tygor-sidebar-service-toggle">
                                {expandedServices()[svc.name] ? "▼" : "▶"}
                              </span>
                              <span class="tygor-sidebar-service-name">{svc.name}</span>
                              <span class="tygor-sidebar-service-count">({svc.endpoints.length})</span>
                            </div>
                            <Show when={expandedServices()[svc.name]}>
                              <ul class="tygor-sidebar-endpoints">
                                <For each={svc.endpoints}>
                                  {(ep) => {
                                    const primitive = ep.primitive || "exec";
                                    const label = {
                                      query: "Query",
                                      exec: "Exec",
                                      stream: "Stream",
                                      atom: "Atom",
                                    }[primitive] || primitive;
                                    const params = formatRequestParams(ep.request, discovery()?.Types);
                                    const res = formatTypeRef(ep.response);
                                    return (
                                      <li class="tygor-sidebar-endpoint">
                                        <div class="tygor-sidebar-endpoint-left">
                                          <span class={`tygor-sidebar-endpoint-verb tygor-sidebar-endpoint-verb--${primitive}`}>
                                            {label}
                                          </span>
                                          <span class="tygor-sidebar-endpoint-sig">
                                            {ep.name}({params})
                                          </span>
                                        </div>
                                        {res && <span class="tygor-sidebar-endpoint-res"><span class="tygor-sidebar-endpoint-arrow">→</span> {res}</span>}
                                      </li>
                                    );
                                  }}
                                </For>
                              </ul>
                            </Show>
                          </li>
                        )}
                      </For>
                    </ul>
                  </Show>
                </Pane>
              );
            }

            return null;
          }}
        </For>
        {/* Drop zone for moving to end of list */}
        <Show when={draggedPane()}>
          <div
            class="tygor-pane-drop-end"
            ondragover={(e) => {
              e.preventDefault();
              movePane("__end__", "after");
            }}
          />
        </Show>
      </div>

      {/* RPC Error */}
      <Show when={props.state.rpcError}>
        {(rpcErr) => (
          <div class="tygor-sidebar-rpc-error">
            <div class="tygor-sidebar-error-header">
              <span class="tygor-sidebar-error-title">RPC Error</span>
              <button class="tygor-sidebar-error-dismiss" onClick={props.onDismissRpcError}>×</button>
            </div>
            <div class="tygor-sidebar-rpc-endpoint">
              {rpcErr().service}.{rpcErr().method}
            </div>
            <div class="tygor-sidebar-rpc-details">
              <span class="tygor-sidebar-rpc-code">{rpcErr().code}</span>
              <span class="tygor-sidebar-rpc-message">{rpcErr().message}</span>
            </div>
          </div>
        )}
      </Show>

    </div>
  );
}
