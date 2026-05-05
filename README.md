# tp7

## What is this app

tp7 is a terminal UI for managing voice memos recorded on the
[Teenage Engineering TP-7](https://teenage.engineering/products/tp-7) field recorder.

> **Linux only.** tp7 relies on sysfs and the gvfs FUSE mount, which are
> Linux-specific. macOS and Windows are not supported.

Connect the device via USB and tp7 lets you:

- **Import** new recordings from the device into a local library
- **Browse** your library, organised by topic
- **Play back** recordings directly in the terminal
- **Ignore** recordings you don't want to import

tp7 uses the GNOME/gvfs MTP mount — no proprietary libraries or CGo required.

## Installation

### Prerequisites

Go 1.26.1 or later ([installation instructions](https://go.dev/doc/install)) and the gvfs MTP backend:

```bash
# Debian / Ubuntu / Linux Mint / Pop!_OS
sudo apt install gvfs-backends fuse3

# Fedora
sudo dnf install gvfs-mtp fuse3

# Arch Linux
sudo pacman -S gvfs-mtp fuse3
```

> **GNOME (or a gvfs-enabled desktop) is required at runtime.** `gvfsd-mtp`
> must be active to mount the TP-7 automatically under
> `/run/user/<uid>/gvfs/`. tp7 reads exclusively from this path.
>
> **KDE** uses KIO for MTP (`kio-extras`) and does **not** create a gvfs FUSE
> mount. tp7 will not find the device under KDE unless you install and start
> `gvfsd` manually alongside KDE.
>
> Headless servers are not supported.

### Install

**Option A — Download binary (no Go required)**

Download the latest binary from the
[Releases page](https://github.com/plue17/tp7/releases/latest):

```bash
curl -L https://github.com/plue17/tp7/releases/latest/download/tp7-linux-amd64 -o tp7
chmod +x tp7
mv tp7 ~/.local/bin/
```

**Option B — Build from source**

```bash
go install github.com/plue17/tp7/cmd/tp7@latest
```

The `tp7` binary is placed in `$GOPATH/bin` (typically `~/go/bin`), which should
already be on your `$PATH`.

### Usage

```
tp7 [-c config.yaml] [-d] [-l logfile]
```

| Flag | Description |
|------|-------------|
| `-c` | Path to a YAML config file (optional) |
| `-d` | Enable debug logging (use with `-l`) |
| `-l` | Write log output to a file |

Connect your TP-7, run `tp7`, and stop it with `Ctrl+C`.

Recordings are imported into the local library at `~/tp7/` by default.
The path can be changed via the `library.path` key in the config file.

## Disclaimer

tp7 is a **read-only** application. It accesses the TP-7 exclusively through
standard Linux kernel interfaces — sysfs (`/sys/bus/usb/devices`) and the
gvfs FUSE mount — and only ever reads files from the device. It does not write
to, modify, or delete any data on the TP-7.

The authors accept no responsibility for any damage to the device or any loss
of data stored on it. Such outcomes cannot be caused by this application.
