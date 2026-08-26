# orei-kvm

CLI, background daemon, and system tray for the **OREI USB3-EX2H330R-K** dual-host USB 3.2 / dual HDMI extender. Implements the full ASCII RS-232 command interface from the [device manual](https://cdn.shopify.com/s/files/1/1988/4253/files/USB3-EX2H330R-K_User_Manual_V1.1.pdf?v=1762423737).

Works on **macOS** and **Linux**.

## Why a daemon?

The RS-232 adapter is expected to sit on a **switched USB device port**. That means:

- Only the **currently selected host** can see the serial device
- After you switch away, the port disappears from this machine
- Polling port presence is how the app knows whether *this* machine is active

Configure `my_host: 1` or `2` so the daemon knows which host port this computer is wired to.

## Install

```bash
go install github.com/wiggs555/orei-kvm/cmd/orei-kvm@latest
```

Or from a checkout:

```bash
go build -o orei-kvm ./cmd/orei-kvm
```

Headless / no GUI libs:

```bash
go build -tags notray -o orei-kvm ./cmd/orei-kvm
```

## Quick start

```bash
# Write ~/.config/orei-kvm/config.yaml
orei-kvm config init

# Edit my_host / port as needed, then:
orei-kvm config show

# List serial devices
orei-kvm ports

# Run daemon + tray (Mac/Linux desktop)
orei-kvm daemon --tray

# Or tray alone (starts daemon if needed)
orei-kvm tray
```

Switch hosts:

```bash
orei-kvm host 1
orei-kvm host 2
orei-kvm host get
orei-kvm status
```

## Config (`~/.config/orei-kvm/config.yaml`)

```yaml
port: ""                 # empty = auto-detect USB serial adapters
baud: 115200
my_host: 1               # which host input this machine uses
poll_interval: 1s
socket: ~/.orei-kvm/orei-kvm.sock
port_patterns:
  - usbserial
  - usbmodem
  - ttyUSB
  - ttyACM
  - FTDI
  - CP210
  - CH340
cycle_on_active: false   # set true on the Mac to cycle USB after a host switch
cycle_side: rx
cycle_port: 0            # 0 = all ports; prefer the trackpad's RX port
cycle_delay: 15s
cycle_restore: on
```

Serial settings match the manual: **8N1**, commands terminated with `<CR><LF>`. Service port baud is fixed at 115200; phoenix RS-232 baud is configurable via `set baud`.

## Full command surface

| CLI | Device command |
|-----|----------------|
| `orei-kvm device-help` | `?` / `help` |
| `orei-kvm fw` | `get fw version` |
| `orei-kvm reboot` | `set reboot` |
| `orei-kvm reset --yes` | `set reset` + `Yes` |
| `orei-kvm device-status` | `get status` |
| `orei-kvm key on\|off\|get` | `set/get key` |
| `orei-kvm baud get\|set` | `get/set baud` |
| `orei-kvm host 1\|2\|get` | `set/get input` |
| `orei-kvm usb5v [0\|1\|2]` | `get usb5v` |
| `orei-kvm autoswitch on\|off\|get` | `set/get autoswitch` |
| `orei-kvm tx-usbd get\|set\|cycle` | `get/set tx usbd … power` |
| `orei-kvm rx-usbd get\|set\|cycle` | `get/set rx usbd … power` |
| `orei-kvm hdbt-update` | `set hdbt update` |
| `orei-kvm raw …` | arbitrary ASCII |

Most commands go through the daemon socket. Use `--direct --port /dev/ttyUSB0` for one-shot access without a daemon.

## System tray

The tray shows the active host (icon color + checkmark), polls once per second, and lets you switch Host 1 / Host 2 when the serial adapter is present. When this machine is inactive, switch actions are disabled — use the front-panel button or the other host.

## USB power-cycle (macOS HID)

After a host switch, some USB HID devices — notably an Apple Magic Trackpad — stay dark on macOS until the RX port is power-cycled. Keyboards usually switch without this.

```bash
# Same as: rx-usbd set 0 off && sleep 15 && rx-usbd set 0 on
orei-kvm rx-usbd cycle
orei-kvm --direct rx-usbd cycle 2 --delay 15s --restore on

# Cycle after an explicit switch (only works if serial is still present)
orei-kvm host 2 --cycle-rx
```

To do it automatically when **this** machine becomes the active host (recommended on the Mac), set `cycle_on_active: true` in config. Prefer `cycle_port` set to the trackpad's RX port so the keyboard stays up. Do not put the RS-232 adapter on a port you cycle.

## Mock mode (no hardware)

```bash
orei-kvm --mock --my-host 1 daemon &
orei-kvm --mock status
orei-kvm --mock host 2
orei-kvm --mock status   # connected=false, active_host=2
```

## Serial wiring notes

1. Prefer the **phoenix RS-232** port with a USB–serial adapter plugged into a **switched** USB device port on the TX/RX, **or** use the SERVICE USB-C virtual serial port if that port is also host-switched in your setup.
2. Default baud **115200 8N1**.
3. On each Mac/Linux host, set `my_host` to match the physical HOST 1 / HOST 2 cable you plugged into that computer.

## License

MIT
