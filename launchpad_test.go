package midi

import (
	"fmt"
	"testing"
	"time"
)

func TestLaunchpad(t *testing.T) {
	// This test can be run if you have a MIDI device with ID "hw:1,0,0"
	// It will tell you if this package is able to talk to your device.
	// The messages that are sent here are specific to the Novation Launchpad Mini.
	// The reason this package exists is because of issues that popped up when
	// trying to use github.com/rakyll/portmidi to talk to the launchpad on Linux.
	// For the launchpad MIDI reference, see https://d19ulaff0trnck.cloudfront.net/sites/default/files/novation/downloads/4080/launchpad-programmers-reference.pdf
	// t.SkipNow()

	device, err := Open("101478690", "Launchpad Mini")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := device.Write([]byte{0xB0, 0x00, 0x00}); err != nil {
		t.Fatal(err)
	}
	if _, err := device.Write([]byte{0xB0, 0x00, 0x7D}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Second)

	if _, err := device.Write([]byte{0xB0, 0x00, 0x00}); err != nil {
		t.Fatal(err)
	}

	// Test hangs here until you send some MIDI data!
	packet := <-device.Packets()
	fmt.Printf("packet %#v\n", packet)

	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
}
