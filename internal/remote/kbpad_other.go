//go:build !linux

package remote

import "errors"

var errNoKeyboardPad = errors.New("jugar con teclado en una sala todavía solo está disponible en Linux")

// KeyboardPadSupported reports whether this computer can play with the
// keyboard, and why not.
func KeyboardPadSupported() error { return errNoKeyboardPad }

// StartKeyboardPad creates the pad and starts following the keyboard.
func StartKeyboardPad() (KeyboardPad, error) { return nil, errNoKeyboardPad }
