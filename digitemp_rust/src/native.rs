use std::io::Read;
use std::io::Write;
use std::time::Duration;
use std::thread;
use clap::{Arg, Command};
use serialport::{SerialPort, DataBits, Parity, StopBits};

// DS18B20 commands
const DS18B20_SKIP_ROM: u8 = 0xCC;
const DS18B20_CONVERT_T: u8 = 0x44;
const DS18B20_READ_SCRATCHPAD: u8 = 0xBE;
const DS18B20_MATCH_ROM: u8 = 0x55;

// UART FIFO size for buffered communication
const UART_FIFO_SIZE: usize = 16; // Start with smaller chunks for reliability

// Error handling
#[derive(Debug)]
pub enum OneWireError {
    SerialError(serialport::Error),
    IoError(std::io::Error),
    DeviceNotPresent,
    InvalidTemperature(f64),
}

impl std::fmt::Display for OneWireError {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        match self {
            OneWireError::SerialError(e) => write!(f, "Serial error: {}", e),
            OneWireError::IoError(e) => write!(f, "IO error: {}", e),
            OneWireError::DeviceNotPresent => write!(f, "No device present on bus"),
            OneWireError::InvalidTemperature(temp) => write!(f, "Temperature out of range: {:.2}°C", temp),
        }
    }
}

impl std::error::Error for OneWireError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            OneWireError::SerialError(e) => Some(e),
            OneWireError::IoError(e) => Some(e),
            _ => None,
        }
    }
}

impl From<serialport::Error> for OneWireError {
    fn from(error: serialport::Error) -> Self {
        OneWireError::SerialError(error)
    }
}

impl From<std::io::Error> for OneWireError {
    fn from(error: std::io::Error) -> Self {
        OneWireError::IoError(error)
    }
}

// Native DS9097 1-Wire adapter implementation
pub struct OneWireAdapter {
    port: Box<dyn SerialPort>,
}

impl OneWireAdapter {
    pub fn new(path: &str) -> Result<Self, OneWireError> {
        // Open port at 115200 baud (data transmission speed)
        let port = serialport::new(path, 115200)
            .data_bits(DataBits::Eight)
            .parity(Parity::None)
            .stop_bits(StopBits::One)
            .timeout(Duration::from_secs(5))
            .open()?;

        Ok(OneWireAdapter { port })
    }

    fn set_baud(&mut self, baud: u32) -> Result<(), OneWireError> {
        self.port.set_baud_rate(baud)?;
        Ok(())
    }

    pub fn reset(&mut self) -> Result<bool, OneWireError> {
        // Flush buffers
        self.port.clear(serialport::ClearBuffer::All)?;
        
        // Set to 9600 baud for reset
        self.set_baud(9600)?;
        
        self.port.write_all(&[0xF0])?;
        thread::sleep(Duration::from_millis(5));
        
        let mut buf = [0u8; 1];
        self.port.read_exact(&mut buf)?;
        
        // Set back to 115200 for data
        self.set_baud(115200)?;
        
        // Presence detected if response is not 0xF0 and not 0x00
        Ok(buf[0] != 0xF0 && buf[0] != 0x00)
    }

    fn touch_bits(&mut self, bits: &[u8]) -> Result<Vec<u8>, OneWireError> {
        let nbits = bits.len();
        let mut send_buf = vec![0u8; nbits];
        
        // Convert bits to bytes for transmission
        for i in 0..nbits {
            send_buf[i] = if bits[i] != 0 { 0xFF } else { 0x00 };
        }
        
        // Send bits in chunks of UART_FIFO_SIZE
        let mut result_bits = Vec::with_capacity(nbits);
        let mut offset = 0;
        
        while offset < nbits {
            let chunk_size = std::cmp::min(UART_FIFO_SIZE, nbits - offset);
            
            // Write chunk
            self.port.write_all(&send_buf[offset..offset + chunk_size])?;
            
            // Read response
            let mut recv_buf = vec![0u8; chunk_size];
            self.port.read_exact(&mut recv_buf)?;
            
            // Extract bits from response (check bit 0 of each byte)
            for byte in recv_buf {
                result_bits.push(byte & 0x01);
            }
            
            offset += chunk_size;
        }
        
        Ok(result_bits)
    }

