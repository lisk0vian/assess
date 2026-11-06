@echo off
chcp 65001 >nul
rem Assess (ax) runs exams in the classroom: the teacher's computer serves a
rem web page that students open from their own devices on the school network.
rem For that, Windows must accept inbound TCP connections on ports 27337-27366
rem (27337 and the next ones, used only when it is busy), on domain and
rem private networks. The rule is by port, not by program, so it keeps
rem working when ax is updated or run from a USB stick. ax only accepts
rem devices from the local network and needs no internet.
rem Run assess-firewall.cmd as administrator once per computer, or deploy the
rem same rule with a group policy.

netsh advfirewall firewall delete rule name="Assess (ax)"
netsh advfirewall firewall add rule name="Assess (ax)" dir=in action=allow protocol=TCP localport=27337-27366 profile=domain,private description="Assess exams: students' browsers on the classroom network"
pause
