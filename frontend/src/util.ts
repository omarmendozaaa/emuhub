import type {library} from '../wailsjs/go/models'

export type Game = library.Game

export function coverUrl(g: Game) {
    return g.hasCover ? `/media/cover/${g.id}?r=${g.coverRev}` : ''
}

export function backdropUrl(g: Game) {
    if (g.hasBackdrop) return `/media/backdrop/${g.id}`
    return coverUrl(g)
}

export function formatPlaytime(seconds: number) {
    if (!seconds) return 'Sin jugar'
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (h === 0) return m === 0 ? '< 1 min' : `${m} min`
    return m === 0 ? `${h} h` : `${h} h ${m} min`
}

export function formatAgo(unix: number) {
    if (!unix) return ''
    const diff = Date.now() / 1000 - unix
    if (diff < 60) return 'ahora mismo'
    if (diff < 3600) return `hace ${Math.floor(diff / 60)} min`
    if (diff < 86400) return `hace ${Math.floor(diff / 3600)} h`
    const d = Math.floor(diff / 86400)
    return d === 1 ? 'ayer' : `hace ${d} días`
}

export function errorText(e: unknown) {
    return typeof e === 'string' ? e : e instanceof Error ? e.message : String(e)
}