    pub fn write_byte(&mut self, byte: u8) -> Result<(), OneWireError> {
        let mut bits = [0u8; 8];
        for i in 0..8 {
            bits[i] = (byte >> i) & 1;
        }
        self.touch_bits(&bits)?;
        Ok(())
    }

    pub fn read_byte(&mut self) -> Result<u8, OneWireError> {
        // Send all 1s to read (0xFF bits)
        let bits = [1u8; 8];
        let result_bits = self.touch_bits(&bits)?;
        
        // Reconstruct byte from bits (LSB first)
        let mut byte = 0u8;
        for i in 0..8 {
            if result_bits[i] != 0 {
                byte |= 1 << i;
            }
        }
        
        Ok(byte)
    }

    pub fn select_device(&mut self, rom: &[u8; 8]) -> Result<(), OneWireError> {
        self.write_byte(DS18B20_MATCH_ROM)?;
        for &byte in rom {
            self.write_byte(byte)?;
        }
        Ok(())
    }

    // CRC-8 validation for DS18B20 scratchpad
    fn validate_crc(data: &[u8; 9]) -> bool {
        Self::calculate_crc8(data) == 0
    }

    // CRC-8 calculation for DS18B20 scratchpad validation
    // Uses Dallas/Maxim CRC-8 lookup table
    fn calculate_crc8(data: &[u8]) -> u8 {
        const CRC_TABLE: [u8; 256] = [
            0, 94,188,226, 97, 63,221,131,194,156,126, 32,163,253, 31, 65,
            157,195, 33,127,252,162, 64, 30, 95,  1,227,189, 62, 96,130,220,
            35,125,159,193, 66, 28,254,160,225,191, 93,  3,128,222, 60, 98,
            190,224,  2, 92,223,129, 99, 61,124, 34,192,158, 29, 67,161,255,
            70, 24,250,164, 39,121,155,197,132,218, 56,102,229,187, 89,  7,
            219,133,103, 57,186,228,  6, 88, 25, 71,165,251,120, 38,196,154,
            101, 59,217,135,  4, 90,184,230,167,249, 27, 69,198,152,122, 36,
            248,166, 68, 26,153,199, 37,123, 58,100,134,216, 91,  5,231,185,
            140,210, 48,110,237,179, 81, 15, 78, 16,242,172, 47,113,147,205,
            17, 79,173,243,112, 46,204,146,211,141,111, 49,178,236, 14, 80,
            175,241, 19, 77,206,144,114, 44,109, 51,209,143, 12, 82,176,238,
            50,108,142,208, 83, 13,239,177,240,174, 76, 18,145,207, 45,115,
            202,148,118, 40,171,245, 23, 73,  8, 86,180,234,105, 55,213,139,
            87,  9,235,181, 54,104,138,212,149,203, 41,119,244,170, 72, 22,
            233,183, 85, 11,136,214, 52,106, 43,117,151,201, 74, 20,246,168,
            116, 42,200,150, 21, 75,169,247,182,232, 10, 84,215,137,107, 53
        ];
        
        let mut crc = 0u8;
        for &byte in data {
            crc = CRC_TABLE[(crc ^ byte) as usize];
        }
        crc
    }

