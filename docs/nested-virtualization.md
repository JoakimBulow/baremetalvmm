# Nested Virtualization

A nested VM can run VMs of its own: it has `/dev/kvm`, so you can run Firecracker, QEMU, Kata Containers, KubeVirt, or vmm itself inside it.

Firecracker can't do this, because it doesn't expose the CPU's virtualization extensions (VT-x/AMD-V) to guests. Nested VMs therefore run under [Cloud Hypervisor](https://github.com/cloud-hypervisor/cloud-hypervisor), a second hypervisor backend. Firecracker stays the default for every other VM, and the two can run side by side on the same host.

## Requirements

- An x86_64 host with nested virtualization enabled in KVM (check below)
- The `cloud-hypervisor` binary, which the installer adds with `--with-cloud-hypervisor`
- A guest kernel with KVM: the `nested-kernel` release

## Setup

### 1. Check nested KVM is enabled

```bash
cat /sys/module/kvm_intel/parameters/nested   # Intel: Y
cat /sys/module/kvm_amd/parameters/nested     # AMD: 1
```

It's on by default on most recent kernels. If it isn't, enable it and reboot (or reload the module with no VMs running):

```bash
echo 'options kvm_intel nested=1' | sudo tee /etc/modprobe.d/kvm-nested.conf   # kvm_amd on AMD
```

### 2. Install Cloud Hypervisor

```bash
# New install, or re-run the installer on an existing one
curl -fsSL https://raesene.github.io/baremetalvmm/install.sh | sudo bash -s -- --with-cloud-hypervisor

# Or with vmm upgrade
sudo vmm upgrade --with-cloud-hypervisor
```

Cloud Hypervisor doesn't publish checksums, so vmm pins the SHA256 of the static binary it installs, and refuses a download that doesn't match. Once Cloud Hypervisor is installed, the installer and `vmm upgrade` keep it up to date like Firecracker, and `vmm upgrade --rollback` covers it.

### 3. Download the nested kernel

```bash
sudo vmm kernel pull nested-kernel
```

The installer does this for you when Cloud Hypervisor is installed. You can also build it yourself:

```bash
sudo vmm kernel build --version 6.1 --name nested-kernel --profile nested
```

## Creating Nested VMs

```bash
sudo vmm create lab --nested --cpus 2 --memory 2048 --disk 8192
sudo vmm start lab
vmm ssh lab -- ls -l /dev/kvm
```

`--nested` selects Cloud Hypervisor and, unless you pass `--kernel`, the `nested-kernel` kernel. vmm checks the host before creating the VM, so a missing binary, missing kernel or disabled nested KVM is reported straight away.

Everything else works as it does for Firecracker VMs: SSH, `vmm cp`, `vmm console`, port forwards, host directory mounts, DNS settings, autostart and the web terminal and file browser.

`vmm list` shows each VM's backend in its last column:

```
NAME   ID        STATE    CPUs  MEMORY   IP ADDRESS  BACKEND
dev    1e2e5700  running  1     512 MB   172.16.0.2  firecracker
lab    67a12c8c  running  2     2048 MB  172.16.0.3  cloud-hypervisor+nested
```

### Running VMs inside

The nested kernel is the default kernel plus KVM and vhost, so a nested VM can run anything that needs `/dev/kvm`. For example, Firecracker:

```bash
sudo vmm cp /usr/local/bin/firecracker lab:/usr/local/bin/
sudo vmm cp /var/lib/vmm/images/kernels/vmlinux.bin lab:/root/
sudo vmm cp /var/lib/vmm/images/rootfs/rootfs.ext4 lab:/root/
```

To run vmm itself inside a nested VM, give the inner vmm a different subnet before creating VMs. The nested VM is already on the host's `172.16.0.0/16` network, so the default would clash with its own address and gateway. Set `subnet` and `gateway` in the inner vmm's `~/.config/vmm/config.json` (see [Configuration](configuration.md)):

```json
{
  "subnet": "172.17.0.0/16",
  "gateway": "172.17.0.1"
}
```

Give a nested VM enough memory for its own guests (2 GB or more), and expect its guests to be slower than first-level VMs.

### Cloud Hypervisor without nesting

```bash
sudo vmm create plain --backend cloud-hypervisor
```

This runs an ordinary VM under Cloud Hypervisor, with the default kernel. It's mainly useful for comparing the two hypervisors.

### Web UI and API

The web UI's create form has a **Nested virtualization** checkbox, and a VM's page shows its backend. The JSON API takes the same options:

```bash
curl -X POST -H "Authorization: Bearer <session-token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"lab","cpus":2,"memory_mb":2048,"nested":true}' \
  http://localhost:8080/api/v1/vms
```

`"backend":"cloud-hypervisor"` selects the backend without nesting.

## How It Works

- **Same images.** Cloud Hypervisor boots the same kernels and root filesystems as Firecracker. Drives appear in the same order (`/dev/vda` for the rootfs, then mounts as `vdb`, `vdc`...), and the guest gets its address from the same `ip=` boot argument on the same TAP device and bridge.
- **One process per VM.** vmm starts `cloud-hypervisor` with the VM's API socket and waits until the VM reports it is running. If it exits instead, the start fails with the last lines of its log.
- **Clean shutdown.** Cloud Hypervisor asks a guest to stop with the ACPI power button. The rootfs images have no udev, so systemd-logind ignores the button. vmm therefore installs a small `vmm-powerbutton.service` in Cloud Hypervisor guests that powers off when it's pressed, which needs the nested kernel's input and ACPI button drivers. If the guest doesn't power off within 3 seconds (for example with the default kernel), vmm falls back to SIGTERM, then SIGKILL, as it does for Firecracker.
- **The VM records its backend.** A VM's JSON file has `"backend": "cloud-hypervisor"` and `"nested": true`. Firecracker VMs have no `backend` key, so older vmm versions can still read them.

## Limitations

- **No snapshots.** `vmm snapshot` is refused for Cloud Hypervisor VMs, and the web UI says so. `vmm image snapshot` (rootfs only) works.
- **x86_64 only.** Nested virtualization on Arm needs CPU features that are still rare.
- **No live migration**, and a VM's backend can't be changed after it is created.
- **Performance.** Nested guests pay for two layers of virtualization, so CPU- and I/O-heavy work is noticeably slower in them.
- **Security.** Giving a guest hardware virtualization adds KVM attack surface on the host. Only use nested VMs for workloads that need them.

## Troubleshooting

| Symptom | Check |
|---------|-------|
| `nested virtualization is disabled on this host` | Enable nested KVM (step 1) |
| `cloud-hypervisor not found` | Install it (step 2) |
| `nested VMs need a kernel with KVM support` | `sudo vmm kernel pull nested-kernel`, or pass `--kernel` |
| `cloud-hypervisor exited while starting the VM` | `/var/lib/vmm/logs/<name>.log` has Cloud Hypervisor's own output |
| No `/dev/kvm` in the guest | The VM was created with a kernel built without KVM. Recreate it with the nested kernel |
| Every stop takes about 3 seconds | The guest didn't see the power button and was stopped with SIGTERM. Use the nested kernel |
| Boot messages and kernel panics | `sudo vmm console <name> --full` |
