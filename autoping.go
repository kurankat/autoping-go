// Autoping is a small application to automatically ping a server every minute
// and log outages, keeping track of the duration of each outage

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/pprof"
	"strconv"
	"syscall"
	"time"

	cron "github.com/robfig/cron"
	ping "github.com/sparrc/go-ping"
)

// Set up flags, loggers and global variables
var importFlag = flag.String("i", "", "IP address or hostname to be pinged")
var traceFlag = flag.Bool("t", false, "turn on trace to file")
var pLog, eLog, oLog, tLog *log.Logger
var ipAddr string // User supplied IP address to ping to

var thisOutage = outageInfo{isOutage: false}
var thisBadLatency = badLatencyPeriod{isBadLatencyPeriod: false}
var dailyOutages = outageTracker{}
var dailyBadLatencyPeriods = badLatencyTracker{}
var connTracker = connectionTracker{}
var crn = cron.New()

var badLatencyPings []badPingInfo // List of recent pings with dodgy latency
var normalLatencies queue
var meanLatency time.Duration
var lastSuccessfulPingTime time.Time

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
	logFile, err := os.OpenFile("/var/log/goping.new.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
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

	// Set up trace logging if -t flag is used
	if *traceFlag {
		tLog.SetOutput(logFile)
	}

	// Schedule a daily digest of outages and periods of bad latency
	tLog.Printf("Creating daily digest")
	crn.AddFunc("@midnight", func() { dailyDigest() })

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
	interval := time.NewTicker(1 * time.Minute)
	for _ = range interval.C {
		tLog.Printf("Running ping now")
		go runPing()
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
			if lastSuccessfulPingTime.Year() == t.Year() &&
				t.Sub(lastSuccessfulPingTime) > 2*time.Minute {
				thisOutage.isOutage = true
				thisOutage.missedPingNumber++
				thisOutage.outageDuration = time.Duration(thisOutage.missedPingNumber) *
					time.Minute
				tLog.Printf("DNS error")
				oLog.Printf("Lost contact. Outage duration %v",
					thisOutage.outageDuration)
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
			go func() {
				time.Sleep(1 * time.Second) // One second delay for log order
				pLog.Printf("%d bytes from %s: icmp_seq=%d time=%v", pkt.Nbytes, pkt.IPAddr,
					pkt.Seq, pkt.Rtt)
			}()
		}
		pinger.OnFinish = func(stats *ping.Statistics) {
			// If no packets come back after timeout, start logging outage after 2 min
			// since last successful ping (2 missed pings in a row)
			if stats.PacketsRecv == 0 {
				// The following conditions have to be met: the ping year of the last
				// successful ping has to be this year (at the start of the run lsPing is
				// set to 0) AND the time difference between the last successful ping and
				// this one has to be more than 2 minutes
				oLog.Printf("Timeout - Missed pong")
				thisOutage.missedPingNumber++
				tLog.Printf("No packet received")
				tLog.Printf("Increasing number of missed pings to %v", thisOutage.missedPingNumber)
				if lastSuccessfulPingTime.Year() == t.Year() &&
					thisOutage.missedPingNumber > 2 {
					thisOutage.isOutage = true
					tLog.Printf("Setting isOutage to %v", thisOutage.isOutage)
				}

			} else if stats.PacketsRecv > 0 {
				// If we get a packet back, reset last successful ping time to the time this
				// ping was fired, and reset outage
				tLog.Printf("Packet received")
				if thisOutage.isOutage {
					thisOutage.isOutage = false
					tLog.Printf("Setting isOutage to %v", thisOutage.isOutage)
					thisOutage.reconnectTime = t
					tLog.Printf("Setting reconnection time to %v", thisOutage.reconnectTime)
					thisOutage.outageDuration = time.Duration(thisOutage.missedPingNumber) *
						time.Minute
					oLog.Printf("Connection restored. Total outage duration %v minutes",
						thisOutage.outageDuration.Minutes())
					dailyOutages.addOutage(&thisOutage)
					tLog.Printf("Adding most current outage to daily list")
					thisOutage.missedPingNumber = 0
					tLog.Printf("Resetting number of missed pings back to %v", thisOutage.missedPingNumber)
				}
				lastSuccessfulPingTime = t
				tLog.Printf("Updating time of last successful ping to to %v", lastSuccessfulPingTime)
				tLog.Printf("Sending to evaluateLatency()")
				evaluateLatency(t, stats.MinRtt)
			}
		}
	}
	tLog.Printf("Executing ping")
	pinger.Run() // Send the ping
}

