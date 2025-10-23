#!/usr/bin/env python3
"""
Native Python3 implementation for reading DS18B20 temperature sensors via DS9097 passive serial adapter.
No external programs or wrappers - implements the complete 1-Wire protocol in pure Python.
"""

import argparse
import serial
import time
import sys
from datetime import datetime
from typing import List, Tuple, Optional

# CRC-8 lookup table for Dallas/Maxim polynomial (0x31)
CRC8_TABLE = [
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
    116, 42, 200, 150, 21, 75, 169, 247, 182, 232, 10, 84, 215, 137, 107, 53
]


def crc8(data: List[int]) -> int:
    """Calculate CRC-8 for Dallas/Maxim devices."""
    crc = 0
    for byte in data:
        crc = CRC8_TABLE[crc ^ byte]
    return crc


class OneWireAdapter:
    """DS9097 passive serial adapter for 1-Wire protocol."""
    
    def __init__(self, device_path: str):
        self.device_path = device_path
        self.port: Optional[serial.Serial] = None
        
    def __enter__(self):
        return self
        
    def __exit__(self, exc_type, exc_val, exc_tb):
        if self.port:
            self.port.close()
    
    def reset(self) -> bool:
        """Send reset pulse and check for presence."""
        # Close existing port if open
        if self.port:
            self.port.close()
            
        # Open at 9600 baud for reset
        try:
            self.port = serial.Serial(
                self.device_path,
                baudrate=9600,
                bytesize=serial.EIGHTBITS,
                parity=serial.PARITY_NONE,
                stopbits=serial.STOPBITS_ONE,
                timeout=1
            )
        except serial.SerialException as e:
            print(f"Error opening {self.device_path}: {e}", file=sys.stderr)
            return False
        
        # Send reset pulse (0xF0)
        self.port.write(bytes([0xF0]))
        self.port.flush()
        
        # Read response
        response = self.port.read(1)
        if len(response) == 0:
            return False
            
        # Check for presence pulse
        presence = response[0]
        return 0x10 <= presence <= 0xE0
    
    def switch_to_data_mode(self):
        """Switch to 115200 baud for data transfer."""
        if self.port:
            self.port.close()
            
        self.port = serial.Serial(
            self.device_path,
            baudrate=115200,
            bytesize=serial.EIGHTBITS,
            parity=serial.PARITY_NONE,
            stopbits=serial.STOPBITS_ONE,
            timeout=1
        )
    
    def touch_bit(self, bit: int) -> int:
        """Transfer a single bit on the 1-Wire bus."""
        # Write slot: 0xFF for read-1/write-1, 0x00 for write-0
        tx_byte = 0xFF if bit != 0 else 0x00
        self.port.write(bytes([tx_byte]))
        
        # Read response
        rx_byte = self.port.read(1)
        if len(rx_byte) == 0:
            return 0
            
        # If we read back 0xFF, the bit is 1; otherwise 0
        return 1 if rx_byte[0] == 0xFF else 0
    
    def touch_byte(self, byte_val: int) -> int:
        """Transfer a byte on the 1-Wire bus (LSB first)."""
        result = 0
        for i in range(8):
            bit = (byte_val >> i) & 1
            read_bit = self.touch_bit(bit)
            result |= (read_bit << i)
        return result
    
    def write_byte(self, byte_val: int):
        """Write a byte to the 1-Wire bus."""
        self.touch_byte(byte_val)
    
    def read_byte(self) -> int:
        """Read a byte from the 1-Wire bus."""
        return self.touch_byte(0xFF)
    
    def search_rom(self) -> List[List[int]]:
        """Search for all devices on the bus using Search ROM algorithm."""
        devices = []
        last_discrepancy = 0
        last_device = False
        
        while not last_device:
            if not self.reset():
                break
                
            self.switch_to_data_mode()
            
            # Send SEARCH_ROM command (0xF0)
            self.write_byte(0xF0)
            
            rom = [0] * 8
            discrepancy = 0
            
            for bit_pos in range(64):
                # Read bit and its complement
                bit = self.touch_bit(1)
                comp_bit = self.touch_bit(1)
                
                if bit == 1 and comp_bit == 1:
                    # No devices responded
                    last_device = True
                    break
                elif bit == 0 and comp_bit == 0:
                    # Discrepancy - multiple devices
                    if bit_pos == last_discrepancy:
                        # Take path 1
                        search_direction = 1
                    elif bit_pos > last_discrepancy:
                        # Take path 0
                        search_direction = 0
                        discrepancy = bit_pos
                    else:
                        # Use previous path
                        byte_idx = bit_pos // 8
                        bit_idx = bit_pos % 8
                        search_direction = (rom[byte_idx] >> bit_idx) & 1
                        if search_direction == 0:
                            discrepancy = bit_pos
                else:
                    # All devices have same bit
                    search_direction = bit
                
                # Write search direction
                self.touch_bit(search_direction)
                
                # Store bit in ROM
                byte_idx = bit_pos // 8
                bit_idx = bit_pos % 8
                if search_direction:
                    rom[byte_idx] |= (1 << bit_idx)
            
            if not last_device:
                last_discrepancy = discrepancy
                if last_discrepancy == 0:
                    last_device = True
                    
                # Validate CRC
                if crc8(rom) == 0:
                    devices.append(rom)
                else:
                    # Invalid CRC, skip this device
                    pass
        
        return devices
    
    def read_temperature(self, rom: List[int]) -> Optional[float]:
        """Read temperature from a specific DS18B20 sensor."""
        # Reset and check presence
        if not self.reset():
            print("Reset failed - no presence pulse", file=sys.stderr)
            return None
            
        self.switch_to_data_mode()
        
        # Send MATCH_ROM command (0x55)
        self.write_byte(0x55)
        
        # Send ROM address (8 bytes)
        for byte in rom:
            self.write_byte(byte)
        
        # Send CONVERT_T command (0x44)
        self.write_byte(0x44)
        
        # Wait for conversion (750ms for 12-bit resolution)
        time.sleep(0.75)
        
        # Reset again
        if not self.reset():
            print("Second reset failed", file=sys.stderr)
            return None
            
        self.switch_to_data_mode()
        
        # Send MATCH_ROM command again
        self.write_byte(0x55)
        
        # Send ROM address
        for byte in rom:
            self.write_byte(byte)
        
        # Send READ_SCRATCHPAD command (0xBE)
        self.write_byte(0xBE)
        
        # Read 9 bytes of scratchpad
        scratchpad = []
        for _ in range(9):
            scratchpad.append(self.read_byte())
        
        # Validate CRC
        if crc8(scratchpad) != 0:
            print(f"CRC validation failed for sensor {rom_to_hex(rom)}", file=sys.stderr)
            return None
        
        # Extract temperature (bytes 0 and 1, little-endian)
        temp_raw = (scratchpad[1] << 8) | scratchpad[0]
        
        # Handle signed 16-bit value
        if temp_raw & 0x8000:
            temp_raw = -((temp_raw ^ 0xFFFF) + 1)
        
        # Convert to Celsius (resolution: 0.0625°C per bit)
        temp_c = temp_raw * 0.0625
        
        return temp_c


