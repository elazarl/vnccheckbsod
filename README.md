# Test KVM VM for BSOD

## Intro

When running a VM with a GUI, it is useful to know whether or not a bluescreen
happened.

When I refer to BSOD, I mean any type of BSOD like panic, whether it's BSOD in
Windows, a black console screen describing kernel panic in Linux, or a pink
screen in ESX.

This program runs a command line that should run a VM, waits a certain amount of
time, and then reads the VM screen from VNC.

If it detects the screen has less than 10 colors overall, it assumes we have a
BSOD. Typical Windows and Linux bluescreens has much less total amount of colors.

Otherwise it would reboot the machine and would try again.

## Usage

Typical usage:

```
$ ./vnctest -histogram 192.168.1.10:5900
$ # prints how many colors are there in the given VNC port

$ # runs 'kvm -snapshot -drive file=img.qcow2 -vnc :%p' on a certain port
$ # replaces %p with a known VNC port.
$ # waits 10 minutes, and then tests the VNC connection to try and detect BSOD
$ ./vnctest -qemu 'kvm -m 1G -snapshot -drive file=img.qcow2 -vnc :%p' -settle 10min

$ # Runs 10 VMs with VNC servers on ports 5977-5787, checks all of them for BSOD
$ ./vnctest -qemu 'kvm -m 1G -snapshot -drive file=img.qcow2 -vnc :%p' -n 10 -settle 10min

```

## Example output

When checking VNC screen and not finding BSOD a single line would be printed

   pid 1706 hist 1850 pid 1703 hist 969 pid 1704 hist 969 48h43m35.804801472s passed

When finding a BSOD

   Found BSOD in pid 1775 after 48h54m21.027502156s screen /tmp/vnctest000-pid13392.png on VNC port 77
