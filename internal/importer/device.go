package importer

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry represents a voice memo file on the device.
type Entry struct {
	Name string
	Size int64
	Path string // absolute path on the local filesystem
}

// DeviceMount represents a gvfs-mounted MTP device accessible via the
// local filesystem. No USB library is required.
type DeviceMount struct {
	// MountPath is the root directory of the gvfs FUSE mount,
	// e.g. /run/user/1000/gvfs/mtp:host=teenage_engineering_TP-7_MTP_Device_TPBXB16H
	MountPath string
}

// Exists returns true if the gvfs mount directory entry is still present.
// It checks the parent directory listing rather than touching the FUSE mount
// itself, which avoids blocking on in-progress MTP transactions.
func (d *DeviceMount) Exists() bool {
	parent := filepath.Dir(d.MountPath)
	name := filepath.Base(d.MountPath)
	entries, err := os.ReadDir(parent)
	if err != nil {
		slog.Debug("importer: Exists: ReadDir failed", "parent", parent, "err", err)
		return false
	}
	slog.Debug("importer: Exists: gvfs dir contents", "parent", parent, "count", len(entries))
	for _, e := range entries {
		slog.Debug("importer: Exists: entry", "name", e.Name(), "want", name)
		if e.Name() == name {
			return true
		}
	}
	return false
}

// recordingsPath is the path to the recordings directory within the mount.
const recordingsPath = "TP-7 MTP Device/recordings"

// ListRecordings returns all voice memo entries from the recordings directory.
func (d *DeviceMount) ListRecordings() ([]Entry, error) {
	full := filepath.Join(d.MountPath, recordingsPath)
	des, err := os.ReadDir(full)
	if err != nil {
		return nil, fmt.Errorf("reading recordings directory: %w", err)
	}

	entries := make([]Entry, 0, len(des))
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Name: de.Name(),
			Size: info.Size(),
			Path: filepath.Join(full, de.Name()),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name > entries[j].Name })
	return entries, nil
}

// FindDeviceMount searches for a gvfs MTP mount belonging to the USB device
// identified by vendorID and productID (both in hex without prefix, e.g.
// "2367" and "0019"). It reads the USB string descriptors from sysfs to
// reconstruct the gvfs mount directory name, which uses the format:
//
//	mtp:host=<manufacturer>_<product>_<serial>
//
// Returns an error if the USB device is not found in sysfs, or if no
// matching gvfs mount exists yet.
func FindDeviceMount(vendorID, productID string) (*DeviceMount, error) {
	sysPath, err := findSysfsDevice(vendorID, productID)
	if err != nil {
		return nil, err
	}
	slog.Debug("importer: FindDeviceMount: found sysfs device", "path", sysPath)

	// Build the gvfs host name from USB string descriptors.
	// gvfs replaces spaces with underscores and joins the three strings.
	var parts []string
	for _, attr := range []string{"manufacturer", "product", "serial"} {
		val, err := readSysAttr(sysPath, attr)
		if err != nil || val == "" {
			slog.Debug("importer: FindDeviceMount: sysfs attr missing or empty", "attr", attr, "err", err)
			continue
		}
		slog.Debug("importer: FindDeviceMount: sysfs attr", "attr", attr, "value", val)
		parts = append(parts, strings.ReplaceAll(val, " ", "_"))
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no USB string descriptors found for device %s:%s", vendorID, productID)
	}

	hostName := strings.Join(parts, "_")
	uid := os.Getuid()
	pattern := fmt.Sprintf("/run/user/%d/gvfs/mtp:host=%s*", uid, hostName)
	slog.Debug("importer: FindDeviceMount: glob pattern", "pattern", pattern)

	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		// Log all current gvfs entries to aid diagnosis.
		gvfsDir := fmt.Sprintf("/run/user/%d/gvfs", uid)
		if all, readErr := os.ReadDir(gvfsDir); readErr == nil {
			for _, e := range all {
				slog.Debug("importer: FindDeviceMount: gvfs entry", "name", e.Name())
			}
		}
		return nil, fmt.Errorf("no gvfs mount found for device %s:%s (host=%s)", vendorID, productID, hostName)
	}
	slog.Debug("importer: FindDeviceMount: matched mount", "path", matches[0])

	return &DeviceMount{MountPath: matches[0]}, nil
}

// findSysfsDevice scans /sys/bus/usb/devices for a device with the given
// vendor and product IDs and returns its sysfs device path.
func findSysfsDevice(vendorID, productID string) (string, error) {
	const sysPath = "/sys/bus/usb/devices"

	entries, err := os.ReadDir(sysPath)
	if err != nil {
		return "", fmt.Errorf("reading sysfs: %w", err)
	}

	// Normalize to lowercase, strip leading zeros for comparison.
	wantVendor := strings.ToLower(strings.TrimLeft(vendorID, "0"))
	wantProduct := strings.ToLower(strings.TrimLeft(productID, "0"))

	for _, e := range entries {
		// Interface entries (contain ":") are skipped.
		if strings.Contains(e.Name(), ":") {
			continue
		}

		base := filepath.Join(sysPath, e.Name())

		vid, err := readSysAttr(base, "idVendor")
		if err != nil {
			continue
		}
		pid, err := readSysAttr(base, "idProduct")
		if err != nil {
			continue
		}

		if strings.ToLower(strings.TrimLeft(vid, "0")) != wantVendor ||
			strings.ToLower(strings.TrimLeft(pid, "0")) != wantProduct {
			continue
		}

		return base, nil
	}

	return "", fmt.Errorf("USB device %s:%s not found in sysfs", vendorID, productID)
}

// readSysAttr reads a single-line sysfs attribute file and returns the
// trimmed string value.
func readSysAttr(devicePath, attr string) (string, error) {
	b, err := os.ReadFile(filepath.Join(devicePath, attr))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
