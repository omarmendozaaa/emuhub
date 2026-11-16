package bundle

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// SeatControls describes how an emulator's config assigns gamepads to
// players, so a room can decide who plays in which seat.
type SeatControls struct {
	// Sections are the config sections of players 1 and 2.
	Sections []string  `json:"sections"`
	Multitap *Multitap `json:"multitap,omitempty"`
}

// Multitap is the setting that lets more than two players in, and the config
// sections the players use while it is on.
type Multitap struct {
	Section  string   `json:"section"`
	Key      string   `json:"key"`
	On       string   `json:"on"`
	Off      string   `json:"off"`
	Sections []string `json:"sections"`
}

// seatBackupSuffix marks the copy of the config kept while a room has its
// own gamepad assignment in place.
const seatBackupSuffix = ".antes-de-sala"

// MaxSeats is how many players a room can arrange in this emulator, or 0 if
// EmuHub cannot assign its gamepads.
func (e *Emulator) MaxSeats() int {
	if e == nil || e.FirstRun == nil || e.FirstRun.Seats == nil || e.FirstRun.Controls == "" {
		return 0
	}
	s := e.FirstRun.Seats
	n := len(s.Sections)
	if s.Multitap != nil {
		n = max(n, len(s.Multitap.Sections))
	}
	return n
}

// ApplySeats rewrites the emulator's gamepad assignment for a room: pads[i]
// is the gamepad of player i+1 as the emulator numbers them, or -1 for a
// player without one. The previous config is kept aside until RestoreSeats.
func (e *Emulator) ApplySeats(pads []int) error {
	if len(pads) == 0 || len(pads) > e.MaxSeats() {
		return fmt.Errorf("%s no admite una sala de %d jugadores", e.Name, len(pads))
	}
	if err := e.RestoreSeats(); err != nil {
		return err
	}
	fr := e.FirstRun
	path := e.Expand(fr.Config, nil)
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	controls, err := seeds.ReadFile("seeds/" + fr.Controls)
	if err != nil {
		return err
	}
	template := firstPadBindings(string(controls))
	if len(template) == 0 {
		return errors.New("la plantilla de mandos no tiene asignaciones")
	}

	sections := fr.Seats.Sections
	managed := append([]string{}, fr.Seats.Sections...)
	updated := current
	if mt := fr.Seats.Multitap; mt != nil {
		value := mt.Off
		if len(pads) > len(fr.Seats.Sections) {
			sections, value = mt.Sections, mt.On
		}
		updated = setINIValue(updated, mt.Section, mt.Key, value)
		for _, s := range mt.Sections {
			if !contains(managed, s) {
				managed = append(managed, s)
			}
		}
	}
	for _, section := range managed {
		updated = dropINILines(updated, "["+section+"]", "SDL-")
		updated = setINIValue(updated, section, "Type", "None")
	}
	for i, pad := range pads {
		section := sections[i]
		updated = setINIValue(updated, section, "Type", "AnalogController")
		if pad < 0 {
			continue
		}
		entries := make([]string, len(template))
		for j, line := range template {
			entries[j] = strings.ReplaceAll(line, "SDL-0/", fmt.Sprintf("SDL-%d/", pad))
		}
		updated = addINILines(updated, "["+section+"]", entries)
	}

	if err := os.WriteFile(path+seatBackupSuffix, current, 0o644); err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o644)
}

// RestoreSeats puts back the config that ApplySeats set aside, if any. It is
// safe to call at any time, including after a crash left a room's
// assignment in place.
func (e *Emulator) RestoreSeats() error {
	if e.MaxSeats() == 0 {
		return nil
	}
	path := e.Expand(e.FirstRun.Config, nil)
	if _, err := os.Stat(path + seatBackupSuffix); err != nil {
		return nil
	}
	return os.Rename(path+seatBackupSuffix, path)
}

// firstPadBindings returns the gamepad bindings of the first section of a
// controls template, which are written for gamepad 0.
func firstPadBindings(controls string) []string {
	var out []string
	started := false
	for _, line := range strings.Split(controls, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			if started {
				break
			}
			started = true
			continue
		}
		if started && strings.Contains(t, "SDL-0/") {
			out = append(out, t)
		}
	}
	return out
}

// dropINILines removes the lines of a section that contain text.
func dropINILines(ini []byte, header, text string) []byte {
	lines := strings.Split(string(ini), "\n")
	out := lines[:0:0]
	in := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			in = t == header
		} else if in && strings.Contains(t, text) {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
