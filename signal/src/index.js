// Servidor de señalización de EmuHub.
//
// Cada sala es un Durable Object con nombre igual a su clave (un hash del
// secreto del código de sala, así que el servidor no puede suplantar a nadie:
// los mensajes van firmados con el secreto). El anfitrión mantiene un
// WebSocket abierto mientras la sala existe; cada invitado abre otro, envía su
// oferta y recibe la respuesta del anfitrión. El servidor solo reenvía.
//
// Se usa la API de hibernación: con los WebSockets abiertos pero en silencio,
// el objeto no consume tiempo de CPU ni de duración.

import { DurableObject } from "cloudflare:workers";

const MAX_MESSAGE = 16 * 1024;
const MAX_GUESTS = 16;
const ROOM_KEY = /^[0-9a-f]{32}$/;
const INVITE = /^[A-Za-z0-9_-]{10,600}$/;
const DOWNLOAD = "https://github.com/omarmendozaaa/emuhub/releases/latest";

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    // Enlace de sala: la invitación entera va en la dirección, el servidor no
    // guarda nada. No pide la clave: la página solo abre EmuHub.
    const invite = url.pathname.match(/^\/j\/([^/]+)\/?$/);
    if (invite) {
      return invitePage(invite[1]);
    }
    const match = url.pathname.match(/^\/v1\/rooms\/([^/]+)$/);
    if (!match) {
      return new Response("EmuHub signaling server\n", { status: url.pathname === "/" ? 200 : 404 });
    }
    const key = match[1];
    if (!ROOM_KEY.test(key)) {
      return new Response("bad room\n", { status: 400 });
    }
    if (request.headers.get("Upgrade") !== "websocket") {
      return new Response("expected websocket\n", { status: 426 });
    }
    const role = url.searchParams.get("role");
    if (role !== "host" && role !== "guest") {
      return new Response("bad role\n", { status: 400 });
    }
    // Con la clave puesta (wrangler secret put ACCESS_KEY), solo crea salas
    // quien la lleve: https://…/?key=<clave>. Los invitados no la necesitan:
    // solo pueden hablar con una sala que tenga anfitrión, y para encontrarla
    // hace falta su nombre, un hash del secreto de la invitación.
    if (role === "host" && env.ACCESS_KEY && !sameKey(url.searchParams.get("key") || "", env.ACCESS_KEY)) {
      return new Response("forbidden\n", { status: 403 });
    }
    const room = env.ROOMS.get(env.ROOMS.idFromName(key));
    return room.fetch(request);
  },
};

export class Room extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    // Responde a los "ping" de mantenimiento sin despertar al objeto.
    ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair("ping", "pong"));
  }

  async fetch(request) {
    const role = new URL(request.url).searchParams.get("role");
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);

    if (role === "host") {
      // Una reconexión del anfitrión sustituye a la anterior.
      for (const old of this.ctx.getWebSockets("host")) {
        safeClose(old, 4000, "replaced");
      }
      this.ctx.acceptWebSocket(server, ["host"]);
    } else {
      const id = crypto.randomUUID();
      this.ctx.acceptWebSocket(server, ["guest", "g:" + id]);
      server.serializeAttachment({ id });
      if (this.ctx.getWebSockets("host").length === 0) {
        server.send(JSON.stringify({ type: "error", reason: "nohost" }));
        safeClose(server, 4004, "no host");
      } else if (this.ctx.getWebSockets("guest").length > MAX_GUESTS) {
        server.send(JSON.stringify({ type: "error", reason: "full" }));
        safeClose(server, 4029, "full");
      }
    }
    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws, message) {
    if (typeof message !== "string" || message.length > MAX_MESSAGE) {
      safeClose(ws, 1009, "too big");
      return;
    }
    let msg;
    try {
      msg = JSON.parse(message);
    } catch {
      return;
    }
    if (typeof msg !== "object" || msg === null) {
      return;
    }

    if (this.ctx.getTags(ws).includes("host")) {
      // Del anfitrión a un invitado concreto.
      if (typeof msg.to !== "string") {
        return;
      }
      for (const guest of this.ctx.getWebSockets("g:" + msg.to)) {
        guest.send(message);
      }
      return;
    }

    // De un invitado al anfitrión, con su identificador.
    const { id } = ws.deserializeAttachment() || {};
    if (!id) {
      return;
    }
    msg.from = id;
    delete msg.to;
    const out = JSON.stringify(msg);
    for (const host of this.ctx.getWebSockets("host")) {
      host.send(out);
    }
  }

  async webSocketClose(ws, code, reason) {
    safeClose(ws, code, reason);
    const otherHosts = this.ctx.getWebSockets("host").filter((h) => h !== ws);
    if (this.ctx.getTags(ws).includes("host") && otherHosts.length === 0) {
      // Los invitados que aún esperan respuesta no la recibirán.
      for (const guest of this.ctx.getWebSockets("guest")) {
        guest.send(JSON.stringify({ type: "error", reason: "nohost" }));
        safeClose(guest, 4004, "no host");
      }
    }
  }

  async webSocketError(ws) {
    safeClose(ws, 1011, "error");
  }
}

