package library

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestBuildCommand(t *testing.T) {
	cases := []struct {
		tpl, rom string
		want     []string
	}{
		{"pcsx2-qt -batch -fullscreen -- {rom}", "/g/a b.iso", []string{"pcsx2-qt", "-batch", "-fullscreen", "--", "/g/a b.iso"}},
		{`"/opt/My Emu/emu" -f`, "/g/x.sfc", []string{"/opt/My Emu/emu", "-f", "/g/x.sfc"}},
		{"retroarch -L '/usr/lib/libretro/snes9x_libretro.so' --rom={rom}", "/r.sfc", []string{"retroarch", "-L", "/usr/lib/libretro/snes9x_libretro.so", "--rom=/r.sfc"}},
	}
	for _, c := range cases {
		if got := BuildCommand(c.tpl, c.rom); !reflect.DeepEqual(got, c.want) {
			t.Errorf("BuildCommand(%q) = %q, want %q", c.tpl, got, c.want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Super Mario World (USA) [!]":      "Super Mario World",
		"Final Fantasy VII (USA) (Disc 1)": "Final Fantasy VII",
		"Legend_of_Zelda":                  "Legend of Zelda",
		"(Prototype)":                      "(Prototype)",
	} {
		if got := CleanTitle(in); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeSFO(t *testing.T, path string, kv map[string]string) {
	t.Helper()
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var keyTab, dataTab bytes.Buffer
	var entries bytes.Buffer
	le := binary.LittleEndian
	for _, k := range keys {
		v := append([]byte(kv[k]), 0)
		binary.Write(&entries, le, uint16(keyTab.Len()))
		binary.Write(&entries, le, uint16(0x0204))
		binary.Write(&entries, le, uint32(len(v)))
		binary.Write(&entries, le, uint32(len(v)))
		binary.Write(&entries, le, uint32(dataTab.Len()))
		keyTab.WriteString(k + "\x00")
		dataTab.Write(v)
	}
	var out bytes.Buffer
	out.WriteString("\x00PSF")
	binary.Write(&out, le, uint32(0x101))
	keyStart := 20 + entries.Len()
	binary.Write(&out, le, uint32(keyStart))
	binary.Write(&out, le, uint32(keyStart+keyTab.Len()))
	binary.Write(&out, le, uint32(len(keys)))
	out.Write(entries.Bytes())
	out.Write(keyTab.Bytes())
	out.Write(dataTab.Bytes())
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanPS1SkipsTracks(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "FF7/Final Fantasy VII (Disc 1).cue"), `FILE "Final Fantasy VII (Disc 1).bin" BINARY`)
	touch(t, filepath.Join(root, "FF7/Final Fantasy VII (Disc 1).bin"), "")
	touch(t, filepath.Join(root, "Crash Bandicoot (USA).chd"), "")
	touch(t, filepath.Join(root, "Crash Bandicoot (USA).png"), "")
	touch(t, filepath.Join(root, "notes.txt"), "")

	games := Scan(ConsoleByID("ps1"), []string{root})
	if len(games) != 2 {
		t.Fatalf("got %d games, want 2: %+v", len(games), games)
	}
	for _, g := range games {
		if g.Title == "Crash Bandicoot" && g.Cover == "" {
			t.Error("sibling cover not picked up")
		}
	}
}

func TestScanPS3Folders(t *testing.T) {
	root := t.TempDir()
	disc := filepath.Join(root, "Demon's Souls", "PS3_GAME")
	writeSFO(t, filepath.Join(disc, "PARAM.SFO"), map[string]string{"TITLE": "Demon's Souls", "TITLE_ID": "BLUS30443", "CATEGORY": "DG"})
	touch(t, filepath.Join(disc, "USRDIR/EBOOT.BIN"), "")
	touch(t, filepath.Join(disc, "ICON0.PNG"), "")

	update := filepath.Join(root, "BLUS30443")
	writeSFO(t, filepath.Join(update, "PARAM.SFO"), map[string]string{"TITLE": "Demon's Souls", "TITLE_ID": "BLUS30443", "CATEGORY": "GD"})
	touch(t, filepath.Join(update, "USRDIR/EBOOT.BIN"), "")

	games := Scan(ConsoleByID("ps3"), []string{root})
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1: %+v", len(games), games)
	}
	g := games[0]
	if g.Title != "Demon's Souls" || g.Serial != "BLUS30443" || g.Cover == "" {
		t.Errorf("unexpected game: %+v", g)
	}
}
