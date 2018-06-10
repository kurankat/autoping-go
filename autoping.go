// Autoping is a small application to automatically ping a server every minute
// and log outages, keeping track of the duration of each outage

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"

	ping "github.com/sparrc/go-ping"
)

// Set up flags, loggers and global variables
var importFlag = flag.String("i", "", "IP address or hostname to be pinged")
var pLog, eLog, oLog *log.Logger
var lsPing time.Time // lsPing holds the last successful ping time
var ipAddr string    // User supplied IP address to ping to

func main() {
	// Parse user flags
	flag.Parse()

	// If the user has supplied an IP address or hostname, save it for later use.
	// If not, exit
	if len(*importFlag) > 0 {
		ipAddr = *importFlag
	} else {
		fmt.Println("You forgot to provide the IP address or hostname to be pinged")
		fmt.Println("Try 'sudo pingtests -i <IP ADDRESS or HOSTNAME>'")
		os.Exit(1)
	}

	// Set up log file
	logFile, err := os.OpenFile("/var/log/goping.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		panic("I'm having trouble writing to the log file")
		os.Exit(1)
	}
	defer logFile.Close() // Defer closing until the program is done

	// Set up loggers for ping results, errors, and outages
	pLog = log.New(logFile, "PING - ", log.LstdFlags)
	eLog = log.New(logFile, "ERROR - ", log.LstdFlags)
	oLog = log.New(logFile, "OUTAGE - ", log.LstdFlags)

	// Set up channel and goroutine to handle interrupts
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range c {
			eLog.Printf("Captured %v, stopping profiler and exiting..\n", sig)
			pprof.StopCPUProfile()
			os.Exit(1)
		}
	}()

	// Launch separate goroutine to carry out ping every minute
	for {
		go runPing()
		time.Sleep(1 * time.Minute)
	}
}

// Separate function to run pings
func runPing() {
	// Set up pinger and handle errors
	pinger, err := ping.NewPinger(ipAddr)
	if err != nil {
		eLog.Printf("ERROR: %s\n", err.Error())
		if lsPing.Year() == time.Now().Year() && time.Now().Sub(lsPing) > 2*time.Minute {
			oLog.Printf("Lost contact. Outage duration %.0f minutes",
				time.Now().Sub(lsPing).Minutes())
		}
	} else {

		// Pinger settings.
		pinger.Count = 1
		pinger.Timeout = 30 * time.Second
		pinger.SetPrivileged(true) // Needed to process TCP pings

		// What to do when ping comes in: log results
		pinger.OnRecv = func(pkt *ping.Packet) {
			pLog.Printf("%d bytes from %s: icmp_seq=%d time=%v", pkt.Nbytes, pkt.IPAddr,
				pkt.Seq, pkt.Rtt)
		}

		t := time.Now() // Keep track of the time the ping was sent
		pinger.Run()    // Send the ping

		// If no packets come back after timeout, start logging outage after 2 min
		// since last successful ping (2 missed pings in a row)
		if pinger.Statistics().PacketsRecv == 0 {
			// The following conditions have to be met: the ping year of the last
			// successful ping has to be this year (at the start of the run lsPing is
			// set to 0) AND the time difference between the last successful ping and
			// this one has to be more than 2 minutes
			if lsPing.Year() == time.Now().Year() && t.Sub(lsPing) > 2*time.Minute {
				oLog.Printf("Lost contact. Outage duration %.0f minutes", t.Sub(lsPing).Minutes())
			}
		}

		// If we get a packet back, reset lsPing to the time this ping was fired
		if pinger.Statistics().PacketsRecv > 0 {
			lsPing = t
		}
	}
}
