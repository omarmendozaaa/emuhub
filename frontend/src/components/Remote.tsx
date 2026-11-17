import {useEffect, useRef, useState} from 'react'
import type {main, remote} from '../../wailsjs/go/models'
import {ClipboardGetText, ClipboardSetText} from '../../wailsjs/runtime/runtime'
import {coverUrl, Game} from '../util'
import {Hint} from './Glyph'

/** How many gamepads this machine has connected, kept up to date. */
export function usePads(): number {
    const count = () => (navigator.getGamepads ? [...navigator.getGamepads()].filter(Boolean).length : 0)
    const [pads, setPads] = useState(count)
    useEffect(() => {
        const update = () => setPads(count())
        const timer = setInterval(update, 1000)
        window.addEventListener('gamepadconnected', update)
        window.addEventListener('gamepaddisconnected', update)
        return () => {
            clearInterval(timer)
            window.removeEventListener('gamepadconnected', update)
            window.removeEventListener('gamepaddisconnected', update)
        }
    }, [])
    return pads
}

/** What the host shares: a code, or a link when the room has a server. */
function CodeBox({code, onCopied}: { code: string, onCopied: () => void }) {
    const link = code.includes('://')
    const copy = async () => {
        if (await ClipboardSetText(code)) onCopied()
    }
    return (
        <>
            <div className={link ? 'room-code link' : 'room-code'}>
                <code>{code}</code>
                <button className="btn small" onClick={copy}>Copiar</button>
            </div>
            {link && <p className="muted small room-code-note">Envía este enlace: al abrirlo, EmuHub entra directamente en tu sala.</p>}
        </>
    )
}

function seatName(s: remote.Seat): string {
    return s.kind === 'host' ? 'Tú' : s.kind === 'guest' ? s.name || `Invitado ${s.guest}` : 'Libre'
}

function freeSeats(seats: remote.Seat[]): number {
    return seats.filter(s => !s.kind).length
}

/** Seated players whose input does not reach the host yet. */
function waitingSeats(seats: remote.Seat[]): remote.Seat[] {
    return seats.filter(s => s.kind && !s.ready)
}

/** Whether the room's game can start: someone seated and everyone seated ready. */
export function roomReady(state: main.RemoteState): boolean {
    const seats = state.seats ?? []
    return seats.some(s => s.kind) && waitingSeats(seats).length === 0
}

/** What a seat's player is doing, for the seat list. */
function seatStatus(s: remote.Seat): string {
    if (s.kind === 'host') return `Anfitrión · ${s.input === 'mando' ? 'con mando' : 'con teclado'}`
    if (s.kind === 'guest') return s.ready ? 'Conectado · con mando' : 'Esperando su mando: que pulse un botón'
    return 'Esperando a un invitado'
}

/** What the host chooses before the room opens. */
export interface RoomSetup {
    gameId: string
    limits: main.RoomLimits
    hostPlays: boolean
    players: number
    row: number
}

/** The fewest and most players a room can have with the host in or out. */
export function playerRange(setup: { limits: main.RoomLimits, hostPlays: boolean }): [number, number] {
    return [setup.hostPlays ? 2 : 1, Math.max(setup.hostPlays ? 2 : 1, setup.limits.maxPlayers)]
}

