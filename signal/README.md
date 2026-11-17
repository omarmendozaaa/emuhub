# Servidor de salas de EmuHub

Punto de encuentro para jugar con amigos de otras redes, y la página a la que llevan los enlaces de
sala. Anfitrión e invitado se conectan aquí por
WebSocket, intercambian sus direcciones (candidatos ICE) y desde ahí conectan directamente entre
ellos, sin abrir puertos en el router ni en el firewall. Cada sala son unos pocos mensajes de unos
KB: el vídeo y los mandos nunca pasan por aquí.

Es un Cloudflare Worker con un Durable Object por sala, y cabe de sobra en el plan gratuito: los
WebSockets usan la API de hibernación, así que una sala abierta y en silencio no consume nada.
Cada grupo de amigos monta el suyo en un par de minutos.

## Crear tu servidor

### Con el botón (recomendado)

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/omarmendozaaa/emuhub/tree/main/signal)

1. Pulsa el botón e inicia sesión en Cloudflare (la cuenta gratuita sirve).
2. Cuando pida `ACCESS_KEY`, escribe una clave larga y apúntala (por ejemplo, la salida de
   `openssl rand -hex 16`). Solo la usarás tú, para hostear.
3. Al terminar, Cloudflare muestra la dirección del Worker, del tipo
   `https://emuhub-signal.<tu-subdominio>.workers.dev`.

### Desde la terminal

```sh
cd signal
npm install
npx wrangler login                   # una vez, con tu cuenta de Cloudflare
npx wrangler secret put ACCESS_KEY   # escribe tu clave
npx wrangler deploy                  # imprime la dirección del Worker
```

Comprueba que responde:

```sh
curl https://emuhub-signal.<tu-subdominio>.workers.dev/
# EmuHub signaling server
```

## Usarlo en EmuHub

Quien hostea pone la dirección, con la clave al final, en *Ajustes → Jugar con amigos → Servidor de
salas*:

```
https://emuhub-signal.<tu-subdominio>.workers.dev/?key=<tu-clave>
```

**La clave es solo para quien hostea**: con ella EmuHub crea las salas en tu servidor, y nunca
sale de tu equipo. Las invitaciones llevan la dirección del servidor sin la clave, así que tus
invitados no configuran nada y nadie puede usar tu servidor para crear sus propias salas.

Los invitados entran sin clave porque el servidor solo les deja hablar con una sala que tenga a su
anfitrión conectado, y para encontrarla necesitan su nombre: un hash del secreto de la invitación,
nuevo en cada sala. En la práctica es una clave por sala que caduca al cerrarla: una invitación
antigua no sirve para nada.

Sin la clave correcta el servidor responde 403 al anfitrión. Para cambiarla, repite
`wrangler secret put ACCESS_KEY` y actualiza la dirección en Ajustes.

## Probar en local

```sh
cd signal
printf 'ACCESS_KEY=prueba\n' > .dev.vars     # opcional
npx wrangler dev --port 8787
# en otra terminal, desde la raíz del repositorio:
EMUHUB_SIGNAL_URL='http://127.0.0.1:8787/?key=prueba' go test ./internal/remote/ -run TestTunnel -v
```

## Enlaces de sala

Con servidor configurado, EmuHub comparte cada sala como un enlace a tu Worker:

```
https://emuhub-signal.<tu-subdominio>.workers.dev/j/<invitación>
```

El Worker sirve ahí una página pequeña que abre EmuHub con `emuhub://join/<invitación>` y, si no se
abre, explica cómo pegar el enlace o descargar EmuHub. La invitación entera (direcciones, PIN,
secreto del túnel y dirección del servidor, sin la clave) va en el enlace: el servidor no guarda
nada.

## Protocolo

`GET /v1/rooms/<sala>?role=host|guest` con cabecera de WebSocket; el anfitrión añade
`&key=<ACCESS_KEY>`. `<sala>` son 32 caracteres hexadecimales: un hash del secreto que va en la
invitación.

- El anfitrión mantiene su WebSocket abierto mientras la sala existe y envía `ping` cada 30 s
  (el servidor responde `pong` sin despertar).
- El invitado envía `{"type":"offer","ice":{…}}`; el servidor la reenvía al anfitrión añadiendo
  `from`. El anfitrión contesta `{"type":"answer","to":"<from>",…}` y el servidor la entrega a
  ese invitado.
- Si no hay anfitrión, el invitado recibe `{"type":"error","reason":"nohost"}`.

Las respuestas del anfitrión van firmadas (HMAC) con el secreto de la sala, y el invitado demuestra
conocerlo dentro de la conexión cifrada, así que el servidor no puede suplantar a ninguno de los dos.
