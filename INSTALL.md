# Installation

## Prerequisites

### System

The app uses the gvfs MTP FUSE mount that GNOME/GTK creates automatically when
an MTP device is connected. No MTP libraries or CGo are required.

On Debian/Ubuntu:

```bash
sudo apt install gvfs-backends fuse3
```

`gvfs-backends` provides `gvfsd-mtp`, which exposes the device under
`/run/user/<uid>/gvfs/mtp:host=…`.

### Go

Go 1.26.1 or later is required to build the project:

```bash
go version
```

## Building

```bash
./build.sh
```

The `tp7` binary is placed in the project root directory.

## Configuration

The app looks for `tp7.yaml` in the current working directory by default.
The file is optional — without it, the built-in defaults
(Vendor ID `2367`, Product ID `0019`) for the Teenage Engineering TP-7 are used.

Example `tp7.yaml`:

```yaml
mtp_device:
  vendor_id: "2367"   # Teenage Engineering
  product_id: "0019"  # TP-7
```

## Usage

```
./tp7 [-c config.yaml] [-d]
```

| Flag | Description |
|------|-------------|
| `-c` | Path to the configuration file (optional) |
| `-d` | Enable debug logging |

The program runs as a foreground service and waits for the device to be
connected. Stop it with `Ctrl+C`.
