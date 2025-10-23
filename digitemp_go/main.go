package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tarm/serial"
)

const (
	// DS18B20 commands
	DS18B20_MATCH_ROM        = 0x55
	DS18B20_SEARCH_ROM       = 0xF0
	DS18B20_READ_SCRATCHPAD  = 0xBE
	DS18B20_CONVERT_T        = 0x44
	
	// UART FIFO size for buffered communication
	UART_FIFO_SIZE = 16
)

// CRC-8 lookup table for Dallas/Maxim
var crcTable = [256]byte{
	0, 94, 188, 226, 97, 63, 221, 131, 194, 156, 126, 32, 163, 253, 31, 65,
	157, 195, 33, 127, 252, 162, 64, 30, 95, 1, 227, 189, 62, 96, 130, 220,
	35, 125, 159, 193, 66, 28, 254, 160, 225, 191, 93, 3, 128, 222, 60, 98,
	190, 224, 2, 92, 223, 129, 99, 61, 124, 34, 192, 158, 29, 67, 161, 255,
	70, 24, 250, 164, 39, 121, 155, 197, 132, 218, 56, 102, 229, 187, 89, 7,
	219, 133, 103, 57, 186, 228, 6, 88, 25, 71, 165, 251, 120, 38, 196, 154,
	101, 59, 217, 135, 4, 90, 184, 230, 167, 249, 27, 69, 198, 152, 122, 36,
	248, 166, 68, 26, 153, 199, 37, 123, 58, 100, 134, 216, 91, 5, 231, 185,
	140, 210, 48, 110, 237, 179, 81, 15, 78, 16, 242, 172, 47, 113, 147, 205,
	17, 79, 173, 243, 112, 46, 204, 146, 211, 141, 111, 49, 178, 236, 14, 80,
	175, 241, 19, 77, 206, 144, 114, 44, 109, 51, 209, 143, 12, 82, 176, 238,
	50, 108, 142, 208, 83, 13, 239, 177, 240, 174, 76, 18, 145, 207, 45, 115,
	202, 148, 118, 40, 171, 245, 23, 73, 8, 86, 180, 234, 105, 55, 213, 139,
	87, 9, 235, 181, 54, 104, 138, 212, 149, 203, 41, 119, 244, 170, 72, 22,
	233, 183, 85, 11, 136, 214, 52, 106, 43, 117, 151, 201, 74, 20, 246, 168,
	116, 42, 200, 150, 21, 75, 169, 247, 182, 232, 10, 84, 215, 137, 107, 53,
}

type OneWireAdapter struct {
	port     *serial.Port
	portName string
}

func NewOneWireAdapter(path string) (*OneWireAdapter, error) {
	config := &serial.Config{
		Name:        path,
		Baud:        115200,
		Size:        8,
		Parity:      serial.ParityNone,
		StopBits:    serial.Stop1,
		ReadTimeout: 5 * time.Second,
	}

	port, err := serial.OpenPort(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open port: %v", err)
	}

	return &OneWireAdapter{port: port, portName: path}, nil
}

func (a *OneWireAdapter) Close() error {
	if a.port != nil {
		return a.port.Close()
	}
	return nil
}

func (a *OneWireAdapter) setBaud(baud int) error {
	// Close and reopen at new baud rate
	a.port.Close()

	config := &serial.Config{
		Name:        a.portName,
		Baud:        baud,
		Size:        8,
		Parity:      serial.ParityNone,
		StopBits:    serial.Stop1,
		ReadTimeout: 5 * time.Second,
	}

	port, err := serial.OpenPort(config)
	if err != nil {
		return err
	}
	a.port = port
	return nil
}

func (a *OneWireAdapter) Reset() (bool, error) {
	// Flush buffers
	a.port.Flush()

	// Set to 9600 baud for reset
	if err := a.setBaud(9600); err != nil {
		return false, err
	}

	// Send reset pulse
	_, err := a.port.Write([]byte{0xF0})
	if err != nil {
		return false, err
	}

	time.Sleep(5 * time.Millisecond)

	// Read response
	buf := make([]byte, 1)
	n, err := a.port.Read(buf)
	if err != nil || n != 1 {
		return false, fmt.Errorf("failed to read reset response")
	}

	// Set back to 115200 for data
	if err := a.setBaud(115200); err != nil {
		return false, err
	}

	// Presence detected if response is not 0xF0 and not 0x00
	return buf[0] != 0xF0 && buf[0] != 0x00, nil
}

