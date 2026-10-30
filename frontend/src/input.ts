import {useEffect, useRef} from 'react'

export type Action =
    | 'left' | 'right' | 'up' | 'down'
    | 'confirm' | 'back' | 'triangle' | 'square'
    | 'options' | 'share' | 'l1' | 'r1'

const keyMap: Record<string, Action> = {
    ArrowLeft: 'left', ArrowRight: 'right', ArrowUp: 'up', ArrowDown: 'down',
    a: 'left', d: 'right', w: 'up', s: 'down',
    Enter: 'confirm', ' ': 'confirm',
    Escape: 'back', Backspace: 'back',
    r: 'triangle', f: 'square',
    o: 'options', F2: 'options',
    m: 'share', ContextMenu: 'share',
    q: 'l1', e: 'r1', PageUp: 'l1', PageDown: 'r1',
}

// Standard gamepad mapping (Xbox / DualShock / DualSense via SDL/libmanette).
const buttonMap: Record<number, Action> = {
    0: 'confirm', 1: 'back', 2: 'square', 3: 'triangle',
    4: 'l1', 5: 'r1', 8: 'share', 9: 'options',
    12: 'up', 13: 'down', 14: 'left', 15: 'right',
}

const REPEATABLE = new Set<Action>(['left', 'right', 'up', 'down'])
const REPEAT_DELAY = 380
const REPEAT_RATE = 95

/**
 * Calls handler for keyboard and gamepad input. Directions auto-repeat while
 * held. Keys typed into form fields are ignored so text inputs still work.
 */
export function useInput(handler: (a: Action) => void) {
    const ref = useRef(handler)
    ref.current = handler

    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            const t = e.target as HTMLElement
            if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) {
                if (e.key === 'Escape') (t as HTMLInputElement).blur()
                return
            }
            const action = keyMap[e.key] ?? keyMap[e.key.toLowerCase()]
            if (!action) return
            if (t?.tagName === 'BUTTON' && action === 'confirm') return
            e.preventDefault()
            ref.current(action)
        }
        window.addEventListener('keydown', onKey)

        const held = new Map<Action, number>() // action -> next fire time
        let raf = 0
        const poll = () => {
            raf = requestAnimationFrame(poll)
            const pads = navigator.getGamepads ? navigator.getGamepads() : []
            const active = new Set<Action>()
            for (const pad of pads) {
                if (!pad) continue
                pad.buttons.forEach((b, i) => {
                    const a = buttonMap[i]
                    if (a && b.pressed) active.add(a)
                })
                const [x = 0, y = 0] = pad.axes
                if (x < -0.55) active.add('left')
                if (x > 0.55) active.add('right')
                if (y < -0.55) active.add('up')
                if (y > 0.55) active.add('down')
            }
            const now = performance.now()
            for (const a of active) {
                const next = held.get(a)
                if (next === undefined) {
                    held.set(a, now + REPEAT_DELAY)
                    ref.current(a)
                } else if (REPEATABLE.has(a) && now >= next) {
                    held.set(a, now + REPEAT_RATE)
                    ref.current(a)
                }
            }
            for (const a of [...held.keys()]) if (!active.has(a)) held.delete(a)
        }
        raf = requestAnimationFrame(poll)

        return () => {
            window.removeEventListener('keydown', onKey)
            cancelAnimationFrame(raf)
        }
    }, [])
}

let audio: AudioContext | null = null

/** Short UI blip, in the spirit of console menu sounds. */
export function blip(kind: 'move' | 'ok' | 'back' = 'move') {
    try {
        audio ??= new AudioContext()
        const t = audio.currentTime
        const osc = audio.createOscillator()
        const gain = audio.createGain()
        const [f0, f1, dur, vol] = kind === 'move' ? [1400, 1100, 0.05, 0.035]
            : kind === 'ok' ? [660, 1320, 0.16, 0.06] : [700, 380, 0.12, 0.05]
        osc.type = 'sine'
        osc.frequency.setValueAtTime(f0, t)
        osc.frequency.exponentialRampToValueAtTime(f1, t + dur)
        gain.gain.setValueAtTime(vol, t)
        gain.gain.exponentialRampToValueAtTime(0.0001, t + dur)
        osc.connect(gain).connect(audio.destination)
        osc.start(t)
        osc.stop(t + dur)
    } catch {
        // audio is a nicety; ignore failures
    }
}
