package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/malacalypse/midi"
	"os"
	"strconv"
)

// TODO: Implement using range on channels with deferred close() and done() signalling
//         per blog.golang.org/pipelines. Might reduce the spin BS.
//       Also try to clean it up a bit more.
func main() {
	var nl3 *midi.Device
	var idxConn int

	if len(os.Args) > 1 {
		if idx, err := strconv.Atoi(os.Args[1]); err == nil {
			idxConn = idx
		}
	}

	devices, err := midi.Devices()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Midi Devices: ")
	for i, device := range devices {
		if i == idxConn {
			fmt.Println(i, "**", device.Name, device.ID, dt(device.Type), device.QueueSize)
			nl3 = device
		} else {
			fmt.Println(i, device.Name, device.ID, dt(device.Type), device.QueueSize)
		}
	}

	scanning := false
	runningProcs := 0
	quitmode := false
	qch := make(chan int)
	cq := make(chan int)
	control := make(chan string)

	go listenForCommands(control)

	for {
		if quitmode && runningProcs == 0 {
			return
		}

		select {
		case str := <-control:
			switch str {
			case "q":
				quitmode = true
				for i := 0; i < runningProcs; i++ {
					qch <- 1
				}
			case "s":
				if !scanning {
					scanning = true
					dumpch := make(chan []byte)
					go importSysex(qch, cq, dumpch)
					go listenForSysex(nl3, qch, cq, dumpch, splitSysex(0x33, 0x09))
					runningProcs += 2
				} else {
					fmt.Println("already scanning")
				}
			case "h":
				for i := 0; i < runningProcs; i++ {
					qch <- 1
				}
			}
		case <-cq:
			runningProcs--
			scanning = false
		default:
			// skip
		}
	}
}

func listenForCommands(control chan string) {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		scanner.Scan()
		control <- scanner.Text()
	}
}

func importSysex(qch <-chan int, cq chan int, dumpch chan []byte) {
	defer func() {
		cq <- 1
	}()

	for {
		select {
		case sysex := <-dumpch:
			fmt.Printf("Got sysex! %x\n", sysex)
		case <-qch:
			fmt.Println("Quitting import")
			return
		}
	}
}

func listenForSysex(device *midi.Device, qch <-chan int, cq chan int, dumpch chan []byte, splitFn func(data []byte, atEOF bool) (advance int, token []byte, err error)) {
	buffer := new(bytes.Buffer)
	defer func() {
		cq <- 1
	}()

	err := device.Open()
	if err != nil {
		fmt.Println("Error opening ", device.Name, ": ", err)
		return
	}
	defer func() {
		fmt.Println("Closing connection...")
		err := device.Close()
		if err != nil {
			fmt.Println("Error closing connection: ", err)
		}
	}()

	input, err := device.ReadChan()
	if err != nil {
		fmt.Println("Error connecting: ", err)
		return
	}

	fmt.Println("Scanning, waiting for data...")

	// TODO: This paradigm of buffered splitting could be extracted...
	for {
		select {
		case data := <-input:
			buffer.Write(data)
			advance, token, _ := splitFn(buffer.Bytes(), false)
			if token != nil {
				dumpch <- token
			}
			for i := 0; i < advance; i++ {
				_, _ = buffer.ReadByte() // advance the buffer
			}
		case <-qch:
			fmt.Println("Quitting scan")
			return
		default:
			// skip
		}
	}
}

func dt(dt midi.DeviceType) (result string) {
	switch dt {
	case midi.DeviceInput:
		result = "Input"
	case midi.DeviceOutput:
		result = "Output"
	case midi.DeviceDuplex:
		result = "Input/Output (Duplex)"
	}
	return result
}

// A bufio.Scanner split function to correctly parse the sysex you're looking for out of a datastream
func splitSysex(vendor, model uint8) func(data []byte, atEOF bool) (advance int, token []byte, err error) {
	header := []byte{0xF0, vendor}
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if si := bytes.Index(data, header); si >= 0 {
			if bytes.IndexByte(data[si:si+4], model) == si+3 {
				if ei := bytes.IndexByte(data[si:], 0xF7); ei >= 0 {
					return si + ei + 1, data[si : si+ei+1], nil
				} else if dup := bytes.Index(data[si+1:], header); dup >= 0 {
					// duplicate start token found, advance to that position
					return dup, nil, nil
				}
			}
			return si, nil, nil
		}

		return 0, nil, nil // no complete token in the buffer yet
	}
}
