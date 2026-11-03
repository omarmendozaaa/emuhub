import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {library, main} from '../wailsjs/go/models'
import {
    ChooseCover, FetchCovers, GetBundleInfo, GetConsoles, GetGames, GetSettings, LaunchGame, Quit,
    RemoveGame, RunningGame, ScanLibrary, StopGame, ToggleFavorite,
} from '../wailsjs/go/main/App'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {Carousel} from './components/Carousel'
import {GameList} from './components/GameList'
import {GameMenu, gameMenuItems} from './components/GameMenu'
import {Glyph, Hint} from './components/Glyph'
import {CoverProgress, Settings} from './components/Settings'
import {Action, blip, useInput} from './input'
import {backdropUrl, coverUrl, errorText, formatAgo, formatPlaytime, Game} from './util'

interface Category {
    id: string
    label: string
    accent?: string
}

type Toast = { id: number, text: string, kind: 'info' | 'error' }

// Rows skipped by left/right in the list.
const LIST_PAGE = 10

export default function App() {
    const [consoles, setConsoles] = useState<main.ConsoleInfo[]>([])
    const [games, setGames] = useState<Game[]>([])
    const [settings, setSettings] = useState<library.Settings | null>(null)
    const [catIndex, setCatIndex] = useState(0)
    const [focusByCat, setFocusByCat] = useState<Record<string, number>>({})
    const [panel, setPanel] = useState<'none' | 'settings' | 'menu'>('none')
    const [settingsTab, setSettingsTab] = useState('general')
    const [bundle, setBundle] = useState<main.BundleState | null>(null)
    const [menuIndex, setMenuIndex] = useState(0)
    const [running, setRunning] = useState<string>('')
    const [scanning, setScanning] = useState(false)
    const [covers, setCovers] = useState<CoverProgress | null>(null)
    const [toasts, setToasts] = useState<Toast[]>([])
    const [clock, setClock] = useState(() => new Date())
    const toastId = useRef(0)

    const toast = useCallback((text: string, kind: Toast['kind'] = 'info') => {
        const id = ++toastId.current
        setToasts(t => [...t, {id, text, kind}])
        setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), kind === 'error' ? 7000 : 3500)
    }, [])

    const reload = useCallback(async () => {
        const [c, g, s] = await Promise.all([GetConsoles(), GetGames(), GetSettings()])
        setConsoles(c ?? [])
        setGames(g ?? [])
        setSettings(s)
    }, [])

    const scan = useCallback(async () => {
        setScanning(true)
        try {
            const n = await ScanLibrary()
            toast(n === 0 ? 'No se encontraron juegos. Revisa las carpetas en Ajustes.' : `Biblioteca actualizada: ${n} juegos`)
        } catch (e) {
            toast(errorText(e), 'error')
        } finally {
            setScanning(false)
            reload()
        }
    }, [reload, toast])

    useEffect(() => {
        reload().then(async () => {
            const g = await GetGames()
            if (!g || g.length === 0) scan()
        })
        RunningGame().then(setRunning)
        GetBundleInfo().then(setBundle)
        const offs = [
            EventsOn('bundle:state', (s: main.BundleState) => {
                setBundle(s)
                if (s.ready || s.error) reload()
            }),
            EventsOn('firmware:installing', (name: string) => toast(`Instalando el firmware de PS3 en ${name}. Sigue las instrucciones de su ventana.`)),
            EventsOn('library:changed', () => reload()),
            EventsOn('covers:progress', (p: CoverProgress) => setCovers(p)),
            EventsOn('game:started', (id: string) => setRunning(id)),
            EventsOn('game:exited', (e: { id: string, seconds: number, error: string }) => {
                setRunning('')
                if (e.error) toast(`El emulador se cerró con error: ${e.error}`, 'error')
                else if (e.seconds >= 60) toast(`Sesión de ${formatPlaytime(e.seconds)}`)
            }),
        ]
        const tick = setInterval(() => setClock(new Date()), 15_000)
        return () => {
            offs.forEach(off => off())
            clearInterval(tick)
        }
    }, [])

    const consoleMap = useMemo(() => Object.fromEntries(consoles.map(c => [c.id, c])), [consoles])

    const categories: Category[] = useMemo(() => [
        {id: 'fav', label: 'Favoritos'},
        {id: 'recent', label: 'Recientes'},
        {id: 'all', label: 'Todos'},
        ...consoles.map(c => ({id: c.id, label: c.shortName, accent: c.accent})),
    ], [consoles])

    const category = categories[catIndex] ?? categories[0]
    // Favorites are few enough for the carousel; everything else is a list.
    const isCarousel = category.id === 'fav'

    const list = useMemo(() => {
        switch (category.id) {
            case 'all':
                return games
            case 'recent':
                return games.filter(g => g.lastPlayed > 0).sort((a, b) => b.lastPlayed - a.lastPlayed)
            case 'fav':
                return games.filter(g => g.favorite)
            default:
                return games.filter(g => g.console === category.id)
        }
    }, [games, category.id])

    const gameCounts = useMemo(() => {
        const out: Record<string, number> = {}
        for (const g of games) out[g.console] = (out[g.console] ?? 0) + 1
        return out
    }, [games])

    const focus = Math.min(focusByCat[category.id] ?? 0, Math.max(0, list.length - 1))
    const current: Game | undefined = list[focus]
    const currentConsole = current ? consoleMap[current.console] : consoleMap[category.id]
    const accent = currentConsole?.accent ?? '#4a6bff'

    const setFocus = (i: number) => {
        if (i === focus || i < 0 || i >= list.length) return
        blip('move')
        setFocusByCat(f => ({...f, [category.id]: i}))
    }

    const switchCategory = (dir: number) => {
        blip('move')
        setCatIndex(i => (i + dir + categories.length) % categories.length)
    }

    const openSettings = (tab = 'general') => {
        setSettingsTab(tab)
        setPanel('settings')
    }

    const play = async (g: Game | undefined = current) => {
        if (!g || running) return
        blip('ok')
        try {
            await LaunchGame(g.id)
        } catch (e) {
            const msg = errorText(e)
            // "REQ:<console>:<message>" means something must be configured first.
            const m = msg.match(/^REQ:([^:]+):(.*)$/s)
            if (m) {
                toast(m[2], 'error')
                openSettings(m[1])
            } else {
                toast(msg, 'error')
            }
        }
    }

    const toggleFav = async (g: Game | undefined = current) => {
        if (!g) return
        try {
            const updated = await ToggleFavorite(g.id)
            setGames(gs => gs.map(x => (x.id === g.id ? updated : x)))
            toast(updated.favorite ? `★ ${g.title} añadido a favoritos` : `${g.title} quitado de favoritos`)
        } catch (e) {
            toast(errorText(e), 'error')
        }
    }

    const menuItems = current ? gameMenuItems(current, {
        play: () => { setPanel('none'); play() },
        favorite: () => { setPanel('none'); toggleFav() },
        cover: async () => {
            setPanel('none')
            try {
                const updated = await ChooseCover(current.id)
                setGames(gs => gs.map(x => (x.id === updated.id ? updated : x)))
            } catch (e) {
                toast(errorText(e), 'error')
            }
        },
        remove: async () => {
            setPanel('none')
            await RemoveGame(current.id)
            toast(`${current.title} quitado (vuelve a aparecer al reescanear si el archivo sigue ahí)`)
            reload()
        },
        close: () => setPanel('none'),
    }) : []

    useInput((a: Action) => {
        if (running) return
        if (panel === 'settings') {
            if (a === 'back' || a === 'options') { blip('back'); setPanel('none') }
            return
        }
        if (panel === 'menu') {
            if (a === 'up') { blip('move'); setMenuIndex(i => (i - 1 + menuItems.length) % menuItems.length) }
            if (a === 'down') { blip('move'); setMenuIndex(i => (i + 1) % menuItems.length) }
            if (a === 'confirm') menuItems[menuIndex]?.run()
            if (a === 'back' || a === 'share') { blip('back'); setPanel('none') }
            return
        }
        switch (a) {
            case 'left':
                setFocus(isCarousel ? focus - 1 : Math.max(0, focus - LIST_PAGE))
                break
            case 'right':
                setFocus(isCarousel ? focus + 1 : Math.min(list.length - 1, focus + LIST_PAGE))
                break
            case 'up':
                setFocus(isCarousel ? 0 : focus - 1)
                break
            case 'down':
                setFocus(isCarousel ? list.length - 1 : focus + 1)
                break
            case 'l1': switchCategory(-1); break
            case 'r1': switchCategory(1); break
            case 'confirm': play(); break
            case 'square': toggleFav(); break
            case 'triangle': if (!scanning) scan(); break
            case 'options': blip('ok'); openSettings(); break
            case 'share': if (current) { blip('ok'); setMenuIndex(0); setPanel('menu') } break
        }
    })

    const totalPlay = games.reduce((s, g) => s + g.playSeconds, 0)
    const readyCount = consoles.filter(c => c.ready).length
    const runningGame = running ? games.find(g => g.id === running) : undefined
    const bg = current ? backdropUrl(current) : ''

    const info = current && (
        <>
            <h1>{current.title}</h1>
            <div className="focus-meta">
                <span className="meta-console" style={{color: currentConsole?.accent}}>{currentConsole?.name}</span>
                {current.serial && <span>{current.serial}</span>}
                <span>{formatPlaytime(current.playSeconds)}</span>
                {current.lastPlayed > 0 && <span>{formatAgo(current.lastPlayed)}</span>}
            </div>
            <button className="play-btn" onClick={() => play()} tabIndex={-1}>
                <Glyph kind="cross" size={30}/>
            </button>
            {currentConsole && !currentConsole.ready && !bundle?.installing && (
                <button className="warn" onClick={() => openSettings(currentConsole.id)} tabIndex={-1}>
                    {!currentConsole.emulatorOk
                        ? `Configura un emulador para ${currentConsole.shortName}`
                        : `Falta: ${currentConsole.requirements.filter(r => !r.ok).map(r => r.label).join(', ')}`}
                </button>
            )}
        </>
    )

    return (
        <div className="app" style={{['--accent' as string]: accent}}>
            <Background image={bg} accent={accent}/>

            <header className="topbar">
                <div className="tabs">
                    <span className="tab-glyph" onClick={() => switchCategory(-1)}><Glyph kind="l1"/></span>
                    {categories.map((c, i) => (
                        <button
                            key={c.id}
                            className={'tab' + (i === catIndex ? ' active' : '')}
                            style={c.accent ? {['--tab-accent' as string]: c.accent} : undefined}
                            onClick={() => { blip('move'); setCatIndex(i) }}
                            tabIndex={-1}
                        >
                            {c.label}
                            {c.accent && <span className="tab-count">{gameCounts[c.id] ?? 0}</span>}
                        </button>
                    ))}
                    <span className="tab-glyph" onClick={() => switchCategory(1)}><Glyph kind="r1"/></span>
                </div>
                <div className="topbar-right">
                    {covers?.busy && <span className="pill">Portadas {covers.done}/{covers.total}</span>}
                    {scanning && <span className="pill">Escaneando…</span>}
                    <span className="clock">{clock.toLocaleTimeString('es', {hour: '2-digit', minute: '2-digit'})}</span>
                    <button className="icon-btn" title="Ajustes" onClick={() => openSettings()} tabIndex={-1}>⚙</button>
                    <button className="icon-btn" title="Salir" onClick={() => Quit()} tabIndex={-1}>⏻</button>
                </div>
            </header>

            <main className="stage">
                {list.length === 0 ? (
                    <EmptyState
                        category={category}
                        hasAny={games.length > 0}
                        scanning={scanning}
                        onSettings={() => openSettings(consoleMap[category.id] ? category.id : 'general')}
                        onScan={scan}
                        onShowAll={() => { blip('move'); setCatIndex(categories.findIndex(c => c.id === 'all')) }}
                    />
                ) : isCarousel ? (
                    <>
                        <Carousel games={list} focus={focus} consoles={consoleMap} onFocus={setFocus} onActivate={i => play(list[i])}/>
                        {info && <div className="focus-info">{info}</div>}
                    </>
                ) : (
                    <div className="list-view">
                        <GameList games={list} focus={focus} consoles={consoleMap} onFocus={setFocus} onActivate={i => play(list[i])}/>
                        {current && (
                            <aside className="list-detail" style={{['--accent' as string]: accent}}>
                                <div className="list-cover">
                                    {current.hasCover
                                        ? <img src={coverUrl(current)} alt="" draggable={false}/>
                                        : <span>{currentConsole?.shortName}</span>}
                                </div>
                                {info}
                            </aside>
                        )}
                    </div>
                )}
            </main>

            <footer className="footer">
                <div className="stats">
                    <div className="stats-title">Biblioteca</div>
                    <div className="stats-line">{games.length} juegos · {totalPlay ? `${formatPlaytime(totalPlay)} jugadas` : "sin partidas aún"}</div>
                    <div className="stats-line">Emuladores listos: {readyCount}/{consoles.length}</div>
                    <div className="stats-bar"><span style={{width: `${consoles.length ? (readyCount / consoles.length) * 100 : 0}%`}}/></div>
                </div>
                <div className="hints">
                    <Hint kind="cross" label="Jugar" onClick={() => play()}/>
                    <Hint kind="square" label="Favorito" onClick={() => toggleFav()}/>
                    <Hint kind="triangle" label="Reescanear" onClick={() => !scanning && scan()}/>
                    <Hint kind="share" label="Opciones" onClick={() => current && setPanel('menu')}/>
                    <Hint kind="options" label="Ajustes" onClick={() => openSettings()}/>
                </div>
            </footer>

            {panel === 'settings' && settings && (
                <Settings
                    key={settingsTab}
                    initialTab={settingsTab}
                    bundle={bundle}
                    consoles={consoles}
                    settings={settings}
                    gameCounts={gameCounts}
                    scanning={scanning}
                    covers={covers}
                    onClose={() => setPanel('none')}
                    onChanged={reload}
                    onScan={scan}
                    onCovers={() => FetchCovers()}
                    onError={msg => toast(msg, 'error')}
                />
            )}

            {panel === 'menu' && current && (
                <GameMenu game={current} items={menuItems} index={menuIndex} onPick={i => menuItems[i].run()} onClose={() => setPanel('none')}/>
            )}

            {running && (
                <div className="running">
                    {runningGame?.hasCover && <img src={coverUrl(runningGame)} alt=""/>}
                    <div className="running-label">En juego</div>
                    <div className="running-title">{runningGame?.title}</div>
                    <button className="btn" onClick={() => StopGame()}>Cerrar juego</button>
                </div>
            )}

            {bundle?.installing && (
                <div className="preparing">
                    <h2>Preparando emuladores</h2>
                    <p>Solo ocurre la primera vez (o tras actualizar EmuHub). Se están descomprimiendo los emuladores incluidos.</p>
                    <div className="preparing-bar"><span style={{width: `${bundle.total ? (bundle.done / bundle.total) * 100 : 0}%`}}/></div>
                    <p className="small">{bundle.total ? Math.round((bundle.done / bundle.total) * 100) : 0}%</p>
                </div>
            )}

            <div className="toasts">
                {toasts.map(t => <div key={t.id} className={'toast ' + t.kind}>{t.text}</div>)}
            </div>
        </div>
    )
}