def rom_to_hex(rom: List[int]) -> str:
    """Convert ROM address to hex string."""
    return ''.join(f'{byte:02x}' for byte in rom)


def format_timestamp() -> str:
    """Format current timestamp like C digitemp."""
    now = datetime.now()
    return now.strftime("%b %d %H:%M:%S")


def celsius_to_fahrenheit(c: float) -> float:
    """Convert Celsius to Fahrenheit."""
    return c * 9.0 / 5.0 + 32.0


def read_config(config_path: str) -> Tuple[str, List[List[int]]]:
    """Read configuration file."""
    device_path = "/dev/ttyUSB0"
    sensors = []
    
    try:
        with open(config_path, 'r') as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                    
                parts = line.split()
                if len(parts) < 2:
                    continue
                
                if parts[0] == "TTY":
                    device_path = parts[1]
                elif parts[0] == "ROM" and len(parts) >= 10:
                    # Parse ROM: ROM <index> <8 hex bytes>
                    rom = []
                    for i in range(2, 10):
                        # Handle hex values with or without 0x prefix
                        hex_str = parts[i].replace('0x', '')
                        rom.append(int(hex_str, 16))
                    sensors.append(rom)
    except FileNotFoundError:
        pass
    
    return device_path, sensors


def write_config(config_path: str, device_path: str, sensors: List[List[int]]):
    """Write configuration file."""
    with open(config_path, 'w') as f:
        f.write(f"TTY {device_path}\n")
        f.write("READ_TIME 1000\n")
        f.write(f"SENSORS {len(sensors)}\n")
        for idx, rom in enumerate(sensors):
            rom_hex = ' '.join(f'0x{byte:02X}' for byte in rom)
            f.write(f"ROM {idx} {rom_hex}\n")


