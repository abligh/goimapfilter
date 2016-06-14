package main

import (
	"flag"
	"github.com/sevlyar/go-daemon"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type omitEntries []string

const (
	ENV_PIDFILE = "_GOIMAPFILTER_PIDFILE"
	PROGNAME    = "goimapfilter"
)

var (
	ssl        = flag.Bool("ssl", false, "Use SSL for remote server")
	local      = flag.String("local", "127.0.0.1:2143", "Listen on addr:port locally")
	remote     = flag.String("remote", "mail.example.com:143", "Connect to remote addr:port")
	timeout    = flag.Duration("timeout", 120*time.Second, "Default timeout")
	facility   = flag.String("facility", "user1", "Use specified syslog facility (\"\" for none)")
	pidFile    = flag.String("pidfile", filepath.Join(os.Getenv("TMPDIR"), "/"+PROGNAME+".pid"), "Path to PID file")
	sendSignal = flag.String("signal", "", "Send signal to daemon (currently only \"stop\" or \"restart\")")
	foreground = flag.Bool("foreground", false, "Run in foreground (not as daemon)")
	debug      = flag.Bool("debug", false, "Debugging")
	omits      omitEntries
)

func (oe *omitEntries) String() string {
	return strings.Join(*oe, " ")
}

func (oe *omitEntries) Set(value string) error {
	*oe = append(*oe, value)
	return nil
}

var nextid int = 0

var openConnections int64

var logger *log.Logger

func ReportConnections() {
	for {
		select {
		case <-time.After(10 * time.Second):
			logger.Printf("[INFO] [M] Open connections: %d", atomic.LoadInt64(&openConnections))
		}
	}
}

func Listener(listen net.Listener) {
	for {
		id := nextid
		nextid++
		lconn, err := listen.Accept()
		if err != nil {
			logger.Printf("[INFO] [C:%08x] Could not accept new connection: %s", id, err)
		} else {
			ic := &ImapConnection{localConn: lconn, id: id}
			ic.res = make([]*regexp.Regexp, len(omits))
			for i, v := range omits {
				if r, err := regexp.Compile(`(?m)^\* (LIST|LSUB) (\([^)]*\))? "[^"]" "?` + v + `[^\r\n]*"?\r\n`); err != nil {
					logger.Fatalf("Cannot compile regexp '%s': %s", v, err)
				} else {
					ic.res[i] = r
				}
			}
			ic.capRe = regexp.MustCompile(`(?m)^([*0-9.]+ OK \[CAPABILITY IMAP4[^\r\n]+) COMPRESS=DEFLATE([^\r\n]*\r\n)`)
			atomic.AddInt64(&openConnections, 1)
			go ic.Proxy()
		}
	}
}

func Run() {
	intr := make(chan os.Signal, 1)
	term := make(chan os.Signal, 1)
	hup := make(chan os.Signal, 1)
	defer close(intr)
	defer close(term)
	defer close(hup)
	signal.Notify(intr, os.Interrupt)
	signal.Notify(term, syscall.SIGTERM)
	signal.Notify(hup, syscall.SIGHUP)

	logger.Printf("[INFO] Starting up")

	listen, err := net.Listen("tcp", *local)
	if err != nil {
		logger.Fatalf("[CRIT] Could not listen on %s: %s", *local, err)
	}

	go ReportConnections()
	go Listener(listen)
	select {
	case <-term:
		return
	case <-intr:
		return
	case <-hup:
	}
}

func main() {
	flag.Var(&omits, "omit", "Regexp to omit (may be used multiple times)")
	flag.Parse()
	if len(omits) == 0 {
		omits = []string{`INBOX\.archive.*`, `archive`}
	}

	// Just for this routine
	logger = GetLogger()

	if *sendSignal == "" {
		*sendSignal = "restart"
	}

	daemon.AddFlag(daemon.StringFlag(sendSignal, "stop"), syscall.SIGTERM)
	daemon.AddFlag(daemon.StringFlag(sendSignal, "restart"), syscall.SIGTERM)
	//daemon.AddFlag(daemon.StringFlag(sendSignal, "reload"), syscall.SIGHUP)

	if daemon.WasReborn() {
		if val := os.Getenv(ENV_PIDFILE); val != "" {
			*pidFile = val
		}
	}

	var err error
	if *pidFile, err = filepath.Abs(*pidFile); err != nil {
		logger.Fatalf("[CRIT] Error canonicalising pid file path: %v", err)
	}

	if *foreground {
		Run()
		return
	}

	os.Setenv(ENV_PIDFILE, *pidFile)

	// Define daemon context
	d := &daemon.Context{
		PidFileName: *pidFile,
		PidFilePerm: 0644,
		Umask:       027,
	}

	// Send commands if needed
	if len(daemon.ActiveFlags()) > 0 {
		start := (*sendSignal == "restart")
		p, err := d.Search()
		if err != nil {
			if start {
				goto skip
			}
			logger.Fatalf("[CRIT] Unable send signal to the daemon - not running")
		} else {
			if err := p.Signal(syscall.Signal(0)); err != nil {
				if start {
					goto skip
				}
				logger.Fatalf("[CRIT] Unable send signal to the daemon - not running, perhaps PID file is stale")
			} else {
				daemon.SendCommands(p)
			}
		}
		if !start {
			return
		}
		killed := false
		for i := 0; i < 20; i++ {
			if err := p.Signal(syscall.Signal(0)); err != nil {
				killed = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !killed {
			logger.Printf("[WARN] Unable to politely kill daemon - escalating violence")
			if err := p.Signal(syscall.Signal(syscall.SIGKILL)); err != nil {
				logger.Fatalf("[CRIT] Unable send kill signal to the daemon")
			}
			for i := 0; i < 20; i++ {
				if err := p.Signal(syscall.Signal(0)); err != nil {
					killed = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !killed {
				logger.Printf("[WARN] Unable to forcibly kill daemon - new start may fail")
			}
		}
	}

	time.Sleep(10 * time.Millisecond)

skip:
	if !daemon.WasReborn() {
		if p, err := d.Search(); err == nil {
			if err := p.Signal(syscall.Signal(0)); err == nil {
				logger.Fatalf("[CRIT] Daemon is already running (pid %d)", p.Pid)
			} else {
				logger.Printf("[INFO] Removing stale PID file %s", *pidFile)
				os.Remove(*pidFile)
			}
		}
	}

	// Process daemon operations - send signal if present flag or daemonize
	child, err := d.Reborn()
	if err != nil {
		logger.Fatalf("[CRIT] Daemonize: %s", err)
	}
	if child != nil {
		return
	}

	defer func() {
		d.Release()
		// for some reason this is not removing the pid file
		os.Remove(*pidFile)
		logger.Printf("[INFO] Quitting cleanly")
	}()

	Run()
}
