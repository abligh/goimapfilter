package main

import (
	"log"
	"log/syslog"
	"os"
	"regexp"
)

// SyslogWriter is a WriterCloser that logs to syslog with an extracted priority
type SyslogWriter struct {
	facility syslog.Priority
	w        *syslog.Writer
}

// facilityMap maps textual
var facilityMap map[string]syslog.Priority = map[string]syslog.Priority{
	"kern":     syslog.LOG_KERN,
	"user":     syslog.LOG_USER,
	"mail":     syslog.LOG_MAIL,
	"daemon":   syslog.LOG_DAEMON,
	"auth":     syslog.LOG_AUTH,
	"syslog":   syslog.LOG_SYSLOG,
	"lpr":      syslog.LOG_LPR,
	"news":     syslog.LOG_NEWS,
	"uucp":     syslog.LOG_UUCP,
	"cron":     syslog.LOG_CRON,
	"authpriv": syslog.LOG_AUTHPRIV,
	"ftp":      syslog.LOG_FTP,
	"local0":   syslog.LOG_LOCAL0,
	"local1":   syslog.LOG_LOCAL1,
	"local2":   syslog.LOG_LOCAL2,
	"local3":   syslog.LOG_LOCAL3,
	"local4":   syslog.LOG_LOCAL4,
	"local5":   syslog.LOG_LOCAL5,
	"local6":   syslog.LOG_LOCAL6,
	"local7":   syslog.LOG_LOCAL7,
}

// levelMap maps textual levels to syslog levels
var levelMap map[string]syslog.Priority = map[string]syslog.Priority{
	"EMERG":   syslog.LOG_EMERG,
	"ALERT":   syslog.LOG_ALERT,
	"CRIT":    syslog.LOG_CRIT,
	"ERR":     syslog.LOG_ERR,
	"ERROR":   syslog.LOG_ERR,
	"WARN":    syslog.LOG_WARNING,
	"WARNING": syslog.LOG_WARNING,
	"NOTICE":  syslog.LOG_NOTICE,
	"INFO":    syslog.LOG_INFO,
	"DEBUG":   syslog.LOG_DEBUG,
}

// Create a new syslog writer
func NewSyslogWriter(facility string) (*SyslogWriter, error) {
	f := syslog.LOG_DAEMON
	if ff, ok := facilityMap[facility]; ok {
		f = ff
	}

	if w, err := syslog.New(f|syslog.LOG_INFO, PROGNAME+":"); err != nil {
		return nil, err
	} else {
		return &SyslogWriter{
			w: w,
		}, nil
	}
}

// Close the channel
func (s *SyslogWriter) Close() error {
	return s.w.Close()
}

var deletePrefix *regexp.Regexp = regexp.MustCompile(PROGNAME + ":")
var replaceLevel *regexp.Regexp = regexp.MustCompile("\\[[A-Z]+\\] ")

// Write to the syslog, removing the prefix and setting the appropriate level
func (s *SyslogWriter) Write(p []byte) (n int, err error) {
	p1 := deletePrefix.ReplaceAllString(string(p), "")
	level := ""
	tolog := string(replaceLevel.ReplaceAllStringFunc(p1, func(l string) string {
		level = l
		return ""
	}))
	switch level {
	case "[DEBUG] ":
		s.w.Debug(tolog)
	case "[INFO] ":
		s.w.Info(tolog)
	case "[NOTICE] ":
		s.w.Notice(tolog)
	case "[WARNING] ", "[WARN] ":
		s.w.Warning(tolog)
	case "[ERROR] ", "[ERR] ":
		s.w.Err(tolog)
	case "[CRIT] ":
		s.w.Crit(tolog)
	case "[ALERT] ":
		s.w.Alert(tolog)
	case "[EMERG] ":
		s.w.Emerg(tolog)
	default:
		s.w.Notice(tolog)
	}
	return len(p), nil
}

func GetLogger() *log.Logger {
	if /*!*foreground &&*/ *facility != "" {
		if s, err := NewSyslogWriter(*facility); err == nil {
			return log.New(s, PROGNAME+":", 0)
		} else {
			log.Printf("[WARN]: cannot create logger %s", err)
		}
	}
	return log.New(os.Stderr, PROGNAME+":", log.LstdFlags)
}
