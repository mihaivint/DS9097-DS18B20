# DS18B20 Temperature Reader - Native Python3 Implementation

**True native Python3 implementation** for reading DS18B20 temperature sensors via DS9097 passive serial adapter. No external programs or wrappers - implements the complete 1-Wire protocol in pure Python.

## Hardware Requirements

- **DS9097 (or DS9097U) passive serial adapter** - connects to RS-232 serial port
- **DS18B20 temperature sensors** - 1-Wire digital thermometers
- **USB-to-Serial adapter** (if needed) - typically appears as `/dev/ttyUSB0`
- **Linux kernel 6.x+** (tested on RHEL 10 / kernel 6.12.0-55)

## Features

✅ **Pure Python3 implementation** - no calls to external digitemp binary  
✅ **Auto-discovery** - Search ROM algorithm finds all sensors on bus  
✅ **Multi-sensor support** - MATCH_ROM addressing for specific sensors  
✅ **CRC-8 validation** - Dallas/Maxim lookup table for data integrity  
✅ **Buffered bit operations** - reliable UART communication  
✅ **Config generation** - automatic `digitemp.conf` creation  
✅ **Single dependency** - only requires `pyserial`  

## Installation

### Prerequisites
```bash
# Python 3.6+ (check version)
python3 --version

# Install pip if needed
sudo dnf install python3-pip  # RHEL/Fedora
sudo apt install python3-pip  # Debian/Ubuntu
```

### Install Dependencies
```bash
cd /opt/sensors/digitemp_python

# Install pyserial
pip3 install -r requirements.txt

# Or install directly
pip3 install pyserial
```

### Make Executable
```bash
chmod +x digitemp.py
```

### Install (Optional)
```bash
sudo cp digitemp.py /usr/local/bin/digitemp_python
sudo chmod +x /usr/local/bin/digitemp_python
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
cd /opt/sensors/digitemp_python
sudo python3 digitemp.py -i
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
sudo python3 digitemp.py
```
Output:
```
Oct 23 08:15:42 Sensor 0 C: 27.44 F: 81.39
Oct 23 08:15:44 Sensor 1 C: 20.75 F: 69.35
```

### Read Specific Sensor
```bash
# Read sensor 0 (temperature only, no timestamp)
sudo python3 digitemp.py -t 0

# Read sensor 1
sudo python3 digitemp.py -t 1
```
Output:
```
27.44
```

### List All Sensors on Bus
```bash
sudo python3 digitemp.py -w
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
sudo python3 digitemp.py -s /dev/ttyUSB1
```

### Using Shebang (if executable)
```bash
# After chmod +x digitemp.py
sudo ./digitemp.py
sudo ./digitemp.py -t 0
sudo ./digitemp.py -w
```

### Command-Line Options
```
Options:
  -t N         Read temperature from sensor N (0-based index)
  -s DEVICE    Serial device path
  -i           Discover sensors and write digitemp.conf
  -w           Discover and list all sensors on bus
  -h, --help   Show help message
```

## Configuration File

The program reads `digitemp.conf` from the **current directory** (not a system path).

**Important:** If you see "No sensors found in config. Run with -i to initialize.", you need to:
1. Navigate to the directory containing `digitemp.conf`, or
2. Run initialization: `sudo python3 digitemp.py -i`

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
**Solution:** Run initialization first: `sudo python3 digitemp.py -i`

### "Permission denied" on /dev/ttyUSB0
**Solution:** Run with `sudo` or add user to dialout group:
```bash
sudo usermod -a -G dialout $USER
# Log out and back in
```

### "No module named 'serial'"
**Solution:** Install pyserial:
```bash
pip3 install pyserial
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

### Import errors
```bash
# Verify Python version (requires 3.6+)
python3 --version

# Check if pyserial is installed
python3 -c "import serial; print(serial.__version__)"

# Reinstall if needed
pip3 install --upgrade pyserial
```

## Technical Details

### DS9097 Protocol
- **Reset pulse:** 9600 baud, send 0xF0, detect presence
- **Data transfer:** 115200 baud, bit-level communication
- **Baud switching:** Port reopened at different speeds for reset vs data
- **Bit operations:** Each bit transferred as full byte (0xFF = 1, 0x00 = 0)

### DS18B20 Commands
- **MATCH_ROM (0x55):** Select specific sensor by 64-bit ROM address
- **SEARCH_ROM (0xF0):** Discover all devices on bus
- **CONVERT_T (0x44):** Trigger temperature conversion (~750ms)
- **READ_SCRATCHPAD (0xBE):** Read 9-byte scratchpad with temperature data

### CRC-8 Validation
- Dallas/Maxim polynomial: 0x31 (x^8 + x^5 + x^4 + 1)
- 256-byte lookup table for fast computation
- Validates all 9 scratchpad bytes (correct CRC yields 0x00)

### Temperature Conversion
- Raw value: 16-bit signed integer (little-endian)
- Resolution: 0.0625°C per bit (12-bit default)
- Range: -55°C to +125°C
- Formula: `temp_c = raw_value * 0.0625`

## Dependencies

```
pyserial>=3.5
```

**Note:** The `pyserial` library provides cross-platform serial port access. Install via `pip3 install pyserial`.

## Python Version Compatibility

- **Minimum:** Python 3.6 (uses f-strings and type hints)
- **Tested:** Python 3.9, Python 3.11, Python 3.13
- **Recommended:** Python 3.9+

## Comparison with C digitemp

This Python implementation produces **identical output** to the original C digitemp:
- Same temperature readings (validated to 0.06°C accuracy)
- Compatible configuration file format
- Same command-line interface conventions
- **Advantages:**
  - No compilation needed
  - Easy to read and modify
  - Cross-platform (Windows, Linux, macOS)
  - Single dependency (`pyserial`)

## Performance

Typical timing per sensor read:
- Reset + presence: ~10ms
- Conversion time: 750ms (DS18B20 spec)
- Scratchpad read: ~20ms
- **Total:** ~780ms per sensor

For 2 sensors: ~1.6 seconds total

## License

This is an independent implementation of the DS9097/DS18B20 protocol.  
Original digitemp by Brian C. Lane is GPL v2.0.

## Development

### Code Structure
- `OneWireAdapter` class - handles all serial communication
- `reset()` - 9600 baud presence detection
- `touch_bit()` - bit-level read/write at 115200 baud
- `search_rom()` - device discovery algorithm
- `read_temperature()` - full conversion sequence
- `crc8()` - CRC validation with lookup table

### Adding Features
The code is designed to be readable and extensible:
- Add new 1-Wire commands by implementing touch_bit/touch_byte sequences
- Support other DS18B20 family devices (DS18S20, DS1822)
- Add logging or database storage
- Integrate with home automation systems

### Testing
```bash
# Test initialization
sudo python3 digitemp.py -i

# Test sensor listing
sudo python3 digitemp.py -w

# Test temperature reading
sudo python3 digitemp.py

# Test specific sensor
sudo python3 digitemp.py -t 0
```
