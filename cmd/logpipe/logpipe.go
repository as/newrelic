package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
)

const man = `
NAME
	logpipe - pipe logs to newrelic

SYNOPSIS
	export NR_KEY=""
	export NR_URL="" # optional
	echo hi newrelic | logpipe 
	app 2>&1 | logpipe [-f flushdur] [-t httptimeout] [-debug]

DESCRIPTION
	Logpipe sends every line read from its standard input to
	newrelic as a log line. If the log line is valid json, and contains
	an integer "ts" fields at its top level, that value is used as the
	newrelic timestamp. By default, each line read is re-emitted
	to standard output (see -q).

	Logpipe will automatically batch log lines. See FLAGS

	Set at least NR_KEY to your newrelic license key and run
	the examples as above. If you are in a different region, set
	$NR_URL too.

BUGS
	(1) Process signals are currently not intercepted
	(2) If push fails, the buffered log lines are lost

FLAGS`

var (
	deadband = flag.Duration("f", 5*time.Second, "flush logs after this duration")
	timeout  = flag.Duration("t", 5*time.Second, "http timeout")
	debug    = flag.Bool("debug", false, "debug output to stderr")
	quiet    = flag.Bool("q", false, "dont emit each log line read back to stdout (default behavior)")

	key = os.Getenv("NR_KEY")
	uri = os.Getenv("NR_URL")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), man)
		flag.PrintDefaults()
	}
}

// newrelic says their max plaintext limit is 1MiB, i dont trust them
const hiwater = 1024 * 1023

// TODO(as): Intercept process SIGINT and SIGKILL
func main() {
	flag.Parse()
	if key == "" {
		fmt.Fprintln(os.Stderr, "logpipe: provide license via $NR_KEY\nexport NR_KEY=")
		os.Exit(1)
	}
	if uri == "" {
		uri = "https://log-api.newrelic.com/log/v1"
	}

	linec := make(chan Log, 256)
	done := make(chan bool)
	ticker := time.NewTicker(*deadband)
	go func() {
		// collect the lines into boxes and periodically flush them to nr
		box := Box{
			Log: []Log{},
		}
		flush := func() {
			push(box)
			// NOTE(as) theres a bug here where if the upstream
			// fails, the logs in box above will be lost
			// i dont expect this to be relevant to my current
			// use case for this program.
			//
			// If you want to fix this, check the return value of
			// push above and then split the boxes at the hiwater mark
			// instead of creating a fresh one below.
			//
			// Alternatively, you can just use push in a loop until
			// it returns true. The scanner should continue collecting
			// lines concurrently.
			box = Box{}
		}
		defer close(done)
		for {
			select {
			case t := <-ticker.C: // prevent stale logs
				dbg("tick: %s", t)
				flush()
			case l, more := <-linec: // collect
				if !more {
					dbg("linec: closed")
					flush()
					return
				}
				if n, m := l.Len(), box.Len(); n+m > hiwater {
					dbg("forcing flush: old=%d new=%d", n, m)
					flush()
				}
				box.Log = append(box.Log, l)
			}
		}
	}()

	// scan lines from stdin
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		ts := int64(0)
		json.Unmarshal(sc.Bytes(), &struct{ TS *int64 }{&ts})
		if ts == 0 {
			ts = time.Now().Unix()
		}
		if !*quiet {
			fmt.Println(sc.Text())
		}
		linec <- Log{T: ts, M: sc.Text()}
	}

	// These channels are not redundant:
	//
	// first, the scanner finishes
	// second, we wait for the USPS goroutine above to finish shipping the existing logs
	// finally, and only then, we can exit the process without losing tail logs
	//
	// If you modify this program to use push in a loop, you will need an additional channel
	// so this process can complete with a proper timeout
	dbg("scanner: done")
	close(linec)
	dbg("linec closed")
	<-done
	dbg("exits")
}

// pushbox is the http meat of this operation
func push(box Box) bool {
	if len(box.Log) == 0 {
		dbg("push: nothing to flush")
		return true
	}
	dbg("log: %s", "["+js(box)+"]")
	req, err := http.NewRequest("POST", uri, strings.NewReader("["+js(box)+"]"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "logpipe: bad newrelic endpoint")
		os.Exit(1)
	}
	req.Header.Add("Api-Key", key)
	req.Header.Add("Content-Type", "application/json")
	ctx, fn := context.WithTimeout(context.Background(), *timeout)
	defer fn()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return false
	}

	// subtle: if you dont read the response body in full and also close it
	// the connection will not be reused. Go attempt to detect this misuse
	// but only for Close()
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		fmt.Fprintf(os.Stderr, "logpipe: bad license key: %s", resp.Status)
		os.Exit(1)
	}
	if resp.StatusCode/100 > 3 {
		return false
	}
	return true
}

// Box is what is wrapped in brackets and sent to nr
type Box struct {
	Log []Log `json:"logs"`
}

type Log struct {
	M string `json:"message"`
	T int64  `json:"timestamp"`
}

// for sizes, just overestimate, it doesn't matter

func (l Log) Len() int {
	const hdr = `{"message":"","timestamp":1684206341000000000}`
	return len(hdr) + len(l.M)*2 // assume the message is escaped
}

func (b Box) Len() (n int) {
	for _, v := range b.Log {
		n += v.Len()
	}
	return n + 32
}

func js(v any) string {
	d, _ := json.Marshal(v)
	return string(d)
}

func dbg(f string, v ...any) {
	if *debug {
		fmt.Fprintf(os.Stderr, f, v...)
		fmt.Fprintln(os.Stderr)
	}
}
