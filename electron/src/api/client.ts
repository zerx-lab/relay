// Hand-written wrapper around openapi-fetch that:
//   1. Pulls baseUrl + token from the preload bridge.
//   2. Injects the auth header on every request.
//   3. Exposes a single typed `client` for the rest of the renderer.
//
// Do NOT hand-write any request/response types here. Always import them from
// the generated `./schema`.

import createClient from "openapi-fetch";
import type { paths } from "./schema";

declare global {
  interface Window {
    relay: {
      handshake: () => Promise<{
        port: number;
        token: string;
        baseUrl: string;
      }>;
    };
  }
}

const AUTH_HEADER = "X-Relay-Token";

let cachedClient: ReturnType<typeof createClient<paths>> | null = null;

/**
 * Lazily create the API client on first use. Resolving the handshake is
 * async because the main process spawns the Go backend during app startup;
 * if the renderer loads before the backend is ready, the IPC call waits.
 */
export async function getClient() {
  if (cachedClient) return cachedClient;
  const { baseUrl, token } = await window.relay.handshake();
  cachedClient = createClient<paths>({
    baseUrl,
    headers: { [AUTH_HEADER]: token },
  });
  return cachedClient;
}

// Re-export the schema types for callers that need them directly.
export type { paths, components, operations } from "./schema";
