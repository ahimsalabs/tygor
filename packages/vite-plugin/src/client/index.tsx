import { render } from "solid-js/web";
import { DevTools } from "./DevTools";
import styles from "./styles.css";

// Mount the devtools in Shadow DOM for style isolation
const host = document.createElement("div");
host.id = "tygor-devtools";
document.body.appendChild(host);

const shadow = host.attachShadow({ mode: "open" });

// Inject styles into shadow root
const styleEl = document.createElement("style");
styleEl.textContent = styles;
shadow.appendChild(styleEl);

// Render into shadow root
const container = document.createElement("div");
shadow.appendChild(container);
render(() => <DevTools />, container);