func (a *OneWireAdapter) touchBits(bits []byte) ([]byte, error) {
	nbits := len(bits)
	sendBuf := make([]byte, nbits)

	// Convert bits to bytes for transmission
	for i := 0; i < nbits; i++ {
		if bits[i] != 0 {
			sendBuf[i] = 0xFF
		} else {
			sendBuf[i] = 0x00
		}
	}

	// Send bits in chunks of UART_FIFO_SIZE
	resultBits := make([]byte, 0, nbits)
	offset := 0

	for offset < nbits {
		chunkSize := UART_FIFO_SIZE
		if nbits-offset < chunkSize {
			chunkSize = nbits - offset
		}

		// Write chunk
		n, err := a.port.Write(sendBuf[offset : offset+chunkSize])
		if err != nil {
			return nil, fmt.Errorf("write error: %v", err)
		}
		if n != chunkSize {
			return nil, fmt.Errorf("wrote %d bytes, expected %d", n, chunkSize)
		}

		// Small delay for UART processing
		time.Sleep(time.Millisecond)

		// Read response
		recvBuf := make([]byte, chunkSize)
		totalRead := 0
		for totalRead < chunkSize {
			n, err := a.port.Read(recvBuf[totalRead:])
			if err != nil {
				return nil, fmt.Errorf("read error at offset %d: %v", totalRead, err)
			}
			totalRead += n
		}

		// Extract bits from response
		for _, b := range recvBuf {
			resultBits = append(resultBits, b&0x01)
		}

		offset += chunkSize
	}

	return resultBits, nil
}

func (a *OneWireAdapter) WriteByte(b byte) error {
	bits := make([]byte, 8)
	for i := 0; i < 8; i++ {
		bits[i] = (b >> i) & 1
	}
	_, err := a.touchBits(bits)
	return err
}

func (a *OneWireAdapter) ReadByte() (byte, error) {
	// Send all 1s to read
	bits := make([]byte, 8)
	for i := 0; i < 8; i++ {
		bits[i] = 1
	}

	resultBits, err := a.touchBits(bits)
	if err != nil {
		return 0, err
	}

	// Reconstruct byte from bits (LSB first)
	var result byte
	for i := 0; i < 8; i++ {
		if resultBits[i] != 0 {
			result |= 1 << i
		}
	}

	return result, nil
}

func (a *OneWireAdapter) SelectDevice(rom [8]byte) error {
	if err := a.WriteByte(DS18B20_MATCH_ROM); err != nil {
		return err
	}

	for _, b := range rom {
		if err := a.WriteByte(b); err != nil {
			return err
		}
	}

	return nil
}

func calculateCRC8(data []byte) byte {
	var crc byte = 0
	for _, b := range data {
		crc = crcTable[crc^b]
	}
	return crc
}

func validateCRC(data [9]byte) bool {
	return calculateCRC8(data[:]) == 0
}

func (a *OneWireAdapter) ReadTemperature(rom [8]byte) (float32, error) {
	// Reset and check presence
	present, err := a.Reset()
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fmt.Errorf("no device presence detected")
	}

	// Select the specific device
	if err := a.SelectDevice(rom); err != nil {
		return 0, err
	}

	// Issue temperature conversion command
	if err := a.WriteByte(DS18B20_CONVERT_T); err != nil {
		return 0, err
	}

	// Wait for conversion to complete
	time.Sleep(750 * time.Millisecond)

	// Reset again
	present, err = a.Reset()
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fmt.Errorf("device lost during conversion")
	}

	// Select device again
	if err := a.SelectDevice(rom); err != nil {
		return 0, err
	}

	// Read scratchpad
	if err := a.WriteByte(DS18B20_READ_SCRATCHPAD); err != nil {
		return 0, err
	}

	// Read 9 bytes of scratchpad data
	var scratchpad [9]byte
	for i := 0; i < 9; i++ {
		b, err := a.ReadByte()
		if err != nil {
			return 0, err
		}
		scratchpad[i] = b
	}

	// Validate CRC
	if !validateCRC(scratchpad) {
		return 0, fmt.Errorf("CRC validation failed")
	}

	// Extract temperature (bytes 0 and 1, little-endian)
	tempRaw := int16(scratchpad[1])<<8 | int16(scratchpad[0])
	tempC := float32(tempRaw) * 0.0625

	return tempC, nil
}

