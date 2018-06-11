// Autoping is a small application to automatically ping a server every minute
// and log outages, keeping track of the duration of each outage

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
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
}

type connTracker struct {
	isOutage           bool
	lastSuccessfulPing time.Time
	outageDuration     time.Duration
}

// Set up flags, loggers and global variables
var importFlag = flag.String("i", "", "IP address or hostname to be pinged")
var traceFlag = flag.Bool("t", false, "turn on trace to file")
var pLog, eLog, oLog, tLog *log.Logger
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
	tLog = log.New(ioutil.Discard, "TRACE - ", log.LstdFlags)

	if *traceFlag {
		tLog.SetOutput(logFile)
	}

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
	tLog.Printf("Setting up channel to handle interrupts")

	// Launch separate goroutine to carry out ping every minute
	for {
		tLog.Printf("Running ping now")
		go runPing()
		time.Sleep(1 * time.Minute)
	}
}

// Separate function to run pings
func runPing() {
	// Set up pinger and handle errors
	t := time.Now() // Keep track of the time the ping was sent
	tLog.Printf("Setting Ping time to %v", t)
	pinger, err := ping.NewPinger(ipAddr)
	if err != nil {
		switch err.(type) {
		case *net.DNSError:
			if connInfo.lastSuccessfulPing.Year() == t.Year() &&
				t.Sub(connInfo.lastSuccessfulPing) > 2*time.Minute {
				connInfo.isOutage = true
				connInfo.outageDuration = time.Now().Sub(connInfo.lastSuccessfulPing)
				tLog.Printf("DNS error")
				oLog.Printf("Lost contact. Outage duration %v",
					connInfo.outageDuration)
			}
		default:
			panic(err)
		}
	} else {
		// Pinger settings.
		pinger.Count = 1
		tLog.Printf("Setting pinger count to %v", pinger.Count)
		pinger.Timeout = 30 * time.Second
		tLog.Printf("Setting pinger timeout to %v", pinger.Timeout)
		pinger.SetPrivileged(true) // Needed to process TCP pings
		tLog.Printf("Setting pinger to privileged")

		// What to do when ping comes in: log results
		pinger.OnRecv = func(pkt *ping.Packet) {
			pLog.Printf("%d bytes from %s: icmp_seq=%d time=%v", pkt.Nbytes, pkt.IPAddr,
				pkt.Seq, pkt.Rtt)
		}
		pinger.OnFinish = func(s *ping.Statistics) {
			// If no packets come back after timeout, start logging outage after 2 min
			// since last successful ping (2 missed pings in a row)
			if s.PacketsRecv == 0 {
				tLog.Printf("Pinger timed out")
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
			} else if s.PacketsRecv > 0 {
				// If we get a packet back, reset last successful ping time to the time this
				// ping was fired, and reset outage
				if connInfo.isOutage {
					oLog.Printf("Connection restored. Total outage duration %v",
						connInfo.outageDuration)
				}
				connInfo.lastSuccessfulPing = t
				connInfo.isOutage = false
				tLog.Printf("Packet recieved. Sending to evaluateLatency()")
				evaluateLatency(t, s.MinRtt)
			}
		}
	}
	pinger.Run() // Send the ping
}

// Evaluate latency of supplied ping. If ping has a long latency, add it to the
// queue. If ping is normal (< 100 ms) then check if previous ping was also
// normal. If so, finalise spl and log total duration of dodgy latency pings.
// If previous ping was dodgy, ignore single normal ping and keep logging
func evaluateLatency(t time.Time, rtt time.Duration) {
	tLog.Printf("Evaluating Pong sent at %v with RTT of %v", t, rtt)
	meanLat = time.Duration(latSlice.mean()) * time.Nanosecond
	tLog.Printf("meanLat is currently %v", meanLat)
	cutoff := meanLat * 3
	prd := false // The previous ping is never dodgy by default

	// Set up the provious dodgy ping to be that of the last item in spl
	if len(spl) > 0 {
		prd = spl[len(spl)-1].crDod // Set prd to the RTT of the previous dodgy ping
		tLog.Printf("Setting prd to %v", spl[len(spl)-1].crDod)
	} else {
		tLog.Printf("spl length is 0")
	}

	// If the ping RTT is more than the cutoff, treat as a dodgy ping and append
	// to spl
	if rtt > cutoff && cutoff > 0 {
		tLog.Printf("Dodgy latency of %v", rtt)
		dPing := dLatPing{crDod: true, prDod: prd, latency: rtt, pTime: t}
		tLog.Printf("Creating dPing of %v", dPing)
		spl = append(spl, dPing)
		tLog.Printf("Total spl is %v", spl)
	} else {
		// Because this is a 'normal' ping RTT, append it to queue to keep a running
		// average
		latSlice.add(float64(rtt.Nanoseconds()))
		tLog.Printf("RTT of %v is normal. Adding it to latency slice to keep average",
			float64(rtt.Nanoseconds()))

		// If the latency is OK, check that of previous. If that one is dodgy,
		// keep logging until two consecutive normal pings
		if prd {
			tLog.Printf("Previous ping was dodgy and had an RTT of %v",
				spl[len(spl)-1].latency)
			dPing := dLatPing{crDod: false, prDod: prd, latency: rtt, pTime: t}
			tLog.Printf("Because this Ping had a normal RTT, dPing is set to %v", dPing)
			spl = append(spl, dPing)
			tLog.Printf("Appending to spl. Current spl = %v", spl)
		} else {
			// If two decent latency pings in a row, then log total and reset spl
			if len(spl) > 2 {
				tLog.Printf("Previous Ping and this Ping both have normal latencies: %v and %v",
					spl[len(spl)-1].latency, rtt)
				tLog.Printf("Calculating bad run and resetting spl")
				startTime := spl[0].pTime
				tLog.Printf("Start of dodgy latency run: %v", startTime)
				endTime := spl[len(spl)-1].pTime
				tLog.Printf("End of dodgy latency run: %v", endTime)
				oLog.Printf("Period of flakey latency finished. Duration = %v",
					endTime.Sub(startTime))
				spl = nil
				tLog.Printf("Resetting spl: %v", spl)
			} else {
				tLog.Printf("Length of spl is less than 2: %v", len(spl))
			}
		}
	}
}

type queue []float64 // Queue of RTTs for normal pings to calculate what's normal

// Method to add a ping RTT to the queue, keeping the queue size to a max of 10
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

// Method to return the arithmetic mean of the RTTs in the queue
func (q *queue) mean() (m float64) {
	var total float64
	iq := []float64(*q)
	for i := range iq {
		total += iq[i]
	}
	m = total / float64(len(iq))
	return m
}
