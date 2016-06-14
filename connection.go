package main

import (
	"bytes"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

type ImapConnection struct {
	localConn  net.Conn
	remoteConn net.Conn
	id         int
	wg         sync.WaitGroup
	written    int64
	res        []*regexp.Regexp
	capRe      *regexp.Regexp
	poke       chan uint64
}

func (ic *ImapConnection) HexDump(h string, b []byte) {
	if *debug {
		logger.Printf("[DEBUG] [C:%08x] %s\n%s", ic.id, h, hex.Dump(b))
	}
}

func (ic *ImapConnection) DoWrite(b []byte) (err error) {
	if *debug {
		ic.HexDump("Sending:", b)
	}
	nw, ew := ic.localConn.Write(b)
	if nw > 0 {
		ic.written += int64(nw)
	}
	if ew != nil {
		err = ew
	}
	if nw != len(b) {
		err = io.ErrShortWrite
	}
	return
}

func (ic *ImapConnection) DoWriteReplace(b []byte) (err error) {
	// fortunately go's weird slice passing semantics are ideal here
	// we replace into (and thus corrupt) the elements of the backing
	// array, but as we never make things longer with a replace, we are
	// fine as we only overwrite stuff we are trying to write. We don't
	// corrupt the slice pointers though.
	if ic.capRe.Find(b) != nil {
		b = ic.capRe.ReplaceAll(b, []byte("$1$2"))
		if *debug {
			logger.Printf("[DEBUG] [C:%08x] Disabled COMPRESS=DEFLATE", ic.id)
		}
	}
	for _, re := range ic.res {
		// optimise for cases where regexp is not found to
		// avoid slice data copies
		if re.Find(b) != nil {
			b = re.ReplaceAll(b, []byte{})
		}
	}
	return ic.DoWrite(b)
}

// This is adapted from the source of io.Copy()
func (ic *ImapConnection) CopyProxy() (int64, error) {
	var err error
	src := ic.remoteConn
	bufSize := 64 * 1024
	buf := make([]byte, bufSize)

	rn := []byte{'\r', '\n'}

	var mta, mua uint64
	warned := false
	wpos := 0

	for {
		if *debug {
			logger.Printf("[DEBUG] [C:%08x] From MTA=%d to MUA=%d wpos %d", ic.id, mta, mua, wpos)
		}
		nr, er := src.Read(buf[wpos:])
		if nr > 0 {
			ic.HexDump("Got:", buf[wpos:wpos+nr])
			mta += uint64(nr)
			// count forward by the number of bytes we have left over
			// since the previous write
			wpos = wpos + nr
			// indicate we are alive
			ic.poke <- mua
			// look backwards through the slice for \r\n
			if idx := bytes.LastIndex(buf[0:wpos], rn); idx < 0 {
				// there is no \r\n in the data.

				if !warned && wpos > 256 {
					logger.Printf("[WARNING] [C:%08x] Data contains no line delimeters. This normally happens if compression is not disabled correctly by us, or if the MUA is attempting to start TLS within our TLS session. Try disabling TLS manually.", ic.id)
					warned = true
				}

				// if we can fit any more in, do another read
				if wpos < bufSize-len(rn) {
					if *debug {
						logger.Printf("[DEBUG] [C:%08x] No return [From MTA=%d to MUA=%d wpos=%d]", ic.id, mta, mua, wpos)
					}
					continue
				}
				if *debug {
					logger.Printf("[DEBUG] [C:%08x] No return large block [From MTA=%d to MUA=%d wpos=%d]", ic.id, mta, mua, wpos)
				}
				// We can't filter it.
				// just write it out
				err = ic.DoWrite(buf[0:wpos])
				mua += uint64(wpos)
				if err != nil {
					break
				}
				wpos = 0
			} else {
				chunkLen := idx + len(rn)
				if *debug {
					logger.Printf("[DEBUG] [C:%08x] Write up to %d [From MTA=%d to MUA=%d wpos=%d]", ic.id, chunkLen, mta, mua, wpos)
				}
				// write the buffer up to and including the index
				err = ic.DoWriteReplace(buf[0:chunkLen])
				// err = ic.DoWrite(buf[0:chunkLen])
				mua += uint64(chunkLen)
				if err != nil {
					break
				}
				if chunkLen < wpos {
					n := wpos - chunkLen
					if *debug {
						logger.Printf("[DEBUG] [C:%08x] Copy buffer (n=%d,chunkLen=%d) [From MTA=%d to MUA=%d wpos=%d]", ic.id, n, chunkLen, mta, mua, wpos)
					}
					copy(buf[0:n], buf[chunkLen:wpos])
					wpos = n
				} else {
					wpos = 0
				}
			}
		}
		if er == io.EOF {
			if ew := ic.DoWrite(buf[0:wpos]); ew != nil {
				err = ew
			} else {
				mua += uint64(wpos)
			}
			wpos = 0
			break
		}
		if er != nil {
			err = er
			if ew := ic.DoWrite(buf[0:wpos]); ew != nil {
				err = ew
			} else {
				mua += uint64(wpos)
			}
			wpos = 0
			break
		}
	}
	if *debug {
		logger.Printf("[DEBUG] [C:%08x] From MTA %d From MUA %d (FINAL)", ic.id, mta, mua)
	}
	return ic.written, err
}

func (ic *ImapConnection) Proxy() {
	defer func() {
		if ic.localConn != nil {
			ic.localConn.Close()
		}
		if ic.remoteConn != nil {
			ic.remoteConn.Close()
		}
		logger.Printf("[INFO] [C:%08x] Closed connection to %s", ic.id, *remote)
		atomic.AddInt64(&openConnections, -1)
	}()

	logger.Printf("[INFO] [C:%08x] New connection to %s", ic.id, *remote)

	// First open the remote connection
	if *ssl {
		conf := &tls.Config{}
		c, err := tls.Dial("tcp", *remote, conf)
		if err != nil {
			logger.Printf("[INFO] [C:%08x] Could not connect using TLS to %s: %s", ic.id, *remote, err)
			return
		}
		ic.remoteConn = c
	} else {
		c, err := net.Dial("tcp", *remote)
		if err != nil {
			logger.Printf("[INFO] [C:%08x] Could not connect in plain to %s: %s ", ic.id, *remote, err)
			return
		}
		ic.remoteConn = c
	}
	ic.poke = make(chan uint64, 64)
	defer close(ic.poke)
	go func() {
		last := time.Now()
		for {
			select {
			case rx, ok := <-ic.poke:
				if !ok {
					if *debug {
						logger.Printf("[DEBUG] [C:%08x] idle monitor exiting", ic.id)
					}
					return
				}
				if *debug {
					logger.Printf("[DEBUG] [C:%08x] connection is not idle", ic.id)
				}
				now := time.Now()
				if now.Sub(last) > 10*time.Second {
					last = now
					logger.Printf("[INFO] [C:%08x] Sent %d bytes", ic.id, rx)
				}
			case <-time.After(*timeout):
				logger.Printf("[INFO] [C:%08x] Closing idle connection", ic.id)
				ic.localConn.Close()
				ic.remoteConn.Close()
			}
		}
	}()
	ic.wg.Add(2)
	go func() {
		//io.Copy(ic.localConn, ic.remoteConn)
		ic.CopyProxy()
		ic.wg.Done()
	}()
	go func() {
		io.Copy(ic.remoteConn, ic.localConn)
		ic.localConn.Close() // this causes the other end to close eventually, via IDLE etc.
		ic.wg.Done()
	}()
	ic.wg.Wait()
}
