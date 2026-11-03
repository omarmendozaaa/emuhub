import {useEffect, useState} from 'react'
import type {library, main} from '../../wailsjs/go/models'
import {
    DetectEmulators, OpenBIOSFolder, PickExecutable, PickFolder, SaveConsoleConfig, SaveSettings,
} from '../../wailsjs/go/main/App'
import {errorText} from '../util'

export interface CoverProgress {
    done: number
    total: number
    found: number
    busy: boolean
}

interface Props {
    initialTab?: string
    bundle: main.BundleState | null
    consoles: main.ConsoleInfo[]
    settings: library.Settings
    gameCounts: Record<string, number>
    scanning: boolean
    covers: CoverProgress | null
    onClose: () => void
    onChanged: () => void
    onScan: () => void
    onCovers: () => void
    onError: (msg: string) => void
}

export function Settings(p: Props) {
    const [tab, setTab] = useState<string>(p.initialTab ?? 'general')
    const consoleInfo = p.consoles.find(c => c.id === tab)

    return (
        <div className="overlay" onMouseDown={e => e.target === e.currentTarget && p.onClose()}>
            <div className="settings">
                <nav className="settings-nav">
                    <h2>Ajustes</h2>
                    <button className={tab === 'general' ? 'active' : ''} onClick={() => setTab('general')}>
                        <span className="nav-dot" style={{background: '#fff'}}/>General
                    </button>
                    <div className="nav-label">Consolas</div>
                    {p.consoles.map(c => (
                        <button key={c.id} className={tab === c.id ? 'active' : ''} onClick={() => setTab(c.id)}>
                            <span className="nav-dot" style={{background: c.accent}}/>
                            {c.name}
                            <span className={'nav-status ' + (c.ready ? 'ok' : 'off')}>{c.ready ? '●' : '○'}</span>
                        </button>
                    ))}
                    <div className="nav-spacer"/>
                    <button className="close-btn" onClick={p.onClose}>Cerrar <kbd>Esc</kbd></button>
                </nav>
                <section className="settings-body">
                    {tab === 'general' && <General {...p}/>}
                    {consoleInfo && <ConsoleSettings key={consoleInfo.id} info={consoleInfo} count={p.gameCounts[consoleInfo.id] ?? 0} {...p}/>}
                </section>
            </div>
        </div>
    )
}