function Background({image, accent}: { image: string, accent: string }) {
    // Each new image fades in over the previous ones, which are dropped only
    // once it fully covers them, so removing them cannot change what is visible.
    const [layers, setLayers] = useState<{ src: string, key: number }[]>([])
    const key = useRef(0)
    useEffect(() => {
        let cancelled = false
        const add = () => {
            if (!cancelled) setLayers(l => [...l, {src: image, key: ++key.current}])
        }
        const t = setTimeout(() => {
            if (!image) return add()
            const img = new Image()
            img.src = image
            img.decode().catch(() => {}).finally(add)
        }, 120)
        return () => {
            cancelled = true
            clearTimeout(t)
        }
    }, [image])

    return (
        <div className="bg">
            <div className="bg-base"/>
            <div className="bg-arts">
                {layers.map(l => (
                    <div
                        key={l.key}
                        className="bg-art"
                        style={l.src ? {backgroundImage: `url("${l.src}")`} : undefined}
                        onAnimationEnd={() => setLayers(all => all.slice(all.findIndex(x => x.key === l.key)))}
                    />
                ))}
            </div>
            <div className="bg-glow" style={{background: `radial-gradient(ellipse 70% 55% at 50% 52%, ${accent}55, transparent 70%)`}}/>
            <div className="bg-waves"/>
            <div className="bg-vignette"/>
        </div>
    )
}

