import {useEffect, useState} from 'react'
import type {main} from '../../wailsjs/go/models'
import {coverUrl, Game} from '../util'

const WINDOW = 8 // tiles rendered on each side of the focused one

function useTileWidth() {
    const calc = () => Math.round(Math.min(250, Math.max(120, Math.min(window.innerWidth * 0.135, window.innerHeight * 0.26))))
    const [w, setW] = useState(calc)
    useEffect(() => {
        const onResize = () => setW(calc())
        window.addEventListener('resize', onResize)
        return () => window.removeEventListener('resize', onResize)
    }, [])
    return w
}

interface Props {
    games: Game[]
    focus: number
    consoles: Record<string, main.ConsoleInfo>
    onFocus: (i: number) => void
    onActivate: (i: number) => void
}

export function Carousel({games, focus, consoles, onFocus, onActivate}: Props) {
    const w = useTileWidth()
    const h = Math.round(w * 1.38)
    const gap = Math.round(w * 0.1)
    const focusScale = 1.16

    const tiles = []
    for (let i = Math.max(0, focus - WINDOW); i <= Math.min(games.length - 1, focus + WINDOW); i++) {
        const d = i - focus
        const abs = Math.abs(d)
        const side = Math.sign(d)
        // Neighbours are pushed out by the focused tile's extra width.
        const x = d * (w + gap) + side * (w * (focusScale - 1) / 2 + gap * 0.6)
        const rot = side * -24
        const z = abs === 0 ? 60 : -40 - abs * 18
        const scale = abs === 0 ? focusScale : 0.94
        const opacity = abs <= 5 ? 1 - abs * 0.06 : Math.max(0, 1 - (abs - 4) * 0.3)
        const g = games[i]
        const c = consoles[g.console]
        tiles.push(
            <div
                key={g.id}
                className={'tile' + (d === 0 ? ' focused' : '')}
                style={{
                    width: w, height: h, marginLeft: -w / 2, marginTop: -h / 2,
                    transform: `translateX(${x}px) perspective(1100px) translateZ(${z}px) rotateY(${rot}deg) scale(${scale})`,
                    opacity, zIndex: 100 - abs,
                    ['--accent' as string]: c?.accent ?? '#4a7dff',
                }}
                onClick={() => (d === 0 ? onActivate(i) : onFocus(i))}
            >
                <Tile game={g} consoleName={c?.shortName ?? g.console}/>
            </div>,
        )
    }

    return (
        <div
            className="carousel"
            onWheel={e => {
                const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
                if (Math.abs(delta) < 4) return
                onFocus(Math.min(games.length - 1, Math.max(0, focus + Math.sign(delta))))
            }}
        >
            <div className="carousel-stage">{tiles}</div>
        </div>
    )
}

function Tile({game, consoleName}: { game: Game, consoleName: string }) {
    const [broken, setBroken] = useState(false)
    const src = coverUrl(game)
    useEffect(() => setBroken(false), [src])
    const showArt = src && !broken

    return (
        <div className="card">
            <div className="strip">
                <span className="strip-logo">{consoleName}</span>
            </div>
            <div className="art">
                {showArt ? (
                    <>
                        <img className="art-fill" src={src} alt="" draggable={false}/>
                        <img className="art-main" src={src} alt="" draggable={false} onError={() => setBroken(true)}/>
                    </>
                ) : (
                    <div className="art-fallback">
                        <span className="fallback-console">{consoleName}</span>
                        <span className="fallback-title">{game.title}</span>
                    </div>
                )}
            </div>
            {game.favorite && <span className="fav-star">★</span>}
            <div className="reflection" aria-hidden>
                {showArt ? <img src={src} alt="" draggable={false}/> : <div className="art-fallback"/>}
            </div>
        </div>
    )
}
