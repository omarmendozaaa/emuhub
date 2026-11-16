//go:build !linux

package remote

import "errors"

// gameAudio would route only the game's sound to guests. Outside Linux there
// is no simple way to give one program its own output, so Sunshine shares
// the whole desktop's audio.
type gameAudio struct{}

func startGameAudio() (*gameAudio, error) {
	return nil, errors.New("audio separado no disponible en este sistema")
}

func (g *gameAudio) sink() string  { return "" }
func (g *gameAudio) env() []string { return nil }
func (g *gameAudio) stop()         {}
func (g *gameAudio) claim(int)     {}
