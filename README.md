# autoping-go
Golang pinger that logs disconnections from your internet service.

Maybe, like me, you're arguing with your internet service provider over the quality of your service. The speed could be an issue, but our main one is frequent disconnects, and a volatile latency that frequently gets into the 1s+ range.

`autoping-go` is a simple program that sends a ping to your choice of server every minute and logs the result. If a pong is not returned, it starts to log the outage and its duration.

The simplest form of running it is as a systemd unit file (if you're on Linux... and if you aren't, why not?), and an example unit file is given. It assumes that you have the binary `autoping-go` in `/opt`

Usage is simple: the program takes a single argument with the flag `-i`. The argument is the hostname or IP address of the server to be pinged. The program needs to run as root, as it logs to `/var/log` and uses privileged TCP ping.

Usage example:

`sudo autoping -i google.com`

## Example output

```
$ sudo autoping-go -i google.com

PING - 2018/06/10 15:23:25 16 bytes from 216.58.220.110: icmp_seq=0 time=37.357487ms
PING - 2018/06/10 15:23:34 16 bytes from 216.58.220.110: icmp_seq=0 time=37.023972ms
PING - 2018/06/10 15:23:44 16 bytes from 216.58.220.110: icmp_seq=0 time=37.327228ms
OUTAGE - 2018/06/10 15:25:44 Lost contact. Outage duration 2m0.04108828s
OUTAGE - 2018/06/10 15:25:54 Lost contact. Outage duration 2m10.044405828s
OUTAGE - 2018/06/10 15:26:04 Lost contact. Outage duration 2m20.044419641s
OUTAGE - 2018/06/10 15:26:14 Lost contact. Outage duration 2m30.047949132s
OUTAGE - 2018/06/10 15:26:24 Lost contact. Outage duration 2m40.051220104s
OUTAGE - 2018/06/10 15:26:34 Lost contact. Outage duration 2m50.055661296s
OUTAGE - 2018/06/10 15:26:44 Lost contact. Outage duration 3m0.059902265s
OUTAGE - 2018/06/10 15:26:54 Lost contact. Outage duration 3m10.064297127s
OUTAGE - 2018/06/10 15:27:04 Lost contact. Outage duration 3m20.069278177s
OUTAGE - 2018/06/10 15:27:14 Lost contact. Outage duration 3m30.070268253s
PING - 2018/06/10 15:27:26 16 bytes from 216.58.196.142: icmp_seq=0 time=1.421013537s
OUTAGE - 2018/06/10 15:27:27 Connection restored. Total outage duration 3m30.070268253s
PING - 2018/06/10 15:27:34 16 bytes from 216.58.196.142: icmp_seq=0 time=37.202811ms
PING - 2018/06/10 15:27:45 16 bytes from 216.58.196.142: icmp_seq=0 time=346.239576ms
PING - 2018/06/10 15:27:54 16 bytes from 216.58.196.142: icmp_seq=0 time=37.970454ms
PING - 2018/06/10 15:28:04 16 bytes from 216.58.196.142: icmp_seq=0 time=37.740339ms
PING - 2018/06/10 15:28:14 16 bytes from 216.58.196.142: icmp_seq=0 time=36.680617ms
PING - 2018/06/10 15:28:24 16 bytes from 216.58.196.142: icmp_seq=0 time=39.112287ms
PING - 2018/06/10 15:28:34 16 bytes from 216.58.196.142: icmp_seq=0 time=35.492549ms
PING - 2018/06/10 15:28:45 16 bytes from 216.58.196.142: icmp_seq=0 time=521.882374ms
OUTAGE - 2018/06/10 15:30:44 Lost contact. Outage duration 2m0.017020864s
OUTAGE - 2018/06/10 15:30:54 Lost contact. Outage duration 2m10.019852307s
OUTAGE - 2018/06/10 15:31:04 Lost contact. Outage duration 2m20.022637297s
OUTAGE - 2018/06/10 15:31:14 Lost contact. Outage duration 2m30.026521211s
OUTAGE - 2018/06/10 15:31:24 Lost contact. Outage duration 2m40.030266446s
OUTAGE - 2018/06/10 15:31:34 Lost contact. Outage duration 2m50.031741336s
OUTAGE - 2018/06/10 15:31:44 Lost contact. Outage duration 3m0.035288593s
OUTAGE - 2018/06/10 15:31:54 Lost contact. Outage duration 3m10.041750822s
OUTAGE - 2018/06/10 15:32:04 Lost contact. Outage duration 3m20.045677731s
OUTAGE - 2018/06/10 15:32:14 Lost contact. Outage duration 3m30.050107602s
OUTAGE - 2018/06/10 15:32:24 Lost contact. Outage duration 3m40.052401616s
OUTAGE - 2018/06/10 15:32:34 Lost contact. Outage duration 3m50.052697179s
OUTAGE - 2018/06/10 15:32:44 Lost contact. Outage duration 4m0.054037562s
OUTAGE - 2018/06/10 15:32:54 Lost contact. Outage duration 4m10.057908261s
OUTAGE - 2018/06/10 15:33:04 Lost contact. Outage duration 4m20.061704342s
PING - 2018/06/10 15:33:15 16 bytes from 216.58.196.142: icmp_seq=0 time=35.678541ms
OUTAGE - 2018/06/10 15:33:15 Connection restored. Total outage duration 4m20.061704342s
PING - 2018/06/10 15:33:25 16 bytes from 216.58.196.142: icmp_seq=0 time=37.199543ms
```
