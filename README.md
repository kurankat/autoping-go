# autoping-go
Golang pinger that logs disconnections from your internet service.

Maybe, like me, you're arguing with your internet service provider over the quality of your service. The speed could be an issue, but our main one is frequent disconnects, and a volatile latency that frequently gets into the 1s+ range.

`autoping-go` is a simple program that sends a ping to your choice of server every minute and logs the result. If a pong is not returned, it starts to log the outage and its duration.

The simplest form of running it is as a systemd unit file (if you're on Linux... and if you aren't, why not?), and an example unit file is given. It assumes that you have the binary `autoping-go` in `/opt`

Usage is simple: the program takes a single argument with the flag `-i`. The argument is the hostname or IP address of the server to be pinged. The program needs to run as root, as it logs to `/var/log`` and uses privileged TCP ping.

Usage example:

`sudo autoping -i google.com`
