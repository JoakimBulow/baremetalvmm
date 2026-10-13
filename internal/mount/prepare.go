package mount

import (
	"fmt"

	"github.com/raesene/baremetalvmm/internal/firecracker"
	"github.com/raesene/baremetalvmm/internal/image"
	"github.com/raesene/baremetalvmm/internal/vm"
)

// guestDevice is the guest block device for the i'th attached mount drive.
// Firecracker attaches drives in order after the rootfs (vda).
func guestDevice(i int) string {
	return fmt.Sprintf("/dev/vd%c", 'b'+i)
}

// PrepareVM builds the VM's mount images, writes their entries into the
// guest fstab and returns the drives to attach, in device order. When skip
// is nil, a mount whose image can't be built fails the start. Otherwise skip
// is called with the error and the mount is left out. Devices are numbered
// by attached drive, not by position in v.Mounts, so a skipped mount doesn't
// shift the later ones onto the wrong device.
func (m *Manager) PrepareVM(v *vm.VM, skip func(*vm.Mount, error)) ([]firecracker.MountDrive, error) {
	if len(v.Mounts) == 0 {
		return nil, nil
	}
	var entries []image.MountEntry
	var drives []firecracker.MountDrive
	for i := range v.Mounts {
		mnt := &v.Mounts[i]
		if err := m.CreateMountImage(mnt, v.Name); err != nil {
			if skip == nil {
				return nil, fmt.Errorf("failed to create mount image for '%s': %w", mnt.GuestTag, err)
			}
			skip(mnt, err)
			continue
		}
		entries = append(entries, image.MountEntry{
			Device:    guestDevice(len(drives)),
			MountPath: "/mnt/" + mnt.GuestTag,
			ReadOnly:  mnt.ReadOnly,
		})
		drives = append(drives, firecracker.MountDrive{
			ImagePath: mnt.ImagePath,
			Tag:       mnt.GuestTag,
			ReadOnly:  mnt.ReadOnly,
		})
	}
	if err := image.InjectMountFstab(v.RootfsPath, entries); err != nil {
		return nil, fmt.Errorf("failed to inject mount fstab: %w", err)
	}
	return drives, nil
}
