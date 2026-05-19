// Renderer entry. Demonstrates the typed client end-to-end:
// schema.ts (generated from Go) → openapi-fetch → calls Go backend.
import { getClient } from "../api/client";

const $ = <T extends HTMLElement = HTMLElement>(id: string) =>
  document.getElementById(id) as T;

async function main() {
  const handshake = await window.relay.handshake();
  $("handshake").textContent = JSON.stringify(
    { ...handshake, token: `${handshake.token.slice(0, 8)}…` },
    null,
    2,
  );

  const client = await getClient();

  const { data: health, error: healthErr } = await client.GET("/health");
  $("health").textContent = healthErr
    ? `error: ${JSON.stringify(healthErr)}`
    : JSON.stringify(health, null, 2);

  const nameInput = $<HTMLInputElement>("greet-name");
  const out = $("greet-out");
  $("greet-btn").addEventListener("click", async () => {
    const { data, error } = await client.GET("/greet/{name}", {
      params: { path: { name: nameInput.value } },
    });
    out.textContent = error
      ? `error: ${JSON.stringify(error)}`
      : JSON.stringify(data, null, 2);
  });
}

main().catch((err) => {
  document.body.prepend(
    Object.assign(document.createElement("pre"), {
      textContent: `startup failed: ${String(err)}`,
      style: "color: crimson; padding: 1rem;",
    }),
  );
});
