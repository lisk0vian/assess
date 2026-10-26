# Classroom network

Students open a web page served by the teacher's computer. Nothing is
installed on their devices and nothing needs the internet. This page explains
what the network must allow, and what to do when the teacher is not an
administrator of the classroom computer.

## Which devices get in

`ax start` listens on every network interface of the computer (like
`vite --host 0.0.0.0`) and shows the likeliest address first: the real
Wi-Fi or Ethernet adapter. Virtual adapters (WSL, Hyper-V, VirtualBox,
VMware, Docker, VPNs such as Tailscale) come last. In the panel's **Link**
view, ←→ switches between the addresses, QR code included.

These devices are let in:

- private addresses (`10.x`, `172.16–31.x`, `192.168.x`, IPv6 ULA);
- carrier-grade NAT (`100.64.0.0/10`), used inside some school networks;
- link-local addresses;
- **any address in the subnets of the teacher's own computer**, so schools
  that number their network with public ranges work too.

Anything else is refused, so a leaked link or code is useless outside the
school. `server.allow_public: true` lifts that limit; use it only behind your
own tunnel or proxy.

## Windows firewall: why it asks for an administrator

A Windows program that receives connections needs a firewall rule, and only
an administrator can create one. The window Windows shows the first time ax
starts an exam does exactly that. Windows ties the rule to the folder of the
program, so a copy in another folder, or on a USB stick that gets another
drive letter, asks again.

`ax firewall` (or `ax firewall check`) shows, without any special rights:

- each connected network and its category (domain, private, public);
- whether students can reach ax there.

It reads the firewall rules directly and needs no administrator. `ax start`
runs the same check while the server starts, and warns if students would be
kept out.

### The fix, once per computer

`ax firewall setup` creates **one rule by port, not by program**: inbound TCP
on `server.port` and the next 29 ports (the server only moves within that
range when a port is busy), on domain and private networks. Because it does
not depend on the folder, every copy and every future version of ax works
from then on.

- When ax is not running as administrator, Windows asks for an
  administrator's password in its own window. Whoever has it types it once;
  nobody needs to stay.
- `--public` also opens networks Windows marks as public. Classroom Wi-Fi is
  often marked public, and `ax firewall` says so. It is better to have IT
  mark the school network as private.
- `ax firewall remove` takes the rule away.

### Without waiting for anyone

`ax firewall script` writes two files for the school's IT staff:

- `assess-firewall.cmd`: run it as administrator, or deploy the same rule to
  every classroom computer with a group policy;
- a short note explaining what the rule opens and why.

Hand the files over once and the problem disappears for every teacher and
every classroom.

### Meanwhile

- **Use a computer you administer.** Your own laptop is the usual choice.
  `ax portable init E:\ax --copy-vault` puts ax, its settings (`ax.yaml`) and
  a copy of your vault in one folder or USB stick; plug it in and run
  `E:\ax\ax.exe`. Everything stays in that folder, so it works on any
  computer, with any drive letter.
- **Linux or macOS.** On Linux, ports above 1024 need no permission. On
  macOS the application firewall may ask once; any user can answer on a Mac
  they administer.
- **Your phone's hotspot.** Connect the laptop and the students to it. This
  helps when the school Wi-Fi isolates devices from each other.
- **Hotspot of the classroom PC.** A Windows hotspot still goes through the
  same firewall, so it does not avoid the rule.

### What is not possible

On a managed Windows computer with no rule and no administrator, there is no
legitimate way to accept connections. ax detects that and says so, and the
panel suggests `ax firewall` when nobody has joined after two minutes, rather
than leaving everyone waiting.
