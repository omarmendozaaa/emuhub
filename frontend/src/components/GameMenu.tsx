import {Game} from '../util'

export interface MenuItem {
    label: string
    danger?: boolean
    run: () => void
}

export function gameMenuItems(g: Game, h: {
    play: () => void, favorite: () => void, cover: () => void, remove: () => void, close: () => void,
}): MenuItem[] {
    return [
        {label: 'Jugar', run: h.play},
        {label: g.favorite ? 'Quitar de favoritos' : 'Añadir a favoritos', run: h.favorite},
        {label: 'Cambiar portada…', run: h.cover},
        {label: 'Quitar de la biblioteca', danger: true, run: h.remove},
        {label: 'Cancelar', run: h.close},
    ]
}

export function GameMenu({game, items, index, onPick, onClose}: {
    game: Game, items: MenuItem[], index: number, onPick: (i: number) => void, onClose: () => void,
}) {
    return (
        <div className="overlay light" onMouseDown={e => e.target === e.currentTarget && onClose()}>
            <div className="game-menu">
                <div className="game-menu-title">{game.title}</div>
                <div className="game-menu-path" title={game.path}>{game.path}</div>
                {items.map((it, i) => (
                    <button
                        key={it.label}
                        className={(i === index ? 'active ' : '') + (it.danger ? 'danger' : '')}
                        onClick={() => onPick(i)}
                    >
                        {it.label}
                    </button>
                ))}
            </div>
        </div>
    )
}
