package remote

import (
	"os"
	"reflect"
	"testing"
)

func TestParseSinkInputs(t *testing.T) {
	out := `Sink Input #1681
	Driver: PipeWire
	Sink: 60
	Properties:
		node.name = "output.loopback-1146-13"
Sink Input #2190
	Sink: 60
	Properties:
		application.name = "RMG"
		application.process.id = "163584"
`
	want := []sinkInput{{index: "1681"}, {index: "2190", pid: 163584}}
	if got := parseSinkInputs(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestDescends(t *testing.T) {
	self, parent := os.Getpid(), os.Getppid()
	if !descends(self, self) || !descends(self, parent) {
		t.Fatal("this process should descend from itself and its parent")
	}
	if descends(parent, self) || descends(0, self) {
		t.Fatal("a parent does not descend from its child")
	}
}
