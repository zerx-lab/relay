import { contextBridge, ipcRenderer } from "electron";

// Surface a minimal, typed API to the renderer. Keep this surface small;
// every method exposed here is a privilege escalation channel.
const api = {
  /** Fetch the backend handshake (baseUrl + auth token) from the main process. */
  handshake: () =>
    ipcRenderer.invoke("relay:handshake") as Promise<{
      port: number;
      token: string;
      baseUrl: string;
    }>,
};

contextBridge.exposeInMainWorld("relay", api);

// Re-exported as a type for the renderer to import.
export type RelayBridge = typeof api;
