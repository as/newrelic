# newrelic
A very simple newrelic command line log plumber

# install

```
go install github.com/as/newrelic/cmd/logpipe
export NR_KEY=""
```

```
echo hello world | logpipe
echo '{ "ts": 16800000000, "level": "error", "msg": "xxx"}' | logpipe
cat /dev/pipe | logpipe
./app | logpipe
```

# manual

```
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
	newrelic timestamp.

	Logpipe will automatically batch log lines. See FLAGS

	Set at least NR_KEY to your newrelic license key and run
	the examples as above. If you are in a different region, set
	$NR_URL too.

FLAGS
  -debug
    	debug output to stderr
  -f duration
    	flush logs after this duration (default 5s)
  -t duration
    	http timeout (default 5s)
```
