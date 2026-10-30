type Kind = 'cross' | 'circle' | 'triangle' | 'square' | 'options' | 'share' | 'l1' | 'r1'

const colors: Record<string, string> = {
    cross: '#7fb2ff', circle: '#ff7a8a', triangle: '#4fe0b5', square: '#e59cff',
}

export function Glyph({kind, size = 22}: { kind: Kind, size?: number }) {
    if (kind === 'options' || kind === 'share' || kind === 'l1' || kind === 'r1') {
        const label = {options: 'OPTIONS', share: 'MENU', l1: 'L1', r1: 'R1'}[kind]
        return <span className="glyph-pill" style={{height: size * 0.82}}>{label}</span>
    }
    const c = colors[kind]
    return (
        <svg className="glyph" width={size} height={size} viewBox="0 0 24 24" aria-hidden>
            <circle cx="12" cy="12" r="11" fill="rgba(255,255,255,0.08)" stroke="rgba(255,255,255,0.85)" strokeWidth="1.3"/>
            {kind === 'cross' && <path d="M7.8 7.8l8.4 8.4M16.2 7.8l-8.4 8.4" stroke={c} strokeWidth="1.9" strokeLinecap="round"/>}
            {kind === 'circle' && <circle cx="12" cy="12" r="5" fill="none" stroke={c} strokeWidth="1.9"/>}
            {kind === 'triangle' && <path d="M12 6.8l5.4 9.2H6.6z" fill="none" stroke={c} strokeWidth="1.8" strokeLinejoin="round"/>}
            {kind === 'square' && <rect x="7.3" y="7.3" width="9.4" height="9.4" fill="none" stroke={c} strokeWidth="1.8" rx="0.6"/>}
        </svg>
    )
}

export function Hint({kind, label, onClick}: { kind: Kind, label: string, onClick?: () => void }) {
    return (
        <button className="hint" onClick={onClick} tabIndex={-1}>
            <Glyph kind={kind}/>
            <span>{label}</span>
        </button>
    )
}
