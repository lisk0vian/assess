Assess (ax) runs exams in the classroom: the teacher's computer serves a
web page that students open from their own devices on the school network.
For that, Windows must accept inbound TCP connections on ports 27337-27366
(27337 and the next ones, used only when it is busy), on domain and
private networks. The rule is by port, not by program, so it keeps
working when ax is updated or run from a USB stick. ax only accepts
devices from the local network and needs no internet.
Run assess-firewall.cmd as administrator once per computer, or deploy the
same rule with a group policy.

netsh advfirewall firewall add rule name="Assess (ax)" dir=in action=allow protocol=TCP localport=27337-27366 profile=domain,private description="Assess exams: students' browsers on the classroom network"

To undo it:
netsh advfirewall firewall delete rule name="Assess (ax)"