/** Before the room opens: who plays and how many. */
export function HostSetup({setup, game, onChange, onOpen, onClose}: {
    setup: RoomSetup, game?: Game, onChange: (s: RoomSetup) => void, onOpen: () => void, onClose: () => void,
}) {
    const [least, most] = playerRange(setup)
    const guests = setup.hostPlays ? setup.players - 1 : setup.players
    const setHost = (hostPlays: boolean) => {
        const [lo, hi] = playerRange({...setup, hostPlays})
        onChange({...setup, hostPlays, players: Math.min(hi, Math.max(lo, setup.players)), row: 0})
    }
    const setPlayers = (players: number) => onChange({...setup, players, row: 1})
    const multitap = setup.limits.multitapOver > 0 && setup.players > setup.limits.multitapOver
    return (
        <div className="overlay" onMouseDown={e => e.target === e.currentTarget && onClose()}>
            <div className="room">
                {game?.hasCover && <img className="room-cover" src={coverUrl(game)} alt="" draggable={false}/>}
                <div className="room-label">Sala para jugar con amigos</div>
                <h2>{game?.title}</h2>

                <div className={`room-field ${setup.row === 0 ? 'focused' : ''}`}>
                    <div className="room-field-label">¿Vas a jugar tú?</div>
                    <div className="room-seg">
                        <button className={`btn ${setup.hostPlays ? 'on' : ''}`} aria-pressed={setup.hostPlays} onClick={() => setHost(true)}>Sí, juego</button>
                        <button className={`btn ${!setup.hostPlays ? 'on' : ''}`} aria-pressed={!setup.hostPlays} onClick={() => setHost(false)}>Solo hosteo</button>
                    </div>
                    {!setup.hostPlays && (
                        <p className="muted small">
                            {setup.limits.seatControl
                                ? 'Tus mandos no llegan al juego. El primer invitado será el jugador 1.'
                                : 'Apaga tu mando antes de abrir la sala: con este emulador, el primer mando conectado es el jugador 1.'}
                        </p>
                    )}
                </div>

                <div className={`room-field ${setup.row === 1 ? 'focused' : ''}`}>
                    <div className="room-field-label">¿Cuántos juegan?</div>
                    <div className="room-stepper">
                        <button className="btn" aria-label="Un jugador menos" disabled={setup.players <= least} onClick={() => setPlayers(setup.players - 1)}>−</button>
                        <output>{setup.players}</output>
                        <button className="btn" aria-label="Un jugador más" disabled={setup.players >= most} onClick={() => setPlayers(setup.players + 1)}>+</button>
                        <span className="muted small">
                            {guests === 1 ? 'Entra 1 invitado' : `Entran ${guests} invitados`}{setup.hostPlays ? ' y juegas tú.' : '. Tú miras.'}
                        </span>
                    </div>
                </div>

                {multitap && <p className="room-note note-info">Para {setup.players} jugadores EmuHub activa el multitap solo en esta partida.</p>}
                {!multitap && setup.players >= most && most < 4 && (
                    <p className="room-note note-info">{most === 2 ? 'Este emulador admite 2 jugadores en una sala.' : `Este emulador admite ${most} jugadores en una sala.`}</p>
                )}

                <div className="row-actions">
                    <button className="btn primary" onClick={onOpen}>Abrir sala</button>
                    <button className="btn" onClick={onClose}>Cancelar</button>
                </div>
                <div className="room-hints">
                    <Hint kind="cross" label="Abrir sala" onClick={onOpen}/>
                    <Hint kind="circle" label="Cancelar" onClick={onClose}/>
                    <span className="hint static">◀ ▶ Cambiar valor</span>
                </div>
            </div>
        </div>
    )
}

/** Who plays in each seat, in the order the game sees them. */
function Seats({state, selected, moving, onSelect}: {
    state: main.RemoteState, selected?: number, moving?: boolean, onSelect?: (i: number) => void,
}) {
    return (
        <ol className="seats">
            {(state.seats ?? []).map((s, i) => {
                const mine = selected === i
                return (
                    <li
                        key={i}
                        className={`seat ${s.kind ? '' : 'seat-free'} ${mine ? 'seat-selected' : ''} ${mine && moving ? 'seat-moving' : ''}`}
                        onClick={() => onSelect?.(i)}
                    >
                        <span className="seat-num">J{i + 1}</span>
                        <span className="seat-who">
                            <b>{seatName(s)}</b>
                            <span>{seatStatus(s)}</span>
                        </span>
                        {mine && moving
                            ? <span className="seat-tag">Moviendo · ▲ ▼</span>
                            : s.kind && <span className={`seat-ready ${s.ready ? 'ok' : 'wait'}`}><span className="dot"/>{s.ready ? 'Listo' : 'Sin mando'}</span>}
                    </li>
                )
            })}
        </ol>
    )
}

