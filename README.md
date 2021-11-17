# goswarm

goswarm is a tool that creates many gomotes and executes the same command on all
of them, in a loop, until any one of them fails.

## Installation

[Install Go.](https://golang.org/doc/install)

Then, install `gomote`:

```
go install golang.org/x/build/cmd/gomote@latest
```

Make sure that `gomote` is now in your path.

Finally, install this tool:

```
go install github.com/mknyszek/goswarm@latest
```

## Usage

**WARNING:** This tool can spin up an arbitrary number of `gomote`
instances so please take great care to ensure the `gomote` instance
type you're spinning up does *not* have limited capacity, or that
you're cleared to do this by the Go release team.

TODO: Make `goswarm` check for and reject limited-capacity instance types
unless the user specifically acknowledges the risks.

The typical use-case is trying to reproduce a rarely-occuring bug, usually with
the goal of capturing a core dump or attaching GDB to the process.
This tool only focuses on the first half of the equation.

To execute `all.bash` on 10 (the default) NetBSD 9.0 gomotes at once, do

```
GOROOT=path/to/go/repo goswarm netbsd-386-9_0 go/src/all.bash
```

It's highly recommended to also pass a `-match` argument that executes until
a failure whose output matches the provided regular expression is encountered.
Even just `-match="fatal error:"` is quite effective.
Without it, `goswarm` will stop even if `gomote` fails due to some unrelated
error.

If `-match` is specified, unmatched failures will always be written to a
temporary file in the default temporary directory for your platform.
By default they will also be logged, but this can be disabled by setting
`-v` to a value less than 2.

### Core dumps

To capture core dump, add the following file to your Go repository (it does not
need to be in git) as `debug.bash` (with the appropriate file permissions).

```bash
#!/usr/bin/env bash

set -e

ulimit -c unlimited

# Linux
# echo "/workdir/go/core.%e.%p" | sudo tee /proc/sys/kernel/core_pattern

# NetBSD
sysctl -w proc.$$.corename=$(dirname $0)/%n.%p.core

export GOTRACEBACK=crash
$(dirname $0)/all.bash
```

and invoke `goswarm` like so:

```
GOROOT=path/to/go/repo goswarm netbsd-386-9_0 go/src/debug.bash
```

`goswarm` will automatically copy down the full working directory on the gomote
back as a gzipped tar (as per `gomote gettar`).

### Clean up

`goswarm` purposefully *does not* clean up instances, so that the failing
instance may be examined in more detail.
The core dump likely must be manually extracted at this point by finding its
location in the filesystem (depends on which process crashed) and using the
`gomote` command to copy it back.

To clean up instances you created of a particular type, use the `-clean` flag.

```
goswarm -clean netbsd-386-9_0
```
