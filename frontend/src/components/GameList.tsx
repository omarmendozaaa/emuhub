import {memo, useEffect, useRef, useState} from 'react'
import type {main} from '../../wailsjs/go/models'
import {coverUrl, formatPlaytime, Game} from '../util'

const ROW_HEIGHT = 56
const OVERSCAN = 3

interface Props {
    games: Game[]
    focus: number
    consoles: Record<string, main.ConsoleInfo>
    onFocus: (i: number) => void
    onActivate: (i: number) => void
}

// Only the rows on screen are rendered, so libraries of any size stay light.
export function GameList({games, focus, consoles, onFocus, onActivate}: Props) {
    const ref = useRef<HTMLDivElement>(null)
    const [height, setHeight] = useState(0)

    useEffect(() => {
        const el = ref.current
        if (!el) return
        const observer = new ResizeObserver(() => setHeight(el.clientHeight))
        observer.observe(el)
        return () => observer.disconnect()
    }, [])

    // The focused row stays centred, except at the ends of the list.
    const maxTop = Math.max(0, games.length * ROW_HEIGHT - height)
    const top = Math.min(maxTop, Math.max(0, focus * ROW_HEIGHT - (height - ROW_HEIGHT) / 2))
    const first = Math.max(0, Math.floor(top / ROW_HEIGHT) - OVERSCAN)
    const last = Math.min(games.length - 1, Math.ceil((top + height) / ROW_HEIGHT) + OVERSCAN)

    const rows = []
    for (let i = first; i <= last; i++) {
        const g = games[i]
        rows.push(
            <Row
                key={g.id}
                game={g}
                y={i * ROW_HEIGHT - top}
                focused={i === focus}
                console={consoles[g.console]}
                onClick={() => (i === focus ? onActivate(i) : onFocus(i))}
            />,
        )
    }

    return (
        <div
            className="game-list"
            ref={ref}
            onWheel={e => {
                if (Math.abs(e.deltaY) < 4) return
                onFocus(Math.min(games.length - 1, Math.max(0, focus + Math.sign(e.deltaY))))
            }}
        >
            {rows}
        </div>
    )
}

const Row = memo(function Row({game, y, focused, console: c, onClick}: {
    game: Game, y: number, focused: boolean, console?: main.ConsoleInfo, onClick: () => void,
}) {
    const [broken, setBroken] = useState(false)
    const src = coverUrl(game)
    useEffect(() => setBroken(false), [src])

    return (
        <div
            className={'list-row' + (focused ? ' focused' : '')}
            style={{transform: `translateY(${y}px)`, height: ROW_HEIGHT, ['--accent' as string]: c?.accent ?? '#4a7dff'}}
            onClick={onClick}
        >
            <div className="list-thumb">
                {src && !broken && <img src={src} alt="" loading="lazy" decoding="async" draggable={false} onError={() => setBroken(true)}/>}
            </div>
            <span className="list-title">{game.title}</span>
            {game.favorite && <span className="list-star">★</span>}
            <span className="list-console">{c?.shortName ?? game.console}</span>
            <span className="list-time">{formatPlaytime(game.playSeconds)}</span>
        </div>
    )
})
