package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetINIValue(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"replace", "[Main]\nA = 1\nWizard = true\n\n[Other]\nWizard = true\n", "[Main]\nA = 1\nWizard = false\n\n[Other]\nWizard = true\n"},
		{"crlf", "[Main]\r\nWizard = true\r\n", "[Main]\r\nWizard = false\r\n"},
		{"add key", "[Main]\nA = 1\n", "[Main]\nWizard = false\nA = 1\n"},
		{"other section only", "[Other]\nWizard = true\n", "[Other]\nWizard = true\n\n[Main]\nWizard = false\n"},
	}
	for _, c := range cases {
		if got := string(setINIValue([]byte(c.in), "Main", "Wizard", "false")); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPrepareFirstRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duckstation", "settings.ini")
	emu := &Emulator{Platform: Platform{FirstRun: &FirstRun{
		Config: path, Template: "duckstation.ini", Section: "Main", Key: "SetupWizardIncomplete", Value: "false",
		Controls: "duckstation-controls.ini", ScreensaverKey: "InhibitScreensaver",
	}}}
	available, known := false, true
	screensaverService = func() (bool, bool) { return available, known }
	t.Cleanup(func() { screensaverService = defaultScreensaverService })

	// A fresh install gets the complete template, controls included.
	if err := emu.PrepareFirstRun(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SetupWizardIncomplete = false", "CheckAtStartup = false", "InhibitScreensaver = false",
		"Cross = Keyboard/K", "Cross = SDL-0/A", "L2 = SDL-0/+LeftTrigger", "LUp = SDL-0/-LeftY", "OpenPauseMenu = Keyboard/Escape", "OpenPauseMenu = SDL-0/Guide",
		"[Pad2]\nUp = SDL-1/DPadUp", "SmallMotor = SDL-1/SmallMotor\nType = AnalogController\n",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("template lacks %q", want)
		}
	}

	if strings.Count(string(b), "[Pad2]") != 1 || strings.Contains(string(b), "Type = None\n\n[Pad3]") {
		t.Error("the second pad is not enabled exactly once")
	}

	// An abandoned wizard is cleared without losing the user's settings.
	// Gamepad bindings the user already set are not touched.
	existing := "[Main]\nSetupWizardIncomplete = true\nInhibitScreensaver = false\nMine = 1\n\n[Pad1]\nCross = SDL-0/B\n\n[Pad2]\nType = None\nCross = SDL-3/A\n\n[Hotkeys]\nOpenPauseMenu = SDL-1/Start\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	available = true
	if err := emu.PrepareFirstRun(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	want := "[Main]\nSetupWizardIncomplete = false\nInhibitScreensaver = true\nMine = 1\n\n[Pad1]\nCross = SDL-0/B\n\n[Pad2]\nType = None\nCross = SDL-3/A\n\n[Hotkeys]\nOpenPauseMenu = SDL-1/Start\n"
	if got := string(b); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Without a way to tell, the emulator's own setting stays.
	known = false
	available = false
	if err := emu.PrepareFirstRun(); err != nil {
		t.Fatal(err)
	}
	if b, _ = os.ReadFile(path); string(b) != want {
		t.Errorf("screensaver changed: %q", b)
	}
}

func TestAddGamepadBindingsKeepsKeyboard(t *testing.T) {
	got := string(addGamepadBindings([]byte("[Pad1]\nCross = Keyboard/K\n"), []byte("[Pad1]\nCross = SDL-0/A\n\n[Hotkeys]\nX = SDL-0/Y\n")))
	want := "[Pad1]\nCross = SDL-0/A\nCross = Keyboard/K\n\n[Hotkeys]\nX = SDL-0/Y\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAddGamepadBindingsReplacesSettings(t *testing.T) {
	controls := []byte("[Pad2]\nType = AnalogController\nCross = SDL-1/A\n")
	got := string(addGamepadBindings([]byte("[Pad2]\nType = None\n\n[Pad3]\nType = None\n"), controls))
	want := "[Pad2]\nCross = SDL-1/A\nType = AnalogController\n\n[Pad3]\nType = None\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A config written before EmuHub knew about the second pad gains it.
	got = string(addGamepadBindings([]byte("[Pad1]\nCross = SDL-0/A\n"), controls))
	want = "[Pad1]\nCross = SDL-0/A\n\n[Pad2]\nType = AnalogController\nCross = SDL-1/A\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDuckStationDir(t *testing.T) {
	t.Setenv("HOME", "/h")
	emu := &Emulator{}
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if got := emu.Expand("{duckstationdir}/bios", nil); got != filepath.FromSlash("/cfg/duckstation/bios") {
		t.Errorf("with XDG_CONFIG_HOME: %s", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	// XDG_DATA_HOME is not consulted by DuckStation.
	t.Setenv("XDG_DATA_HOME", "/data")
	if got := emu.Expand("{duckstationdir}/bios", nil); got != filepath.FromSlash("/h/.local/share/duckstation/bios") {
		t.Errorf("without XDG_CONFIG_HOME: %s", got)
	}
}
