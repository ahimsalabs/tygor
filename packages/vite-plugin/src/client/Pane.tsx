import { Show, JSX } from "solid-js";

export type DropPosition = "before" | "after";

export interface PaneProps {
  id: string;
  title: string;
  collapsed: boolean;
  collapsedStatus?: () => string | JSX.Element | null;
  onToggle: () => void;
  // Drag-and-drop
  onDragStart: () => void;
  onDragOver: (position: DropPosition) => void;
  isDragging: boolean;
  children: JSX.Element;
}

export function Pane(props: PaneProps) {
  return (
    <div
      class="tygor-pane"
      classList={{
        "tygor-pane--collapsed": props.collapsed,
        "tygor-pane--dragging": props.isDragging,
      }}
      ondragover={(e) => {
        e.preventDefault();
        const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
        const midpoint = rect.top + rect.height / 2;
        const position: DropPosition = e.clientY < midpoint ? "before" : "after";
        props.onDragOver(position);
      }}
    >
      <div
        class="tygor-pane-header"
        draggable={true}
        ondragstart={(e) => {
          e.dataTransfer!.effectAllowed = "move";
          props.onDragStart();
        }}
        onClick={props.onToggle}
      >
        <span class="tygor-pane-chevron">{props.collapsed ? "▸" : "▾"}</span>
        <span class="tygor-pane-title">{props.title}</span>
        <Show when={props.collapsed && props.collapsedStatus}>
          <span class="tygor-pane-status">{props.collapsedStatus!()}</span>
        </Show>
      </div>
      <Show when={!props.collapsed}>
        <div class="tygor-pane-content">
          {props.children}
        </div>
      </Show>
    </div>
  );
}