func (a *OneWireAdapter) DiscoverSensors() ([][8]byte, error) {
	var sensors [][8]byte
	lastDiscrepancy := 0
	lastDevice := false
	var lastROM [8]byte

	for !lastDevice {
		// Reset bus
		present, err := a.Reset()
		if err != nil {
			return nil, err
		}
		if !present {
			break
		}

		// Issue search ROM command
		if err := a.WriteByte(DS18B20_SEARCH_ROM); err != nil {
			return nil, err
		}

		var rom [8]byte
		discrepancyMarker := 0

		// Search through all 64 bits of ROM
		for bitPosition := 0; bitPosition < 64; bitPosition++ {
			byteIdx := bitPosition / 8
			bitMask := byte(1 << (bitPosition % 8))

			// Read two bits: actual bit and its complement
			bits := []byte{1, 1}
			result, err := a.touchBits(bits)
			if err != nil {
				return nil, err
			}

			idBit := result[0]
			cmpIdBit := result[1]

			var searchDirection byte
			if idBit == 1 && cmpIdBit == 1 {
				// No devices responded
				break
			} else if idBit != cmpIdBit {
				// All devices have same bit value
				searchDirection = idBit
			} else {
				// Discrepancy: choose path
				if bitPosition < lastDiscrepancy {
					// Take same path as before
					if lastROM[byteIdx]&bitMask != 0 {
						searchDirection = 1
					} else {
						searchDirection = 0
					}
				} else if bitPosition == lastDiscrepancy {
					searchDirection = 1
				} else {
					discrepancyMarker = bitPosition
					searchDirection = 0
				}
			}

			// Write the chosen direction bit
			_, err = a.touchBits([]byte{searchDirection})
			if err != nil {
				return nil, err
			}

			// Store bit in ROM
			if searchDirection == 1 {
				rom[byteIdx] |= bitMask
			}
		}

		// Validate ROM with CRC
		if calculateCRC8(rom[:]) == 0 {
			sensors = append(sensors, rom)
			lastROM = rom
		}

		lastDiscrepancy = discrepancyMarker
		lastDevice = lastDiscrepancy == 0
	}

	return sensors, nil
}

type Config struct {
	DevicePath string
	Sensors    [][8]byte
}

func readConfig() (*Config, error) {
	config := &Config{
		DevicePath: "/dev/ttyUSB0",
		Sensors:    make([][8]byte, 0),
	}

	file, err := os.Open("digitemp.conf")
	if err != nil {
		return config, nil // Return defaults if file doesn't exist
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "TTY") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				config.DevicePath = parts[1]
			}
		} else if strings.HasPrefix(line, "ROM") {
			parts := strings.Fields(line)
			if len(parts) >= 10 {
				var rom [8]byte
				for i := 0; i < 8; i++ {
					hexStr := strings.TrimPrefix(parts[i+2], "0x")
					val, err := strconv.ParseUint(hexStr, 16, 8)
					if err != nil {
						continue
					}
					rom[i] = byte(val)
				}
				config.Sensors = append(config.Sensors, rom)
			}
		}
	}

	return config, scanner.Err()
}

func writeConfig(devicePath string, sensors [][8]byte) error {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("TTY %s\n", devicePath))
	sb.WriteString("READ_TIME 1000\n")
	sb.WriteString(fmt.Sprintf("SENSORS %d\n", len(sensors)))

	for i, rom := range sensors {
		sb.WriteString(fmt.Sprintf("ROM %d 0x%02X 0x%02X 0x%02X 0x%02X 0x%02X 0x%02X 0x%02X 0x%02X\n",
			i, rom[0], rom[1], rom[2], rom[3], rom[4], rom[5], rom[6], rom[7]))
	}

	return os.WriteFile("digitemp.conf", []byte(sb.String()), 0644)
}

