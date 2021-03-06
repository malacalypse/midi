package midi

// #include <stddef.h>
// #include <stdlib.h>
// #include <CoreMIDI/CoreMIDI.h>
// #include "midi_darwin.h"
// #cgo darwin LDFLAGS: -framework CoreFoundation -framework CoreMIDI
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/pkg/errors"
)

// Common errors.
var (
	ErrNotOpen = errors.New("Did you remember to open the device?")
)

var (
	connectionMutex  sync.RWMutex
	connectedDevices = map[*Device]chan []byte{}
)

// Device provides an interface for MIDI devices.
type Device struct {
	ID   string
	Name string
	Type DeviceType

	// QueueSize controls the buffer size of the read channel. Use 0 for blocking reads.
	QueueSize int

	conn   C.Midi
	input  C.MIDIEndpointRef
	output C.MIDIEndpointRef
}

// Open opens a MIDI device.
// queueSize is the number of packets to buffer in the channel associated with the device.
func (d *Device) Open() error {
	result := C.Midi_open(d.input, d.output)
	if result.error != 0 {
		fmt.Println("Error: ", result.error, result.loc)
		return coreMidiError(result.error)
	}
	d.conn = result.midi
	connectionMutex.Lock()
	connectedDevices[d] = make(chan []byte)
	connectionMutex.Unlock()
	return nil
}

// Close closes the connection to the MIDI device.
func (d *Device) Close() error {
	connectionMutex.Lock()
	connectedDevices[d] = nil
	connectionMutex.Unlock()
	return coreMidiError(C.OSStatus(C.Midi_close(d.conn)))
}

// Returns the device's channel for streaming raw MIDI bytes
// If the device has not been opened it will return ErrNotOpen.
func (d *Device) ReadChan() (<-chan []byte, error) {
	connectionMutex.RLock()
	defer connectionMutex.RUnlock()

	if connectedDevices[d] == nil {
		return nil, ErrNotOpen
	}
	return connectedDevices[d], nil
}

// Write writes data to a MIDI device.
func (d *Device) Write(buf []byte) (int, error) {
	result := C.Midi_write(d.conn, C.CString(string(buf)), C.size_t(len(buf)))
	if result.error != 0 {
		return 0, coreMidiError(result.error)
	}
	return len(buf), nil
}

//export SendPacket
func SendPacket(conn C.Midi, pkt *C.MIDIPacket) {
	var ch chan []byte
	// Attempt to identify connected device from C.Midi
	connectionMutex.RLock()
	for device, channel := range connectedDevices {
		if device.conn == conn {
			ch = channel
		}
	}
	connectionMutex.RUnlock()

	if ch == nil {
		return
	}
	data := make([]byte, 0, int(pkt.length))
	for i := 0; i < int(pkt.length); i++ {
		data = append(data, byte(pkt.data[i]))
	}
	ch <- data
}

// coreMidiError maps a CoreMIDI error code to a Go error.
func coreMidiError(code C.OSStatus) error {
	switch code {
	case 0:
		return nil
	case C.kMIDIInvalidClient:
		return errors.New("an invalid MIDIClientRef was passed")
	case C.kMIDIInvalidPort:
		return errors.New("an invalid MIDIPortRef was passed")
	case C.kMIDIWrongEndpointType:
		return errors.New("a source endpoint was passed to a function expecting a destination, or vice versa")
	case C.kMIDINoConnection:
		return errors.New("attempt to close a non-existant connection")
	case C.kMIDIUnknownEndpoint:
		return errors.New("an invalid MIDIEndpointRef was passed")
	case C.kMIDIUnknownProperty:
		return errors.New("attempt to query a property not set on the object")
	case C.kMIDIWrongPropertyType:
		return errors.New("attempt to set a property with a value not of the correct type")
	case C.kMIDINoCurrentSetup:
		return errors.New("there is no current MIDI setup object")
	case C.kMIDIMessageSendErr:
		return errors.New("communication with MIDIServer failed")
	case C.kMIDIServerStartErr:
		return errors.New("unable to start MIDIServer")
	case C.kMIDISetupFormatErr:
		return errors.New("unable to read the saved state")
	case C.kMIDIWrongThread:
		return errors.New("a driver is calling a non-I/O function in the server from a thread other than the server's main thread")
	case C.kMIDIObjectNotFound:
		return errors.New("the requested object does not exist")
	case C.kMIDIIDNotUnique:
		return errors.New("attempt to set a non-unique kMIDIPropertyUniqueID on an object")
	case C.kMIDINotPermitted:
		return errors.New("attempt to perform an operation that is not permitted")
	case -10900:
		// See Midi_write in midi_darwin.c if you're curious where the number comes from.
		// [briansorahan]
		// I tried to add a const to midi_darwin.h for this number,
		// but it resulted in link errors:
		// duplicate symbol _kInsufficientSpaceInPacket in:
		//     $WORK/github.com/scgolang/midi/_test/_obj_test/_cgo_export.o
		//     $WORK/github.com/scgolang/midi/_test/_obj_test/midi_darwin.cgo2.o
		// duplicate symbol _kInsufficientSpaceInPacket in:
		//     $WORK/github.com/scgolang/midi/_test/_obj_test/_cgo_export.o
		//     $WORK/github.com/scgolang/midi/_test/_obj_test/midi_darwin.o
		// ld: 2 duplicate symbols for architecture x86_64
		return errors.New("insufficient space in packet")
	default:
		return errors.Errorf("unknown CoreMIDI error: %d", code)
	}
}

// Devices returns a list of devices.
func Devices() ([]*Device, error) {
	var (
		maxEndpoints    C.ItemCount
		devices                     = []*Device{}
		numDestinations C.ItemCount = C.MIDIGetNumberOfDestinations()
		numSources      C.ItemCount = C.MIDIGetNumberOfSources()
	)
	if numDestinations > numSources {
		maxEndpoints = numDestinations
	} else {
		maxEndpoints = numSources
	}
	for i := C.ItemCount(0); i < maxEndpoints; i++ {
		var (
			d   *Device
			obj C.MIDIObjectRef
		)
		if i < numDestinations && i < numSources {
			d = &Device{
				Type:   DeviceDuplex,
				input:  C.MIDIGetSource(i),
				output: C.MIDIGetDestination(i),
			}
			obj = C.MIDIObjectRef(d.output)
		} else if i < numDestinations {
			d = &Device{
				Type:   DeviceOutput,
				output: C.MIDIGetDestination(i),
			}
			obj = C.MIDIObjectRef(d.output)
		} else {
			d = &Device{
				Type:  DeviceInput,
				input: C.MIDIGetSource(i),
			}
			obj = C.MIDIObjectRef(d.input)
		}
		var name C.CFStringRef
		if rc := C.MIDIObjectGetStringProperty(obj, C.kMIDIPropertyName, &name); rc != 0 {
			return nil, coreMidiError(rc)
		}
		d.Name = fromCFString(name)
		C.CFRelease(C.CFTypeRef(name))
		devices = append(devices, d)
	}
	return devices, nil
}

func fromCFString(cfs C.CFStringRef) string {
	var (
		cs = C.CFStringToUTF8(cfs)
		gs = C.GoString(cs)
	)
	C.free(unsafe.Pointer(cs))
	return gs
}