/** The host's room: shown from the moment it opens until the game starts. */
export function HostRoom({state, game, selected, moving, onSelect, onMoving, onStart, onClose, onCopied}: {
    state: main.RemoteState, game?: Game, selected: number, moving: boolean,
    onSelect: (i: number) => void, onMoving: (on: boolean) => void,
    onStart: () => void, onClose: () => void, onCopied: () => void,
}) {
    const starting = state.phase === 'starting'
    const seats = state.seats ?? []
    const free = freeSeats(seats)
    const filled = seats.length - free
    const waiting = waitingSeats(seats)
    const ready = roomReady(state)
    const canMove = state.seatControl && !!seats[selected]?.kind
    const copy = async () => {
        if (await ClipboardSetText(state.code)) onCopied()
    }
    return (
        <div className="overlay">
            <div className="room wide">
                {game?.hasCover && <img className="room-cover" src={coverUrl(game)} alt="" draggable={false}/>}
                <div className="room-label">{starting ? 'Sala para jugar con amigos' : `Sala abierta${state.hostPlays ? '' : ' · solo hosteas'}`}</div>
                <h2>{game?.title}</h2>
                {starting ? (
                    <>
                        <p className="muted">Abriendo la sala…</p>
                        <p className="muted small">
                            Si tu escritorio pregunta si quieres compartir la pantalla, acepta: es lo que verán tus invitados.
                        </p>
                    </>
                ) : (
                    <>
                        <CodeBox code={state.code} onCopied={onCopied}/>
                        <Seats state={state} selected={selected} moving={moving} onSelect={i => { onSelect(i); onMoving(false) }}/>
                        {moving
                            ? <p className="room-note note-ok">Sube o baja al jugador y confirma.</p>
                            : waiting.length > 0
                                ? <p className="room-note note-warn">
                                    {waiting.map(seatName).join(', ')} {waiting.length === 1 ? 'aún no está listo' : 'aún no están listos'}: pide que pulse{waiting.length === 1 ? '' : 'n'} un botón de su mando. La partida no puede empezar hasta entonces.
                                </p>
                                : free === 0
                                    ? <p className="room-note note-ok">Todos listos. Puedes empezar.</p>
                                    : filled > 0
                                        ? <p className="room-note note-warn">{free === 1 ? 'Falta 1 jugador' : `Faltan ${free} jugadores`}. Envía la invitación o empieza con los que están listos.</p>
                                        : <p className="room-note note-warn">Envía la invitación a tus amigos para que entren.</p>}
                        {!state.seatControl && (
                            <p className="muted small">
                                Con este emulador el número de jugador lo decide el orden en que se conectan los mandos; EmuHub no puede cambiarlo.
                            </p>
                        )}
                        {state.warning && <p className="room-warning">{state.warning}</p>}
                        <p className="muted small">
                            Tus invitados ven toda tu pantalla mientras la sala esté abierta. La sala se cierra sola al salir del juego.
                        </p>
                    </>
                )}
                <div className="row-actions">
                    {!starting && (
                        <button className="btn primary" disabled={!ready} onClick={onStart}>
                            {filled > 0 && !ready ? 'Esperando a los jugadores…' : free === 0 ? 'Empezar partida' : `Empezar con ${filled} de ${seats.length}`}
                        </button>
                    )}
                    {!starting && canMove && (
                        <button className="btn" onClick={() => onMoving(!moving)}>{moving ? 'Dejar aquí' : `Mover a ${seatName(seats[selected])}`}</button>
                    )}
                    <button className="btn" onClick={onClose}>{starting ? 'Cancelar' : 'Cerrar sala'}</button>
                </div>
                {!starting && (
                    <div className="room-hints">
                        {moving ? (
                            <>
                                <span className="hint static">▲ ▼ Cambiar de asiento</span>
                                <Hint kind="cross" label="Dejar aquí" onClick={() => onMoving(false)}/>
                            </>
                        ) : (
                            <>
                                {ready && <Hint kind="cross" label="Empezar" onClick={onStart}/>}
                                {canMove && <Hint kind="square" label="Mover jugador" onClick={() => onMoving(true)}/>}
                                <Hint kind="triangle" label="Copiar invitación" onClick={copy}/>
                                <Hint kind="circle" label="Cerrar" onClick={onClose}/>
                            </>
                        )}
                    </div>
                )}
            </div>
        </div>
    )
}

/** Shown over the running game's screen while the room stays open. */
export function HostBadge({state, onCopied}: { state: main.RemoteState, onCopied: () => void }) {
    return (
        <div className="room-badge">
            <div className="muted small">
                Sala abierta{state.hostPlays ? '' : ' · solo hosteas'}{state.multitap ? ' · multitap' : ''}
            </div>
            <Seats state={state}/>
            <CodeBox code={state.code} onCopied={onCopied}/>
            {state.warning && <p className="room-warning">{state.warning}</p>}
        </div>
    )
}

/** Whether the guest's own gamepad is there, in words as well as colour. */
function PadStatus({pads}: { pads: number }) {
    return pads > 0
        ? <p className="pad-status ok"><span className="dot"/>Mando conectado</p>
        : <p className="pad-status off"><span className="dot"/>Mando sin conectar</p>
}