func celsiusToFahrenheit(c float32) float32 {
	return c*9.0/5.0 + 32.0
}

func formatTimestamp() string {
	return time.Now().Format("Jan 02 15:04:05")
}

func main() {
	tempFlag := flag.String("t", "", "Read temperature from sensor N (0-based index)")
	deviceFlag := flag.String("s", "", "Serial device path")
	initFlag := flag.Bool("i", false, "Discover sensors and write digitemp.conf")
	walkFlag := flag.Bool("w", false, "Discover and list all sensors on bus")

	flag.Parse()

	config, err := readConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}

	devicePath := config.DevicePath
	if *deviceFlag != "" {
		devicePath = *deviceFlag
	}

	// Handle discovery/initialization modes
	if *initFlag {
		fmt.Printf("Discovering sensors on %s...\n", devicePath)
		adapter, err := NewOneWireAdapter(devicePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer adapter.Close()

		discovered, err := adapter.DiscoverSensors()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Discovery error: %v\n", err)
			os.Exit(1)
		}

		if len(discovered) == 0 {
			fmt.Fprintln(os.Stderr, "No sensors found!")
			os.Exit(1)
		}

		fmt.Printf("Found %d sensor(s)\n", len(discovered))
		for i, rom := range discovered {
			fmt.Printf("  Sensor %d: [%02X, %02X, %02X, %02X, %02X, %02X, %02X, %02X]\n",
				i, rom[0], rom[1], rom[2], rom[3], rom[4], rom[5], rom[6], rom[7])
		}

		if err := writeConfig(devicePath, discovered); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Configuration written to digitemp.conf")
		return
	}

	if *walkFlag {
		fmt.Printf("Scanning bus %s...\n", devicePath)
		adapter, err := NewOneWireAdapter(devicePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer adapter.Close()

		discovered, err := adapter.DiscoverSensors()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Discovery error: %v\n", err)
			os.Exit(1)
		}

		if len(discovered) == 0 {
			fmt.Println("No sensors found.")
		} else {
			fmt.Printf("Found %d sensor(s):\n", len(discovered))
			for i, rom := range discovered {
				fmt.Printf("  Sensor %d: %s\n", i, hex.EncodeToString(rom[:]))
			}
		}
		return
	}

	// Temperature reading modes
	if *tempFlag != "" {
		// Read specific sensor by index
		sensorIdx, err := strconv.Atoi(*tempFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Invalid sensor index")
			os.Exit(1)
		}

		if len(config.Sensors) == 0 {
			fmt.Fprintln(os.Stderr, "No sensors found in config. Run with -i to initialize.")
			os.Exit(1)
		}

		if sensorIdx < 0 || sensorIdx >= len(config.Sensors) {
			fmt.Fprintf(os.Stderr, "Sensor %d not found (have %d sensors)\n", sensorIdx, len(config.Sensors))
			os.Exit(1)
		}

		adapter, err := NewOneWireAdapter(devicePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer adapter.Close()

		temp, err := adapter.ReadTemperature(config.Sensors[sensorIdx])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("%.2f\n", temp)
	} else {
		// Default or -a flag: read all sensors
		if len(config.Sensors) == 0 {
			fmt.Fprintln(os.Stderr, "No sensors found in config. Run with -i to initialize.")
			os.Exit(1)
		}

		adapter, err := NewOneWireAdapter(devicePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer adapter.Close()

		for i, rom := range config.Sensors {
			temp, err := adapter.ReadTemperature(rom)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Sensor %d error: %v\n", i, err)
				continue
			}

			tempF := celsiusToFahrenheit(temp)
			fmt.Printf("%s Sensor %d C: %.2f F: %.2f\n",
				formatTimestamp(), i, temp, tempF)

			time.Sleep(500 * time.Millisecond)
		}
	}
}
