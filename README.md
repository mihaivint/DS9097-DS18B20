# Native implementation in go/rust/python for DS18B20 temperature reader over DS9097u passive adapter
## Hardware Requirements

- **DS9097 (or DS9097U) passive serial adapter** - connects to RS-232 serial port
- **DS18B20 temperature sensors** - 1-Wire digital thermometers
- **USB-to-Serial adapter** (if needed) - typically appears as `/dev/ttyUSB0`
- **Linux kernel 6.x+** (tested on RHEL 10 / kernel 6.12.0-55)