function EmptyState({category, hasAny, scanning, onSettings, onScan, onShowAll}: {
    category: Category, hasAny: boolean, scanning: boolean, onSettings: () => void, onScan: () => void, onShowAll: () => void,
}) {
    let title = 'Tu biblioteca está vacía'
    let text = 'Añade las carpetas donde guardas tus juegos y configura los emuladores en Ajustes.'
    if (hasAny && category.id === 'fav') {
        title = 'Sin favoritos'
        text = 'Pulsa □ (o F) sobre un juego de tus listas para añadirlo aquí.'
    } else if (hasAny && category.id === 'recent') {
        title = 'Nada jugado todavía'
        text = 'Los juegos que inicies aparecerán aquí.'
    } else if (hasAny) {
        title = `No hay juegos de ${category.label}`
        text = 'Añade una carpeta para esta consola en Ajustes y vuelve a escanear.'
    }
    return (
        <div className="empty">
            <h2>{title}</h2>
            <p>{text}</p>
            <div className="row-actions">
                {hasAny && category.id === 'fav' && <button className="btn primary" onClick={onShowAll}>Ver todos los juegos</button>}
                <button className="btn primary" onClick={onSettings}>Abrir ajustes</button>
                <button className="btn" disabled={scanning} onClick={onScan}>{scanning ? 'Escaneando…' : 'Escanear'}</button>
            </div>
        </div>
    )
}