function safeClose(ws, code, reason) {
  try {
    ws.close(code, reason);
  } catch {
    // Ya estaba cerrado, o el código no se puede enviar (1005/1006).
  }
}

// Compara la clave en tiempo constante para no dar pistas sobre ella.
function sameKey(given, expected) {
  const enc = new TextEncoder();
  const a = enc.encode(given);
  const b = enc.encode(expected);
  if (a.byteLength !== b.byteLength) {
    return false;
  }
  return crypto.subtle.timingSafeEqual(a, b);
}

// invitePage abre EmuHub con emuhub://join/… y, si no está instalado o el
// navegador no lo abre, explica qué hacer.
function invitePage(token) {
  if (!INVITE.test(token)) {
    return new Response("Enlace de sala no válido\n", { status: 404 });
  }
  const app = "emuhub://join/" + token;
  const html = `<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Sala de EmuHub</title>
<meta property="og:title" content="Te invitan a jugar en EmuHub">
<meta property="og:description" content="Abre el enlace para entrar en la sala. No necesitas el juego.">
<style>
  :root { color-scheme: dark; --bg: #080a1c; --card: #11142b; --line: #262b4d; --text: #eef0ff; --muted: #9aa0c3; --accent: #3f6cff; }
  * { box-sizing: border-box; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 16px; background: var(--bg); color: var(--text); font: 16px/1.5 system-ui, sans-serif; }
  main { width: min(460px, 100%); background: var(--card); border: 1px solid var(--line); border-radius: 18px; padding: 28px; }
  .label { color: var(--muted); font-size: 13px; text-transform: uppercase; letter-spacing: .08em; }
  h1 { margin: 6px 0 12px; font-size: 24px; }
  p { color: var(--muted); margin: 0 0 16px; }
  .btn { display: block; width: 100%; padding: 12px 16px; border-radius: 12px; border: 1px solid var(--line); background: transparent; color: var(--text); font: inherit; font-weight: 600; text-align: center; text-decoration: none; cursor: pointer; }
  .btn.primary { background: var(--accent); border-color: var(--accent); }
  .btn + .btn { margin-top: 10px; }
  ol { color: var(--muted); padding-left: 20px; margin: 20px 0 0; }
  li { margin-bottom: 6px; }
  b { color: var(--text); }
</style>
</head>
<body>
<main>
  <div class="label">Jugar con amigos</div>
  <h1>Te invitan a una sala de EmuHub</h1>
  <p>Si tienes EmuHub, se abrirá solo con la sala lista. No necesitas el juego: lo ves y lo juegas desde el equipo de tu amigo.</p>
  <a class="btn primary" href="${app}">Abrir en EmuHub</a>
  <button class="btn" id="copy" type="button">Copiar enlace</button>
  <ol>
    <li>¿No se abre? Copia el enlace, abre EmuHub y pégalo en <b>Unirse a una sala</b>.</li>
    <li>¿No tienes EmuHub? <a href="${DOWNLOAD}">Descárgalo</a>, ábrelo una vez y vuelve a pulsar el enlace.</li>
  </ol>
</main>
<script>
  location.href = ${JSON.stringify(app)};
  const copy = document.getElementById("copy");
  copy.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(location.href);
      copy.textContent = "Enlace copiado";
    } catch {
      copy.textContent = "Selecciona la barra de direcciones y cópiala";
    }
  });
</script>
</body>
</html>`;
  return new Response(html, {
    headers: {
      "content-type": "text/html; charset=utf-8",
      "cache-control": "no-store",
      "referrer-policy": "no-referrer",
      "x-content-type-options": "nosniff",
      "content-security-policy": "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'",
    },
  });
}