def main():
    parser = argparse.ArgumentParser(
        description="Native Python3 DS18B20 temperature reader via DS9097"
    )
    parser.add_argument('-t', dest='sensor_index', type=int, metavar='N',
                        help='Read temperature from sensor N (0-based index)')
    parser.add_argument('-s', dest='device', type=str, metavar='DEVICE',
                        help='Serial device path')
    parser.add_argument('-i', dest='init', action='store_true',
                        help='Discover sensors and write digitemp.conf')
    parser.add_argument('-w', dest='walk', action='store_true',
                        help='Discover and list all sensors on bus')
    
    args = parser.parse_args()
    
    config_path = "digitemp.conf"
    
    # Determine device path
    if args.device:
        device_path = args.device
    else:
        device_path, _ = read_config(config_path)
    
    # Initialize mode (-i flag)
    if args.init:
        print(f"Discovering sensors on {device_path}...")
        with OneWireAdapter(device_path) as adapter:
            sensors = adapter.search_rom()
            
            if not sensors:
                print("No sensors found", file=sys.stderr)
                return 1
            
            print(f"Found {len(sensors)} sensor(s)")
            for idx, rom in enumerate(sensors):
                rom_str = ', '.join(f'{byte:02X}' for byte in rom)
                print(f"  Sensor {idx}: [{rom_str}]")
            
            write_config(config_path, device_path, sensors)
            print(f"Configuration written to {config_path}")
        
        return 0
    
    # Walk mode (-w flag)
    if args.walk:
        print(f"Scanning bus {device_path}...")
        with OneWireAdapter(device_path) as adapter:
            sensors = adapter.search_rom()
            
            if not sensors:
                print("No sensors found", file=sys.stderr)
                return 1
            
            print(f"Found {len(sensors)} sensor(s):")
            for idx, rom in enumerate(sensors):
                print(f"  Sensor {idx}: {rom_to_hex(rom)}")
        
        return 0
    
    # Read temperature mode
    device_path, sensors = read_config(config_path)
    
    if not sensors:
        print("No sensors found in config. Run with -i to initialize.", file=sys.stderr)
        return 1
    
    with OneWireAdapter(device_path) as adapter:
        if args.sensor_index is not None:
            # Read specific sensor
            if args.sensor_index < 0 or args.sensor_index >= len(sensors):
                print(f"Invalid sensor index: {args.sensor_index}", file=sys.stderr)
                return 1
            
            rom = sensors[args.sensor_index]
            temp_c = adapter.read_temperature(rom)
            
            if temp_c is None:
                return 1
            
            # Print only temperature value (no timestamp)
            print(f"{temp_c:.2f}")
        else:
            # Read all sensors
            for idx, rom in enumerate(sensors):
                temp_c = adapter.read_temperature(rom)
                
                if temp_c is None:
                    continue
                
                timestamp = format_timestamp()
                temp_f = celsius_to_fahrenheit(temp_c)
                print(f"{timestamp} Sensor {idx} C: {temp_c:.2f} F: {temp_f:.2f}")
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