/** Where a guest types or pastes the code a friend sent. */
export function JoinRoom({support, initial, onJoin, onClose}: {
    support: main.RemoteSupport | null, initial?: string,
    onJoin: (code: string, keyboard: boolean) => Promise<boolean>, onClose: () => void,
}) {
    const [code, setCode] = useState(initial ?? '')
    const [busy, setBusy] = useState(false)
    const [keyboard, setKeyboard] = useState(false)
    const input = useRef<HTMLInputElement>(null)
    const pads = usePads()
    useEffect(() => input.current?.focus(), [])

    const join = async (value = code) => {
        if (!value.trim() || busy) return
        setBusy(true)
        if (!await onJoin(value.trim(), keyboard)) setBusy(false)
    }
    const paste = async () => {
        const text = (await ClipboardGetText()).trim()
        if (text) setCode(text)
        input.current?.focus()
    }
    const blocked = support && !support.guest
    return (
        <div className="overlay" onMouseDown={e => e.target === e.currentTarget && onClose()}>
            <div className="room">
                <div className="room-label">Jugar con amigos</div>
                <h2>Unirse a una sala</h2>
                {blocked ? (
                    <p className="room-warning">{support.guestReason}</p>
                ) : (
                    <>
                        <p className="muted">
                            {initial
                                ? 'Te han invitado a una sala. No necesitas tener el juego: lo ves y lo juegas desde el equipo del anfitrión.'
                                : 'Pega el código o el enlace que te han enviado. No necesitas tener el juego: lo ves y lo juegas desde el equipo del anfitrión.'}
                        </p>
                        <form className="room-form" onSubmit={e => { e.preventDefault(); join() }}>
                            <input
                                ref={input}
                                value={code}
                                onChange={e => setCode(e.target.value)}
                                placeholder="Código o enlace de la sala"
                                aria-label="Código o enlace de la sala"
                                spellCheck={false}
                                autoCapitalize="off"
                                autoCorrect="off"
                            />
                            <button type="button" className="btn" onClick={paste}>Pegar</button>
                        </form>
                        {keyboard ? <p className="pad-status ok"><span className="dot"/>Jugarás con el teclado</p> : <PadStatus pads={pads}/>}
                        {pads === 0 && !keyboard && (
                            <p className="room-note note-warn">
                                Sin mando ves la partida pero no puedes jugar. Conecta uno antes de entrar{support?.keyboard ? ' o juega con el teclado' : ''}.
                            </p>
                        )}
                        <label className={`toggle ${support?.keyboard ? '' : 'toggle-disabled'}`}>
                            <div>
                                <div>Jugar con teclado</div>
                                <div className="muted small">
                                    {support?.keyboard
                                        ? 'EmuHub convierte tu teclado en un mando, y el anfitrión te ve como un jugador más.'
                                        : support?.keyboardReason}
                                </div>
                            </div>
                            <input type="checkbox" checked={keyboard} disabled={!support?.keyboard}
                                   onChange={e => setKeyboard(e.target.checked)}/>
                            <span className="switch"/>
                        </label>
                        {keyboard && (
                            <table className="key-layout">
                                <tbody>
                                    {(support?.keyboardLayout ?? []).map(b => (
                                        <tr key={b.keys}><td><kbd>{b.keys}</kbd></td><td>{b.action}</td></tr>
                                    ))}
                                </tbody>
                            </table>
                        )}
                        {keyboard && pads > 0 && (
                            <p className="room-note note-warn">También tienes un mando conectado: contará como otro jugador. Desconéctalo si solo quieres jugar con el teclado.</p>
                        )}
                        <p className="muted small">El anfitrión decide qué jugador eres desde su sala.</p>
                    </>
                )}
                <div className="row-actions">
                    {!blocked && <button className="btn primary" disabled={!code.trim() || busy} onClick={() => join()}>Unirse</button>}
                    <button className="btn" onClick={onClose}>Cancelar</button>
                </div>
            </div>
        </div>
    )
}

/** Covers EmuHub while the guest is connecting to or playing in a room. */
export function GuestSession({state, onLeave}: { state: main.RemoteState, onLeave: () => void }) {
    const playing = state.phase === 'playing'
    const pads = usePads()
    return (
        <div className="running">
            <div className="running-label">{playing ? 'En la sala de tu amigo' : 'Uniéndote a la sala'}</div>
            <div className="running-title">{playing ? 'Jugando en línea' : state.message || 'Conectando…'}</div>
            <PadStatus pads={pads}/>
            {pads === 0 && <p className="muted small">Conecta un mando por cable o Bluetooth; no hace falta salir de la sala.</p>}
            {playing && pads > 0 && <p className="muted small">Pulsa un botón de tu mando: así el anfitrión te ve listo y puede empezar la partida.</p>}
            {playing && <p className="muted small">Para salir desde el juego: Ctrl + Alt + Mayús + Q.</p>}
            <button className="btn" onClick={onLeave}>{playing ? 'Salir de la sala' : 'Cancelar'}</button>
        </div>
    )
}
