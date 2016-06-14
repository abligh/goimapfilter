# goimapfilter [![Build Status](https://travis-ci.org/abligh/goimapfilter.svg?branch=master)](https://travis-ci.org/abligh/goimapfilter) [![GoDoc](http://godoc.org/github.com/abligh/goimapfilter?status.png)](http://godoc.org/github.com/abligh/goimapfilter/nbd) [![GitHub release](https://img.shields.io/github/release/abligh/goimapfilter.svg)](https://github.com/abligh/goimapfilter/releases)

goimapfilter is designed to work around a ridiculous limitation
in Apple Mail. Apple Mail does not support subscriptions for
mailboxes, so if you sync Apple Mail it either syncs every folder
or no folders. For some of us with large mailing list archives
and SSD disks we'd rather not fill with them, or syncing over
slow connections, this is not desirable behaviour.

So goimapfilter sets up a proxy imap server (unencrypted,
but the connection to the real server from the proxy can be
encrypted), and allows various mailboxes to be filtered out,
by a sequence of go regexps specified on the command line.
It runs as a daemon which should be launched
on logon (preferably before Apple Mail).

This sofware was inspired by by imapfilter.pl written by
Lars Eggert <lars.eggert@gmail.com>, then almost entirely
rewritten bar the daemonize subroutine (imapmboxfiler.pl)
then rewritten again in go.

In order to perform the filtering, goimapfilter removes
the IMAP capability for compression. This is because this
breaks the easy to understand line discipline. You should
also ensure that TLS/SSL is turned OFF in your mail client
(recent OS-X mail likes autodetecting it can turn it on).
Turning it on is not a good idea as this runs a TLS session
inside another TLS session (I'm assuming here you are connecting
to your IMAP server using the -ssl option).

    Usage: goimapfilter [options] OMITREGEXP ...

    -debug               Enable Debugging
    -facility NAME       Use specified syslog facility ("" for none) (default "user1")
    -foreground          Run in foreground (not as daemon)
    -local ADDRESS       Listen on addr:port locally (default "127.0.0.1:2143")
    -omit OMITREGEXP     Regexp to omit (may be used multiple times)
    -pidfile FILENAME    Path to PID file (default "/var/folders/.../T/goimapfilter.pid")
    -remote ADDRESS      Connect to remote addr:port (default "mail.example.com:143")
    -signal ACTION       Send signal to daemon (currently only "stop" or "restart")
    -ssl                 Use SSL for remote server
    -timeout duration    Default timeout (default 2m0s)

OMITREGEXP is a go regexp matching a mailbox to omit.
Put as many as you like on the command line, but remember
quoting.

Example:

    goimapfilter -remote mail.example.com:993 -ssl 'INBOX\.[omit|archive]\..*'

will omit all folders beneath 'omit' and 'archive'.

This software is beta quality. If it breaks you get to keep both
halves.
