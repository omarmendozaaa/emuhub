package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func section(ini, name string) string {
	_, rest, ok := strings.Cut(ini, "["+name+"]\n")
	if !ok {
		return ""
	}
	if i := strings.Index(rest, "\n["); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func TestApplySeats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duckstation", "settings.ini")
	emu := &Emulator{Name: "DuckStation", Platform: Platform{FirstRun: &FirstRun{
		Config: path, Template: "duckstation.ini", Section: "Main", Key: "SetupWizardIncomplete", Value: "false",
		Controls: "duckstation-controls.ini",
		Seats: &SeatControls{
			Sections: []string{"Pad1", "Pad2"},
			Multitap: &Multitap{Section: "ControllerPorts", Key: "MultitapMode", On: "Port1Only", Off: "Disabled", Sections: []string{"Pad1", "Pad3", "Pad4", "Pad5"}},
		},
	}}}
	if emu.MaxSeats() != 4 {
		t.Fatalf("MaxSeats = %d", emu.MaxSeats())
	}
	if err := emu.PrepareFirstRun(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	read := func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// Two players, the host only hosts with a pad of their own plugged in.
	if err := emu.ApplySeats([]int{1, 2}); err != nil {
		t.Fatal(err)
	}
	ini := read()
	if p := section(ini, "Pad1"); !strings.Contains(p, "Cross = SDL-1/A") || strings.Contains(p, "SDL-0/") || !strings.Contains(p, "Cross = Keyboard/") {
		t.Errorf("Pad1:\n%s", p)
	}
	if p := section(ini, "Pad2"); !strings.Contains(p, "Start = SDL-2/Start") || !strings.Contains(p, "Type = AnalogController") {
		t.Errorf("Pad2:\n%s", p)
	}
	if !strings.Contains(section(ini, "ControllerPorts"), "MultitapMode = Disabled") {
		t.Error("multitap debería estar desactivado con 2 jugadores")
	}
	if !strings.Contains(section(ini, "Hotkeys"), "OpenPauseMenu = SDL-0/Guide") {
		t.Error("los atajos no deben tocarse")
	}

	// Four players switch to the multitap layout, on top of the room before.
	if err := emu.ApplySeats([]int{0, 3, -1, 1}); err != nil {
		t.Fatal(err)
	}
	ini = read()
	if !strings.Contains(section(ini, "ControllerPorts"), "MultitapMode = Port1Only") {
		t.Error("multitap debería activarse con 4 jugadores")
	}
	for name, want := range map[string]string{"Pad1": "SDL-0/A", "Pad3": "SDL-3/A", "Pad5": "SDL-1/A"} {
		if p := section(ini, name); !strings.Contains(p, "Cross = "+want) || !strings.Contains(p, "Type = AnalogController") {
			t.Errorf("%s:\n%s", name, p)
		}
	}
	if p := section(ini, "Pad4"); strings.Contains(p, "SDL-") || !strings.Contains(p, "Type = AnalogController") {
		t.Errorf("Pad4, jugador sin mando:\n%s", p)
	}
	if p := section(ini, "Pad2"); strings.Contains(p, "SDL-") || !strings.Contains(p, "Type = None") {
		t.Errorf("Pad2 no se usa con multitap en el puerto 1:\n%s", p)
	}

	if err := emu.ApplySeats(make([]int, 5)); err == nil {
		t.Error("5 jugadores deberían rechazarse")
	}

	// Closing the room leaves the config as it was.
	if err := emu.RestoreSeats(); err != nil {
		t.Fatal(err)
	}
	if read() != string(before) {
		t.Error("la configuración no volvió a su estado anterior")
	}
	if _, err := os.Stat(path + seatBackupSuffix); err == nil {
		t.Error("quedó la copia de seguridad")
	}
	if err := emu.RestoreSeats(); err != nil {
		t.Error(err)
	}
}
