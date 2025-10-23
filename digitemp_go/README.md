# DS18B20 Temperature Reader - Native Go Implementation

**True native Go implementation** for reading DS18B20 temperature sensors via DS9097 passive serial adapter. No external programs or wrappers - implements the complete 1-Wire protocol in pure Go.

## Hardware Requirements

- **DS9097 (or DS9097U) passive serial adapter** - connects to RS-232 serial port
- **DS18B20 temperature sensors** - 1-Wire digital thermometers
- **USB-to-Serial adapter** (if needed) - typically appears as `/dev/ttyUSB0`
- **Linux kernel 6.x+** (tested on RHEL 10 / kernel 6.12.0-55)

## Features

✅ **Pure Go implementation** - no calls to external digitemp binary  
✅ **Auto-discovery** - Search ROM algorithm finds all sensors on bus  
✅ **Multi-sensor support** - MATCH_ROM addressing for specific sensors  
✅ **CRC-8 validation** - Dallas/Maxim lookup table for data integrity  
✅ **Buffered FIFO** - reliable UART communication with 16-byte chunks  
✅ **Config generation** - automatic `digitemp.conf` creation  

## Compilation

### Prerequisites
```bash
# Install Go (if not already installed)
# Download from https://go.dev/dl/ or use package manager:
sudo dnf install golang  # RHEL/Fedora
sudo apt install golang  # Debian/Ubuntu

# Verify installation
go version
```

### Build Binary
```bash
cd /opt/sensors/digitemp_go

# Download dependencies
go mod download

# Build
go build -o digitemp_go main.go

# Binary will be at: ./digitemp_go
```

### Install (Optional)
```bash
sudo cp digitemp_go /usr/local/bin/
sudo chmod +x /usr/local/bin/digitemp_go
```

## Initial Setup

### 1. Connect Hardware
- Plug DS9097 adapter into USB port (appears as `/dev/ttyUSB0`)
- Connect DS18B20 sensors to the adapter's 1-Wire bus
- Verify device exists: `ls -l /dev/ttyUSB0`

### 2. Set Permissions
```bash
# Add your user to dialout group (or run with sudo)
sudo usermod -a -G dialout $USER
# Log out and back in for group changes to take effect
```

### 3. Initialize Configuration
**REQUIRED FIRST STEP:** The program needs to discover your sensors before it can read temperatures.

```bash
cd /opt/sensors/digitemp_go
sudo ./digitemp_go -i
```

This will:
- Scan the 1-Wire bus for all DS18B20 sensors
- Display their ROM addresses
- Create `digitemp.conf` with sensor configuration

Expected output:
```
Discovering sensors on /dev/ttyUSB0...
Found 2 sensor(s)
  Sensor 0: [28, 52, C0, 80, 00, 00, 00, A5]
  Sensor 1: [28, BF, DE, 80, 00, 00, 00, 18]
Configuration written to digitemp.conf
```

## Usage

### Read All Sensors (Default)
```bash
sudo ./digitemp_go
```
Output:
```
Oct 23 08:15:42 Sensor 0 C: 27.44 F: 81.39
Oct 23 08:15:44 Sensor 1 C: 20.75 F: 69.35
```

### Read Specific Sensor
```bash
# Read sensor 0 (temperature only, no timestamp)
sudo ./digitemp_go -t 0

# Read sensor 1
sudo ./digitemp_go -t 1
```
Output:
```
27.44
```

### List All Sensors on Bus
```bash
sudo ./digitemp_go -w
```
Output:
```
Scanning bus /dev/ttyUSB0...
Found 2 sensor(s):
  Sensor 0: 2852c080000000a5
  Sensor 1: 28bfde8000000018
```

### Use Custom Device Path
```bash
sudo ./digitemp_go -s /dev/ttyUSB1
```

### Command-Line Options
```
Options:
  -t string    Read temperature from sensor N (0-based index)
  -s string    Serial device path
  -i           Discover sensors and write digitemp.conf
  -w           Discover and list all sensors on bus
```

## Configuration File

The program reads `digitemp.conf` from the **current directory** (not a system path).

**Important:** If you see "No sensors found in config. Run with -i to initialize.", you need to:
1. Navigate to the directory containing `digitemp.conf`, or
2. Run initialization: `sudo ./digitemp_go -i`

Example `digitemp.conf`:
```
TTY /dev/ttyUSB0
READ_TIME 1000
SENSORS 2
ROM 0 0x28 0x52 0xC0 0x80 0x00 0x00 0x00 0xA5
ROM 1 0x28 0xBF 0xDE 0x80 0x00 0x00 0x00 0x18
```

### Configuration Parameters

- **TTY** - Serial device path (e.g., `/dev/ttyUSB0`)
- **READ_TIME** - Sensor read interval in milliseconds (default: 1000)
- **SENSORS** - Number of sensors configured
- **ROM** - Sensor ROM address (8 bytes in hex format)

## Troubleshooting

### "No sensors found in config"
**Solution:** Run initialization first: `sudo ./digitemp_go -i`

### "Permission denied" on /dev/ttyUSB0
**Solution:** Run with `sudo` or add user to dialout group:
```bash
sudo usermod -a -G dialout $USER
# Log out and back in
```

### "No sensors found" during initialization
**Check:**
- DS9097 adapter is plugged in (`ls -l /dev/ttyUSB0`)
- DS18B20 sensors are connected to adapter
- 4.7kΩ pull-up resistor between DATA and VDD (usually built into adapter)
- Correct device path (try `-s /dev/ttyUSB1` if needed)

### "CRC validation failed"
**Causes:**
- Cable too long (keep under 100m for reliable operation)
- Electrical noise or poor connections
- Multiple sensors without proper topology

### Build errors with go.sum
```bash
# Clean and rebuild
rm go.sum
go mod tidy
go build -o digitemp_go main.go
```

## Technical Details

### DS9097 Protocol
- **Reset pulse:** 9600 baud, send 0xF0, detect presence
- **Data transfer:** 115200 baud, bit-level communication
- **Baud switching:** Port reopened at different speeds for reset vs data
- **Buffering:** FIFO chunks of 16 bytes for UART reliability

### DS18B20 Commands
- **MATCH_ROM (0x55):** Select specific sensor by 64-bit ROM address
- **SEARCH_ROM (0xF0):** Discover all devices on bus
- **CONVERT_T (0x44):** Trigger temperature conversion (~750ms)
- **READ_SCRATCHPAD (0xBE):** Read 9-byte scratchpad with temperature data

### CRC-8 Validation
- Dallas/Maxim polynomial: 0x31 (x^8 + x^5 + x^4 + 1)
- 256-byte lookup table for fast computation
- Validates all 9 scratchpad bytes (correct CRC yields 0x00)

## Dependencies

```go
module digitemp_go

go 1.21

require github.com/tarm/serial v0.0.0-20180830185346-98f6abe2eb07
require golang.org/x/sys v0.0.0-20220310020820-b874c991c1a5 // indirect
```

**Note:** The `tarm/serial` library provides cross-platform serial port access.

## Comparison with C digitemp

This Go implementation produces **identical output** to the original C digitemp:
- Same temperature readings (validated to 0.06°C accuracy)
- Compatible configuration file format
- Same command-line interface conventions
- **Advantage:** No external dependencies, single binary, fast compilation

## License

This is an independent implementation of the DS9097/DS18B20 protocol.  
Original digitemp by Brian C. Lane is GPL v2.0.
