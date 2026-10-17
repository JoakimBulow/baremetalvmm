# Troubleshooting

## Debugging

```bash
cat /var/lib/vmm/vms/<name>.json          # VM state
cat /var/lib/vmm/logs/<name>.log          # Firecracker or Cloud Hypervisor logs
sudo vmm console <name> --full            # Serial console output (kernel boot, panics)
sudo vmm console <name>                   # Tail + follow console output live
ip link show vmm-br0                       # Bridge
sudo iptables -t nat -L -n -v             # NAT rules
ps aux | grep -E 'firecracker|cloud-hypervisor'   # Processes
sudo vmm ssh myvm -- 'getent hosts google.com'  # DNS from VM
```

## KVM Not Available

```
Error: /dev/kvm not found
```

Ensure:
1. Your CPU supports virtualization (Intel VT-x or AMD-V)
2. Virtualization is enabled in BIOS
3. KVM modules are loaded: `sudo modprobe kvm_intel` or `sudo modprobe kvm_amd`

## Permission Denied on /dev/kvm

```bash
# Add your user to the kvm group
sudo usermod -aG kvm $USER
# Log out and back in
```

## Network Not Working in VM

Ensure IP forwarding is enabled:
```bash
sudo sysctl -w net.ipv4.ip_forward=1
```

Check iptables rules:
```bash
sudo iptables -t nat -L -n
```

Test connectivity from host:
```bash
ping 172.16.0.2
```

## VM Can't Reach the Internet

Verify the `host_interface` in your config matches your actual network interface:
```bash
# Find your network interface
ip route | grep default
# Example output: default via 192.168.1.1 dev wlp3s0

# Check your config
cat ~/.config/vmm/config.json

# Update host_interface if needed (e.g., change "eth0" to "wlp3s0")
```

After updating the config, restart your VM for the NAT rules to be recreated with the correct interface.

## Port Forward Doesn't Respond

1. Check its status: `sudo vmm port-forward list`. If it shows `missing` or `outdated` (common after upgrading from an older vmm while VMs were running), run `sudo vmm port-forward apply <vm>`.
2. Check the service in the VM is listening on all interfaces, not just localhost: `sudo vmm ssh <vm> -- ss -ltnu`. A service bound to `127.0.0.1` can't be reached through a forward.
3. Test from the host with its LAN address (`curl http://<host-ip>:<port>`); `127.0.0.1:<port>` is not forwarded.
4. Inspect the rules: `sudo iptables -t nat -S | grep <port>` should show a `PREROUTING` and an `OUTPUT` DNAT rule, and `sudo iptables -S FORWARD | head -3` should include `-o vmm-br0 -m conntrack --ctstate DNAT -j ACCEPT`.

## VM Won't Start

Check the serial console log for kernel boot errors:
```bash
sudo vmm console <name> --full
```

Or check the Firecracker log:
```bash
cat /var/lib/vmm/logs/<vmname>.log
```

Check Firecracker socket:
```bash
ls -la /var/lib/vmm/sockets/
```

## Stopping a VM Takes 30 Seconds

vmm asks the guest to shut down (Ctrl+Alt+Del under Firecracker, the ACPI power button under Cloud Hypervisor) and waits up to 30 seconds for it to finish before sending a signal, which would lose any writes the guest hadn't flushed. A guest that shuts down cleanly stops in well under a second, or a few seconds for a Kubernetes node. If every stop takes 30 seconds, the VM's kernel can't see the request: kernels released before v0.17.0 were built without the AT keyboard driver Firecracker needs. Download current ones:

```bash
sudo vmm kernel pull vmlinux.bin --force   # the default kernel
sudo vmm kernel pull k8s-kernel --force    # and any other kernels you use
```

The new kernel is used from the VM's next start.

## VM Shows as Stopped When Running

Check with `sudo vmm list`. Without sudo, vmm can't read its data directory (`/var/lib/vmm` is root-only) and fails with "permission denied". A VM's state comes from its hypervisor process, not its socket file, so a VM whose process is alive is reported as running even if the socket has gone missing.