// Evaluate latency of supplied ping. If ping has a long latency, add it to the
// queue. If ping is normal (< 100 ms) then check if previous ping was also
// normal. If so, finalise badLatencyPings and log total duration of dodgy latency pings.
// If previous ping was dodgy, ignore single normal ping and keep logging
func evaluateLatency(t time.Time, rtt time.Duration) {
	tLog.Printf("Evaluating Pong sent at %v with RTT of %v", t, rtt)
	meanLatency = time.Duration(normalLatencies.mean()) * time.Nanosecond
	tLog.Printf("meanLatency is currently %v", meanLatency)
	cutoff := meanLatency * 3

	// If the ping RTT is more than the cutoff, treat as a dodgy ping and append
	// to badLatencyPings
	if rtt > cutoff && cutoff > 0 {
		tLog.Printf("Dodgy latency of %v", rtt)
		badPing := badPingInfo{thisLatencyBad: true, latency: rtt, timeFired: t}
		tLog.Printf("Creating badPing of %v", badPing)
		connTracker.addPing(badPing)
		tLog.Printf("Adding bad ping to connTracker")
		thisBadLatency.badLatencyNumber++
		tLog.Printf("Increasing number of bad latency RTTs to %v", thisBadLatency.badLatencyNumber)

		if thisBadLatency.badLatencyNumber > 2 {
			thisBadLatency.isBadLatencyPeriod = true
			tLog.Printf("More than two high-latency RTTs (n=%v), setting isBadLatencyPeriod to %v",
				thisBadLatency.badLatencyNumber, thisBadLatency.isBadLatencyPeriod)
		}

		tLog.Printf("Bad latency for %v pings", thisBadLatency.badLatencyNumber)
	} else {
		// Because this is a 'normal' ping RTT, append it to queue to keep a running
		// average
		normalLatencies.add(float64(rtt.Nanoseconds()))
		tLog.Printf("RTT of %v is normal. Adding it to latency slice to keep average",
			float64(rtt.Nanoseconds()))

		// If the latency is OK, check that of previous. If that one is dodgy,
		// keep logging until two consecutive normal pings
		if connTracker.getPreviousLatencyState() {
			tLog.Printf("Previous ping was dodgy and had an RTT of %v",
				connTracker.getPreviousLatencyState())
			badPing := badPingInfo{thisLatencyBad: false, latency: rtt, timeFired: t}
			tLog.Printf("Because this Ping had a normal RTT, badPing is set to %v", badPing)
			connTracker.addPing(badPing)
		} else {
			// If two decent latency pings in a row, then log total and reset badLatencyPings
			if thisBadLatency.badLatencyNumber > 2 {
				tLog.Printf("Previous Ping and this Ping both have normal latencies: %v and %v",
					connTracker.getPreviousLatencyState(), rtt)
				tLog.Printf("Normality restored. Calculating bad run and resetting badLatencyPings")
				tLog.Printf("Start of dodgy latency run: %v", connTracker.pingBeforeLatest.timeFired)
				thisBadLatency.resumeNormalTime = connTracker.pingBeforeLatest.timeFired
				tLog.Printf("End of dodgy latency run: %v", thisBadLatency.resumeNormalTime)
				thisBadLatency.badLatencyPeriodDuration = time.Duration(thisBadLatency.badLatencyNumber) * time.Minute
				oLog.Printf("Period of flakey latency finished. Duration = %v",
					thisBadLatency.badLatencyPeriodDuration)
				dailyBadLatencyPeriods.addBadLatencyPeriod(&thisBadLatency)
				thisBadLatency.badLatencyNumber = 0
				tLog.Printf("Resetting badLatencyNumber to %v", thisBadLatency.badLatencyNumber)
				thisBadLatency.isBadLatencyPeriod = false
				tLog.Printf("Resetting isBadLatencyPeriod to %v", thisBadLatency.isBadLatencyPeriod)
			} else {
				tLog.Printf("Length of badLatencyPings is less than 2: %v", thisBadLatency.badLatencyNumber)
			}
		}
	}
}