    // Discover all DS18B20 sensors on the bus using search ROM algorithm
    pub fn discover_sensors(&mut self) -> Result<Vec<[u8; 8]>, OneWireError> {
        let mut sensors = Vec::new();
        let mut last_discrepancy = 0;
        let mut last_device = false;
        let mut last_rom = [0u8; 8];
        
        while !last_device {
            // Reset bus
            if !self.reset()? {
                break;
            }
            
            // Issue search ROM command
            self.write_byte(0xF0)?; // SEARCH_ROM command
            
            let mut rom = [0u8; 8];
            let mut discrepancy_marker = 0;
            
            // Search through all 64 bits of ROM
            for bit_position in 0..64 {
                let byte_idx = bit_position / 8;
                let bit_mask = 1u8 << (bit_position % 8);
                
                // Read two bits: actual bit and its complement
                let mut bits = [1u8; 2];
                let result = self.touch_bits(&bits)?;
                let id_bit = result[0];
                let cmp_id_bit = result[1];
                
                let search_direction = if id_bit == 1 && cmp_id_bit == 1 {
                    // No devices responded
                    break;
                } else if id_bit != cmp_id_bit {
                    // All devices have same bit value
                    id_bit
                } else {
                    // Discrepancy: choose path
                    if bit_position < last_discrepancy {
                        // Take same path as before
                        if last_rom[byte_idx] & bit_mask != 0 { 1 } else { 0 }
                    } else if bit_position == last_discrepancy {
                        1 // Take 1 path at discrepancy point
                    } else {
                        discrepancy_marker = bit_position;
                        0 // Take 0 path for new discrepancy
                    }
                };
                
                // Write the chosen direction bit
                self.touch_bits(&[search_direction])?;
                
                // Store bit in ROM
                if search_direction == 1 {
                    rom[byte_idx] |= bit_mask;
                }
            }
            
            // Validate ROM with CRC
            if Self::calculate_crc8(&rom) == 0 {
                sensors.push(rom);
                last_rom = rom;
            }
            
            last_discrepancy = discrepancy_marker;
            last_device = last_discrepancy == 0;
        }
        
        Ok(sensors)
    }

    // Read temperature from a specific DS18B20 sensor
    pub fn read_temperature(&mut self, rom: &[u8; 8]) -> Result<f32, OneWireError> {
        // Reset and check presence
        if !self.reset()? {
            return Err(OneWireError::IoError(std::io::Error::new(
                std::io::ErrorKind::NotConnected,
                "No device presence detected"
            )));
        }

        // Select the specific device
        self.select_device(rom)?;

        // Issue temperature conversion command
        self.write_byte(0x44)?;

        // Wait for conversion to complete (750ms max for 12-bit)
        thread::sleep(Duration::from_millis(750));

        // Reset again
        if !self.reset()? {
            return Err(OneWireError::IoError(std::io::Error::new(
                std::io::ErrorKind::NotConnected,
                "Device lost during conversion"
            )));
        }

        // Select device again
        self.select_device(rom)?;

        // Read scratchpad
        self.write_byte(0xBE)?;

        // Read 9 bytes of scratchpad data
        let mut scratchpad = [0u8; 9];
        for i in 0..9 {
            scratchpad[i] = self.read_byte()?;
        }

        // Validate CRC
        if !Self::validate_crc(&scratchpad) {
            return Err(OneWireError::IoError(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                "CRC validation failed"
            )));
        }

        // Extract temperature (bytes 0 and 1, little-endian)
        let temp_raw = i16::from_le_bytes([scratchpad[0], scratchpad[1]]);
        let temp_c = temp_raw as f32 * 0.0625;

        Ok(temp_c)
    }
}

fn read_config() -> (String, Vec<[u8; 8]>) {
    let mut device_path = "/dev/ttyUSB0".to_string();
    let mut sensors = Vec::new();
    
    if let Ok(content) = std::fs::read_to_string("digitemp.conf") {
        for line in content.lines() {
            if line.starts_with("TTY") {
                let parts: Vec<&str> = line.split_whitespace().collect();
                if parts.len() >= 2 {
                    device_path = parts[1].to_string();
                }
            } else if line.starts_with("ROM") {
                let parts: Vec<&str> = line.split_whitespace().collect();
                if parts.len() >= 10 {
                    let mut rom = [0u8; 8];
                    for i in 0..8 {
                        if let Ok(byte) = u8::from_str_radix(&parts[i + 2].trim_start_matches("0x"), 16) {
                            rom[i] = byte;
                        }
                    }
                    sensors.push(rom);
                }
            }
        }
    }
    
    (device_path, sensors)
}

fn celsius_to_fahrenheit(celsius: f32) -> f32 {
    celsius * 9.0 / 5.0 + 32.0
}

