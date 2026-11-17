package remote

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gameSinkName is the virtual sink the game plays into while a room is open.
// Sunshine sends guests only this sink, so they do not also hear the rest of
// the host's desktop (a voice chat they are already in, for example).
const gameSinkName = "emuhub_juego"

// gameAudio is the virtual sink of a room and the loopback that lets the
// host keep hearing the game on their own speakers.
type gameAudio struct {
	modules  []string
	speakers string
	watch    context.CancelFunc

	mu    sync.Mutex
	games []int // processes whose sound goes to the room's sink
}

// startGameAudio creates the room's sink through PulseAudio or PipeWire's
// PulseAudio server. Without pactl the room shares the whole desktop's audio.
func startGameAudio() (*gameAudio, error) {
	if _, err := exec.LookPath("pactl"); err != nil {
		return nil, errors.New("pactl no está instalado")
	}
	// A room that ended badly may have left its modules behind.
	unloadGameAudio()

	speakers := strings.TrimSpace(pactlOutput("get-default-sink"))
	if speakers == "" || speakers == gameSinkName {
		return nil, errors.New("no hay una salida de audio por defecto")
	}
	g := &gameAudio{speakers: speakers}
	sink, err := pactlLoad("module-null-sink", "sink_name="+gameSinkName,
		"sink_properties=device.description=EmuHub-juego", "rate=48000", "channels=2")
	if err != nil {
		return nil, err
	}
	g.modules = append(g.modules, sink)
	// The new sink must not take over the desktop's audio: everything else
	// would then reach the guests too.
	if strings.TrimSpace(pactlOutput("get-default-sink")) != speakers {
		pactl("set-default-sink", speakers)
	}
	loop, err := pactlLoad("module-loopback", "source="+gameSinkName+".monitor", "sink="+speakers,
		"latency_msec=30", "source_dont_move=true")
	if err != nil {
		g.stop()
		return nil, err
	}
	g.modules = append(g.modules, loop)
	g.watchServer()
	return g, nil
}

// watchServer follows the sound server while the room is open:
//
//   - Sunshine makes the room's sink the default output when a guest starts
//     streaming, which would move every program that follows the default,
//     voice chat included, into the sink guests hear. The speakers are put
//     back at once; Sunshine keeps capturing the sink by name.
//   - Every time a stream appears or changes, the game's streams are put in
//     the room's sink and everything else is taken out of it. Emulators ask
//     for the default output by name, the sound server restores the output
//     it remembers for them, and SDL reopens its stream whenever the default
//     output changes, so moving the game's stream once is not enough.
func (g *gameAudio) watchServer() {
	ctx, cancel := context.WithCancel(context.Background())
	g.watch = cancel
	restore := func() {
		if strings.TrimSpace(pactlOutput("get-default-sink")) == gameSinkName {
			pactl("set-default-sink", g.speakers)
		}
	}
	go func() {
		for ctx.Err() == nil {
			cmd := exec.CommandContext(ctx, "pactl", "subscribe")
			out, err := cmd.StdoutPipe()
			if err == nil && cmd.Start() == nil {
				restore()
				g.sortStreams()
				lines := bufio.NewScanner(out)
				for lines.Scan() {
					switch line := lines.Text(); {
					case strings.Contains(line, "on server"):
						restore()
						g.sortStreams()
					case strings.Contains(line, "'new' on sink-input"), strings.Contains(line, "'change' on sink-input"):
						g.sortStreams()
					}
				}
				cmd.Wait()
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}()
}

// claim sends the sound of a process and its children to the room's sink.
func (g *gameAudio) claim(pid int) {
	g.mu.Lock()
	g.games = append(g.games, pid)
	g.mu.Unlock()
	go g.sortStreams()
}

// sortStreams puts the claimed processes' playback in the room's sink and
// moves anything else that ended up there back to the speakers, so guests
// hear only the game.
func (g *gameAudio) sortStreams() {
	g.mu.Lock()
	games := append([]int{}, g.games...)
	g.mu.Unlock()
	room := sinkIndex(pactlOutput("list", "short", "sinks"), gameSinkName)
	if room == "" {
		return
	}
	for _, s := range parseSinkInputs(pactlOutput("list", "sink-inputs")) {
		game := false
		for _, root := range games {
			if descends(s.pid, root) {
				game = true
				break
			}
		}
		switch {
		case game && s.sink != room:
			pactl("move-sink-input", s.index, gameSinkName)
		case !game && s.sink == room:
			pactl("move-sink-input", s.index, g.speakers)
		}
	}
}

type sinkInput struct {
	index string
	sink  string
	pid   int
}

// parseSinkInputs reads the playback streams out of `pactl list sink-inputs`.
func parseSinkInputs(out string) []sinkInput {
	var list []sinkInput
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if n, ok := strings.CutPrefix(t, "Sink Input #"); ok {
			list = append(list, sinkInput{index: n})
			continue
		}
		if len(list) == 0 {
			continue
		}
		if v, ok := strings.CutPrefix(t, "Sink: "); ok {
			list[len(list)-1].sink = v
		}
		if v, ok := strings.CutPrefix(t, "application.process.id = "); ok {
			list[len(list)-1].pid, _ = strconv.Atoi(strings.Trim(v, `"`))
		}
	}
	return list
}

// sinkIndex finds a sink's index in `pactl list short sinks`.
func sinkIndex(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[1] == name {
			return f[0]
		}
	}
	return ""
}

// descends reports whether pid is root or one of its descendants.
func descends(pid, root int) bool {
	for i := 0; pid > 1 && i < 64; i++ {
		if pid == root {
			return true
		}
		pid = parentPID(pid)
	}
	return false
}

func parentPID(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	// The command name, in parentheses, may itself contain spaces.
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 {
		s = s[i+1:]
	}
	f := strings.Fields(s)
	if len(f) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(f[1])
	return ppid
}

// sink is the name Sunshine captures from.
func (g *gameAudio) sink() string { return gameSinkName }

// env sends the game's sound to the room's sink, for PulseAudio and native
// PipeWire clients alike.
func (g *gameAudio) env() []string {
	return []string{"PULSE_SINK=" + gameSinkName, "PIPEWIRE_NODE=" + gameSinkName}
}

// stop removes the sink. A game still playing moves to the default output.
func (g *gameAudio) stop() {
	if g.watch != nil {
		g.watch()
	}
	for i := len(g.modules) - 1; i >= 0; i-- {
		pactl("unload-module", g.modules[i])
	}
	g.modules = nil
}

// unloadGameAudio removes any module that belongs to a room's sink.
func unloadGameAudio() {
	for _, line := range strings.Split(pactlOutput("list", "short", "modules"), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && strings.Contains(line, gameSinkName) {
			pactl("unload-module", fields[0])
		}
	}
}

func pactlLoad(module string, args ...string) (string, error) {
	out, err := pactlRun(append([]string{"load-module", module}, args...)...)
	id := strings.TrimSpace(out)
	if err != nil || id == "" {
		return "", errors.New("pactl no pudo cargar " + module + ": " + strings.TrimSpace(out))
	}
	return id, nil
}

func pactl(args ...string)              { pactlRun(args...) }
func pactlOutput(args ...string) string { out, _ := pactlRun(args...); return out }
func pactlRun(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pactl", args...).CombinedOutput()
	return string(out), err
}
