//go:build !linux

package netsniff

import (
	"errors"
	"testing"
	"time"
)

func TestStartReturnsUnsupportedError(t *testing.T) {
	sniffer := NewSniffer()
	if err := sniffer.Start("test0"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Start() error = %v, want ErrUnsupported", err)
	}

	sniffer.Stop()
	sniffer.Stop() // Stop is idempotent, including after a failed Start.
	select {
	case _, ok := <-sniffer.Packets():
		if ok {
			t.Fatal("Packets channel remained open after Stop")
		}
	case <-time.After(time.Second):
		t.Fatal("Packets channel was not closed after Stop")
	}
}