function General(p: Props) {
    const save = async (patch: Partial<library.Settings>) => {
        try {
            await SaveSettings({...p.settings, ...patch} as library.Settings)
            p.onChanged()
        } catch (e) {
            p.onError(errorText(e))
        }
    }
    const ready = p.consoles.filter(c => c.ready).length
    return (
        <div className="panel">
            <h3>Biblioteca</h3>
            <p className="muted">
                EmuHub busca juegos en las carpetas de cada consola. {ready} de {p.consoles.length} consolas tienen emulador listo.
            </p>
            <div className="row-actions">
                <button className="btn primary" disabled={p.scanning} onClick={p.onScan}>
                    {p.scanning ? 'Escaneando…' : 'Escanear biblioteca'}
                </button>
                <button className="btn" disabled={p.covers?.busy} onClick={p.onCovers}>
                    {p.covers?.busy ? `Descargando portadas ${p.covers.done}/${p.covers.total}` : 'Descargar portadas que faltan'}
                </button>
            </div>
            {p.covers && !p.covers.busy && p.covers.total > 0 && (
                <p className="muted small">Última descarga: {p.covers.found} de {p.covers.total} portadas encontradas.</p>
            )}

            <h3>Comportamiento</h3>
            <Toggle
                label="Minimizar EmuHub al iniciar un juego"
                hint="La ventana vuelve sola cuando cierras el emulador."
                value={p.settings.minimizeOnLaunch}
                onChange={v => save({minimizeOnLaunch: v})}
            />
            <Toggle
                label="Descargar portadas automáticamente"
                hint="Usa la base de datos libretro-thumbnails tras cada escaneo. Funciona mejor con nombres No-Intro / Redump."
                value={p.settings.autoCovers}
                onChange={v => save({autoCovers: v})}
            />

            {p.bundle?.available && (
                <>
                    <h3>Emuladores incluidos</h3>
                    <p className="muted small">
                        Esta copia de EmuHub trae sus propios emuladores (paquete {p.bundle.version}), así todos usáis las mismas versiones.
                        {p.bundle.installing && ' Preparándolos…'}
                        {p.bundle.error && <span className="error-text"> {p.bundle.error}</span>}
                    </p>
                    <table className="controls">
                        <tbody>
                        {p.bundle.emulators.map(e => (
                            <tr key={e.name}>
                                <td>{e.name} <span className="muted">{e.version}</span></td>
                                <td title={e.source}>{e.license}</td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                </>
            )}

            <h3>Controles</h3>
            <table className="controls">
                <tbody>
                <tr><td>Navegar</td><td>← → / D-pad / stick</td></tr>
                <tr><td>Jugar</td><td>Enter / ✕ (A)</td></tr>
                <tr><td>Cambiar categoría</td><td>Q · E / L1 · R1</td></tr>
                <tr><td>Favorito</td><td>F / □ (X)</td></tr>
                <tr><td>Opciones del juego</td><td>M / Share (View)</td></tr>
                <tr><td>Reescanear</td><td>R / △ (Y)</td></tr>
                <tr><td>Ajustes</td><td>O / Options (Menu)</td></tr>
                <tr><td>Atrás</td><td>Esc / ○ (B)</td></tr>
                </tbody>
            </table>
        </div>
    )
}

function Toggle({label, hint, value, onChange}: { label: string, hint?: string, value: boolean, onChange: (v: boolean) => void }) {
    return (
        <label className="toggle">
            <div>
                <div>{label}</div>
                {hint && <div className="muted small">{hint}</div>}
            </div>
            <input type="checkbox" checked={value} onChange={e => onChange(e.target.checked)}/>
            <span className="switch"/>
        </label>
    )
}

function ConsoleSettings(p: Props & { info: main.ConsoleInfo, count: number }) {
    const {info} = p
    const [mode, setMode] = useState(info.emulator)
    const [command, setCommand] = useState(info.command)
    const [dirs, setDirs] = useState<string[]>(info.romDirs ?? [])
    const [detected, setDetected] = useState<library.DetectedEmulator[] | null>(null)

    useEffect(() => {
        DetectEmulators(info.id).then(d => setDetected(d ?? []))
    }, [info.id])

    const persist = async (cmd: string, d: string[], m = mode) => {
        try {
            await SaveConsoleConfig(info.id, m, cmd, d)
            p.onChanged()
        } catch (e) {
            p.onError(errorText(e))
        }
    }

    const addDir = async () => {
        const dir = await PickFolder()
        if (!dir || dirs.includes(dir)) return
        const next = [...dirs, dir]
        setDirs(next)
        persist(command, next)
    }

    const removeDir = (dir: string) => {
        const next = dirs.filter(d => d !== dir)
        setDirs(next)
        persist(command, next)
    }

    const browse = async () => {
        const exe = await PickExecutable()
        if (!exe) return
        const quoted = exe.includes(' ') ? `"${exe}"` : exe
        const next = `${quoted} {rom}`
        setCommand(next)
        setMode('')
        persist(next, dirs, '')
    }

    return (
        <div className="panel">
            <div className="console-head" style={{['--accent' as string]: info.accent}}>
                <span className="console-badge">{info.shortName}</span>
                <div>
                    <h3>{info.name}</h3>
                    <p className="muted small">{info.maker} · {p.count} juegos · {info.extensions.join(' ')}</p>
                </div>
                <span className={'status-chip ' + (info.ready ? 'ok' : 'off')}>
                    {info.ready ? 'Listo para jugar' : !info.emulatorOk ? 'Sin emulador' : 'Faltan archivos'}
                </span>
            </div>

            <h4>Emulador</h4>
            {info.bundled && (
                <div className="choice-row">
                    <button
                        className={'choice' + (mode === 'bundled' ? ' active' : '')}
                        onClick={() => { setMode('bundled'); persist(command, dirs, 'bundled') }}
                    >
                        <span className="choice-title">Incluido: {info.bundled.name}</span>
                        <span className="choice-sub">{info.bundled.version} · misma versión para todos</span>
                    </button>
                    <button
                        className={'choice' + (mode !== 'bundled' ? ' active' : '')}
                        onClick={() => { setMode(''); persist(command, dirs, '') }}
                    >
                        <span className="choice-title">Mi propio emulador</span>
                        <span className="choice-sub">Un comando o ejecutable de tu sistema</span>
                    </button>
                </div>
            )}
            {mode === 'bundled' && info.bundled && !info.emulatorOk && (
                <p className="muted small">{p.bundle?.installing ? 'Preparando el emulador incluido…' : p.bundle?.error || 'El emulador incluido no está disponible.'}</p>
            )}
            {mode !== 'bundled' && <>
            <p className="muted small">
                Comando para lanzar un juego. <code>{'{rom}'}</code> se sustituye por la ruta del juego.
            </p>
            <div className="input-row">
                <input
                    className="text"
                    value={command}
                    placeholder='p. ej. pcsx2-qt -batch -nogui -fullscreen -- {rom}'
                    spellCheck={false}
                    onChange={e => setCommand(e.target.value)}
                    onBlur={() => command !== info.command && persist(command, dirs)}
                    onKeyDown={e => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
                />
                <button className="btn" onClick={browse}>Examinar…</button>
            </div>
            <div className="detected">
                {detected === null && <span className="muted small">Buscando emuladores instalados…</span>}
                {detected?.length === 0 && (
                    <span className="muted small">
                        No se detectó ningún emulador. Instala uno (p. ej. {defaultSuggestion(info.id)}) o indica la ruta manualmente.
                    </span>
                )}
                {detected?.map(d => (
                    <button
                        key={d.command}
                        className={'chip' + (d.command === command ? ' active' : '')}
                        title={d.command}
                        onClick={() => {
                            setCommand(d.command)
                            persist(d.command, dirs)
                        }}
                    >
                        {d.label}
                    </button>
                ))}
            </div>
            </>}

            {info.requirements.length > 0 && (
                <>
                    <h4>BIOS y firmware</h4>
                    <p className="muted small">
                        EmuHub no incluye estos archivos. Déjalos en esta carpeta; se comprueban antes de cada partida
                        {mode === 'bundled' && ' y se copian al emulador incluido automáticamente'}.
                    </p>
                    {info.requirements.map(r => (
                        <RequirementRow key={r.id} req={r} console={info.id} onChanged={p.onChanged} onError={p.onError}/>
                    ))}
                </>
            )}

            <h4>Carpetas de juegos</h4>
            {info.id === 'ps3' && (
                <p className="muted small">
                    Para PS3 se detectan carpetas de juego extraídas (con <code>PS3_GAME</code>), juegos instalados en
                    <code> dev_hdd0/game</code> e imágenes ISO.
                </p>
            )}
            <ul className="dir-list">
                {dirs.length === 0 && <li className="muted small">Ninguna carpeta añadida.</li>}
                {dirs.map(d => (
                    <li key={d}>
                        <span className="dir-path" title={d}>{d}</span>
                        <button className="btn ghost small" onClick={() => removeDir(d)}>Quitar</button>
                    </li>
                ))}
            </ul>
            <div className="row-actions">
                <button className="btn" onClick={addDir}>Añadir carpeta…</button>
                <button className="btn primary" disabled={p.scanning} onClick={p.onScan}>
                    {p.scanning ? 'Escaneando…' : 'Escanear ahora'}
                </button>
            </div>
        </div>
    )
}

function defaultSuggestion(id: string) {
    return {snes: 'Snes9x o RetroArch', n64: 'RMG, simple64 o Mupen64Plus', ps1: 'DuckStation', ps2: 'PCSX2', ps3: 'RPCS3'}[id] ?? 'un emulador'
}

function RequirementRow({req, console: consoleId, onChanged, onError}: {
    req: main.RequirementStatus, console: string, onChanged: () => void, onError: (msg: string) => void,
}) {
    const open = async () => {
        try {
            await OpenBIOSFolder(consoleId)
        } catch (e) {
            onError(errorText(e))
        }
    }

    return (
        <div className={'req ' + (req.ok ? 'ok' : 'missing')}>
            <div className="req-head">
                <span className="req-icon">{req.ok ? '✓' : '!'}</span>
                <span className="req-label">{req.label}</span>
                <span className="req-detail">{req.detail}</span>
            </div>
            <p className="muted small">{req.hint}</p>
            <p className="small"><code>{req.path}</code></p>
            <div className="row-actions">
                <button className="btn" onClick={open}>Abrir carpeta</button>
                <button className="btn" onClick={onChanged}>Comprobar de nuevo</button>
            </div>
        </div>
    )
}