fn format_timestamp() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs();
    let h = (now / 3600) % 24;
    let m = (now / 60) % 60;
    let s = now % 60;
    format!("Oct 23 {:02}:{:02}:{:02}", h, m, s)
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let matches = Command::new("digitemp_rust_native")
        .version("0.1.0")
        .about("DS18B20 Temperature Reader - True Native Rust Implementation")
        .arg(Arg::new("all")
            .short('a')
            .long("all")
            .help("Read all sensors with header")
            .action(clap::ArgAction::SetTrue))
        .arg(Arg::new("temp")
            .short('t')
            .long("temp")
            .value_name("SENSOR")
            .help("Read temperature from sensor N (0-based index)"))
        .arg(Arg::new("device")
            .short('s')
            .long("serial")
            .value_name("DEVICE")
            .help("Serial device path"))
        .arg(Arg::new("init")
            .short('i')
            .long("init")
            .help("Discover sensors and write digitemp.conf")
            .action(clap::ArgAction::SetTrue))
        .arg(Arg::new("walk")
            .short('w')
            .long("walk")
            .help("Discover and list all sensors on bus")
            .action(clap::ArgAction::SetTrue))
        .get_matches();

    let (config_device_path, sensors) = read_config();
    
    let device_path = matches.get_one::<String>("device")
        .map(|s| s.as_str())
        .unwrap_or(&config_device_path);

    let mut adapter = OneWireAdapter::new(device_path)?;

    // Handle discovery/initialization modes
    if matches.get_flag("init") {
        println!("Discovering sensors on {}...", device_path);
        let discovered = adapter.discover_sensors()?;
        
        if discovered.is_empty() {
            eprintln!("No sensors found!");
            std::process::exit(1);
        }
        
        println!("Found {} sensor(s)", discovered.len());
        
        // Write config file
        let mut config_content = format!("TTY {}\n", device_path);
        config_content.push_str("READ_TIME 1000\n");
        config_content.push_str(&format!("SENSORS {}\n", discovered.len()));
        
        for (i, rom) in discovered.iter().enumerate() {
            config_content.push_str(&format!("ROM {} 0x{:02X} 0x{:02X} 0x{:02X} 0x{:02X} 0x{:02X} 0x{:02X} 0x{:02X} 0x{:02X}\n",
                i, rom[0], rom[1], rom[2], rom[3], rom[4], rom[5], rom[6], rom[7]));
            println!("  Sensor {}: {:02X?}", i, rom);
        }
        
        std::fs::write("digitemp.conf", config_content)?;
        println!("Configuration written to digitemp.conf");
        return Ok(());
    }
    
    if matches.get_flag("walk") {
        println!("Scanning bus {}...", device_path);
        let discovered = adapter.discover_sensors()?;
        
        if discovered.is_empty() {
            println!("No sensors found.");
        } else {
            println!("Found {} sensor(s):", discovered.len());
            for (i, rom) in discovered.iter().enumerate() {
                println!("  Sensor {}: {:02X}{:02X}{:02X}{:02X}{:02X}{:02X}{:02X}{:02X}",
                    i, rom[0], rom[1], rom[2], rom[3], rom[4], rom[5], rom[6], rom[7]);
            }
        }
        return Ok(());
    }

    // Temperature reading modes
    if let Some(sensor_arg) = matches.get_one::<String>("temp") {
        // Read specific sensor by index
        let sensor_idx: usize = sensor_arg.parse()
            .map_err(|_| "Invalid sensor index")?;
        
        if sensors.is_empty() {
            eprintln!("No sensors found in config. Run with -i to initialize.");
            std::process::exit(1);
        }
        
        if sensor_idx >= sensors.len() {
            eprintln!("Sensor {} not found (have {} sensors)", sensor_idx, sensors.len());
            std::process::exit(1);
        }
        
        match adapter.read_temperature(&sensors[sensor_idx]) {
            Ok(temp) => println!("{:.2}", temp),
            Err(e) => {
                eprintln!("Error: {}", e);
                std::process::exit(1);
            }
        }
    } else {
        // Default or -a flag: read all sensors
        if sensors.is_empty() {
            eprintln!("No sensors found in config. Run with -i to initialize.");
            std::process::exit(1);
        }
        
        for (i, rom) in sensors.iter().enumerate() {
            match adapter.read_temperature(rom) {
                Ok(temp_c) => {
                    let temp_f = celsius_to_fahrenheit(temp_c);
                    println!("{} Sensor {} C: {:.2} F: {:.2}", 
                        format_timestamp(), i, temp_c, temp_f);
                }
                Err(e) => eprintln!("Sensor {} error: {}", i, e),
            }
            thread::sleep(Duration::from_millis(500));
        }
    }

    Ok(())
}
