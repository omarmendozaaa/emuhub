import {useEffect, useRef, useState} from 'react'
import type {main} from '../../wailsjs/go/models'
import {ClipboardGetText, ClipboardSetText} from '../../wailsjs/runtime/runtime'
import {coverUrl, Game} from '../util'

/** Room codes are easier to read and to dictate in groups. */
function CodeBox({code, onCopied}: { code: string, onCopied: () => void }) {
    const copy = async () => {
        if (await ClipboardSetText(code)) onCopied()
    }
    return (
        <div className="room-code">
            <code>{code}</code>
            <button className="btn small" onClick={copy}>Copiar</button>
        </div>
    )
}

/** The host's room: shown from the moment it opens until the game starts. */
export function HostRoom({state, game, onStart, onClose, onCopied}: {
    state: main.RemoteState, game?: Game, onStart: () => void, onClose: () => void, onCopied: () => void,
}) {
    const starting = state.phase === 'starting'
    return (
        <div className="overlay">
            <div className="room">
                {game?.hasCover && <img className="room-cover" src={coverUrl(game)} alt="" draggable={false}/>}
                <div className="room-label">Sala para jugar con amigos</div>
                <h2>{game?.title}</h2>
                {starting ? (
                    <>
                        <p className="muted">Abriendo la sala…</p>
                        <p className="muted small">
                            Si tu escritorio pregunta si quieres compartir la pantalla, acepta: es lo que verá tu invitado.
                        </p>
                    </>
                ) : (
                    <>
                        <p className="muted">Envía este código a tu amigo. En su EmuHub: <b>Unirse a una sala</b>.</p>
                        <CodeBox code={state.code} onCopied={onCopied}/>
                        <p className="room-guests">
                            {state.guests === 0 ? 'Esperando invitados…' : state.guests === 1 ? '1 invitado conectado' : `${state.guests} invitados conectados`}
                        </p>
                        {state.warning && <p className="room-warning">{state.warning}</p>}
                        <p className="muted small">
                            Tu invitado verá toda tu pantalla mientras la sala esté abierta, y juega con su mando como jugador 2.
                            La sala se cierra sola al salir del juego.
                        </p>
                    </>
                )}
                <div className="row-actions">
                    {!starting && <button className="btn primary" onClick={onStart}>Empezar partida</button>}
                    <button className="btn" onClick={onClose}>{starting ? 'Cancelar' : 'Cerrar sala'}</button>
                </div>
            </div>
        </div>
    )
}

/** Shown over the running game's screen while the room stays open. */
export function HostBadge({state, onCopied}: { state: main.RemoteState, onCopied: () => void }) {
    return (
        <div className="room-badge">
            <div className="muted small">
                Sala abierta · {state.guests === 1 ? '1 invitado' : `${state.guests} invitados`}
            </div>
            <CodeBox code={state.code} onCopied={onCopied}/>
            {state.warning && <p className="room-warning">{state.warning}</p>}
        </div>
    )
}

/** Where a guest types or pastes the code a friend sent. */
export function JoinRoom({support, onJoin, onClose}: {
    support: main.RemoteSupport | null, onJoin: (code: string) => Promise<boolean>, onClose: () => void,
}) {
    const [code, setCode] = useState('')
    const [busy, setBusy] = useState(false)
    const input = useRef<HTMLInputElement>(null)
    useEffect(() => input.current?.focus(), [])

    const join = async (value = code) => {
        if (!value.trim() || busy) return
        setBusy(true)
        if (!await onJoin(value.trim())) setBusy(false)
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
                            Pega el código que te ha enviado tu amigo. No necesitas tener el juego: lo ves y lo juegas desde su equipo.
                        </p>
                        <form className="room-form" onSubmit={e => { e.preventDefault(); join() }}>
                            <input
                                ref={input}
                                value={code}
                                onChange={e => setCode(e.target.value)}
                                placeholder="EH-XXXX-XXXX-XXXX-XXXX"
                                spellCheck={false}
                                autoCapitalize="characters"
                            />
                            <button type="button" className="btn" onClick={paste}>Pegar</button>
                        </form>
                        <p className="muted small">Conecta tu mando antes de entrar: serás el jugador 2.</p>
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
    return (
        <div className="running">
            <div className="running-label">{playing ? 'En la sala de tu amigo' : 'Uniéndote a la sala'}</div>
            <div className="running-title">{playing ? 'Jugando en línea' : state.message || 'Conectando…'}</div>
            {playing && <p className="muted small">Para salir desde el juego: Ctrl + Alt + Mayús + Q.</p>}
            <button className="btn" onClick={onLeave}>{playing ? 'Salir de la sala' : 'Cancelar'}</button>
        </div>
    )
}
