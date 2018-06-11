// Autoping is a small application to automatically ping a server every minute
// and log outages, keeping track of the duration of each outage

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"

	ping "github.com/sparrc/go-ping"
)

type dLatPing struct {
	crDod   bool          // Is the previous ping latency dodgy?
	prDod   bool          // Did the previous ping of dodgy latency?
	latency time.Duration // Latency of latest ping
	pTime   time.Time     // Time latest ping was fired
	nDodg   int
}

type connTracker struct {
	isOutage           bool
	lastSuccessfulPing time.Time
	outageDuration     time.Duration
}

type queue []float64

// Set up flags, loggers and global variables
var importFlag = flag.String("i", "", "IP address or hostname to be pinged")
var pLog, eLog, oLog *log.Logger
var ipAddr string // User supplied IP address to ping to

var connInfo = connTracker{isOutage: false}

var spl []dLatPing // List of recent pings with dodgy latency
var latSlice queue
var meanLat time.Duration

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
	t := time.Now() // Keep track of the time the ping was sent
	pinger, err := ping.NewPinger(ipAddr)
	if err != nil {
		switch err.(type) {
		case *net.DNSError:
			if connInfo.lastSuccessfulPing.Year() == t.Year() &&
				t.Sub(connInfo.lastSuccessfulPing) > 2*time.Minute {
				connInfo.isOutage = true
				connInfo.outageDuration = time.Now().Sub(connInfo.lastSuccessfulPing)
				oLog.Printf("Lost contact. Outage duration %v",
					connInfo.outageDuration)
			}
		default:
			panic(err)
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
			evaluateLatency(t, pkt.Rtt)
		}
		pinger.Run() // Send the ping

		// If no packets come back after timeout, start logging outage after 2 min
		// since last successful ping (2 missed pings in a row)
		if pinger.Statistics().PacketsRecv == 0 {
			// The following conditions have to be met: the ping year of the last
			// successful ping has to be this year (at the start of the run lsPing is
			// set to 0) AND the time difference between the last successful ping and
			// this one has to be more than 2 minutes
			oLog.Printf("Timeout - Missed pong")
			if connInfo.lastSuccessfulPing.Year() == t.Year() &&
				t.Sub(connInfo.lastSuccessfulPing) > 2*time.Minute {
				connInfo.isOutage = true
				connInfo.outageDuration = time.Now().Sub(connInfo.lastSuccessfulPing)
				oLog.Printf("Lost contact. Outage duration %v minutes",
					connInfo.outageDuration)
			}
		}

		// If we get a packet back, reset last successful ping time to the time this
		// ping was fired, and reset outage
		if pinger.Statistics().PacketsRecv > 0 {
			if connInfo.isOutage {
				oLog.Printf("Connection restored. Total outage duration %v",
					connInfo.outageDuration)
			}
			connInfo.lastSuccessfulPing = t
			connInfo.isOutage = false
		}
	}
}

// Evaluate latency of supplied ping. If ping has a long latency, add it to the
// queue. If ping is normal (< 100 ms) then check if previous ping was also
// normal. If so, finalise spl and log total duration of dodgy latency pings.
// If previous ping was dodgy, ignore single normal ping and keep logging
func evaluateLatency(t time.Time, rtt time.Duration) {
	meanLat = time.Duration(latSlice.mean()) * time.Nanosecond
	cutoff := meanLat * 3
	prd := false // The provious ping is never dodgy by default

	// Set up the provious dodgy ping to be that of the last item in spl
	if len(spl) > 0 {
		fmt.Println("spl is longer than 1. Setting prd to previous")
		prd = spl[len(spl)-1].crDod
	}

	// If the ping RTT is more than the cutoff, treat as a dodgy ping and append
	if rtt > cutoff && cutoff > 0 {
		dPing := dLatPing{crDod: true, prDod: prd, latency: rtt, pTime: t}
		spl = append(spl, dPing)
		fmt.Printf("RTT of %v is longer than cutoff of %v. Appendling to spl\n", rtt, cutoff)
		fmt.Println(latSlice, meanLat)
	} else {
		latSlice.add(float64(rtt.Nanoseconds()))
		fmt.Println(latSlice, meanLat)
		// If the latency is OK, check that of previous. If that one dodgy, keep logging
		if prd {
			dPing := dLatPing{crDod: false, prDod: prd, latency: rtt, pTime: t}
			spl = append(spl, dPing)
			fmt.Printf("RTT of %v is ok but previous bad. Appendling to spl\n", rtt)
		} else { // If two decent latency pings in a row, then log total and reset spl
			if len(spl) > 2 {
				startTime := spl[0].pTime
				endTime := spl[len(spl)-1].pTime
				fmt.Printf("Period of flakey latency. Duration = %v",
					endTime.Sub(startTime))
				oLog.Printf("Period of flakey latency. Duration = %v",
					endTime.Sub(startTime))
				spl = nil
			} else {
				fmt.Println("No current issues")
			}

		}
	}

}

func (q *queue) add(f float64) {
	iq := []float64(*q)
	if len(iq) < 10 {
		iq = append(iq, f)
	} else {
		iq = iq[1:]
		iq = append(iq, f)
	}
	*q = queue(iq)
}

func (q *queue) mean() (m float64) {
	var total float64
	iq := []float64(*q)
	for i := range iq {
		total += iq[i]
	}
	m = total / float64(len(iq))
	return m
}