type outageInfo struct {
	isOutage         bool
	missedPingNumber int
	reconnectTime    time.Time
	outageDuration   time.Duration
}

type outageTracker struct {
	outageList []outageInfo
}

func (ot *outageTracker) addOutage(oi *outageInfo) {
	ot.outageList = append(ot.outageList, *oi)
}

func dailyDigest() {
	var outNum, blNum int = 0, 0
	timeStamp := time.Now().Format("20060102")
	digestFile, err := os.OpenFile("/var/log/goping.digest."+timeStamp+".log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		panic("I'm having trouble writing to the digest file")
		os.Exit(1)
	}
	defer digestFile.Close()

	writer := bufio.NewWriter(digestFile)

	writer.WriteString("Outage Digest for " + timeStamp)
	writer.WriteString("Number of outages: " + string(len(dailyOutages.outageList)))

	for _, outage := range dailyOutages.outageList {
		outNum++
		outageString := "\tOutage " + string(outNum) + " ended at " +
			outage.reconnectTime.Format("T15:04:05") + " and lasted " +
			strconv.FormatFloat(outage.outageDuration.Minutes(), 'f', 2, 64) + " minutes"
		writer.WriteString(outageString)
	}

	writer.WriteString("Bad latency Digest for " + timeStamp)
	writer.WriteString("Number of periods of bad latency: " +
		string(len(dailyBadLatencyPeriods.badLatencyList)))

	for _, badLatency := range dailyBadLatencyPeriods.badLatencyList {
		blNum++
		badLatencyString := "\tOutage " + string(blNum) + " ended at " +
			badLatency.resumeNormalTime.Format("T15:04:05") + " and lasted " +
			strconv.FormatFloat(badLatency.badLatencyPeriodDuration.Minutes(), 'f', 2, 64) + " minutes"
		writer.WriteString(badLatencyString)
	}
	dailyOutages = outageTracker{}
	dailyBadLatencyPeriods = badLatencyTracker{}

}

type connectionTracker struct {
	latestPing, pingBeforeLatest badPingInfo
}

// Returns true if the previous ping latency was over the limit
func (c *connectionTracker) getPreviousLatencyState() bool {
	if c.pingBeforeLatest != (badPingInfo{}) {
		return c.pingBeforeLatest.thisLatencyBad
	} else {
		return false
	}

}

func (c *connectionTracker) addPing(p badPingInfo) {
	c.pingBeforeLatest = c.latestPing
	c.latestPing = p
}

func (c *connectionTracker) getLatency() time.Duration {
	return c.latestPing.latency
}

func (c *connectionTracker) getPreviousPing() (p badPingInfo) {
	return c.pingBeforeLatest
}

type badPingInfo struct {
	thisLatencyBad bool          // Is the latest ping latency dodgy?
	latency        time.Duration // Latency of latest ping
	timeFired      time.Time     // Time latest ping was fired
}

type badLatencyPeriod struct {
	isBadLatencyPeriod       bool
	badLatencyNumber         int
	resumeNormalTime         time.Time
	badLatencyPeriodDuration time.Duration
}

type badLatencyTracker struct {
	badLatencyList []badLatencyPeriod
}

func (blt *badLatencyTracker) addBadLatencyPeriod(bpi *badLatencyPeriod) {
	blt.badLatencyList = append(blt.badLatencyList, *bpi)
}

func (blt *badLatencyTracker) dailyDigest() {
	// Produce a daily digest log with the number and duration of outages for
	// the 24h to midnight
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
