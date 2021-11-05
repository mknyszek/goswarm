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

Before continuing, please note that *with great power comes great
responsibility*.
This tool can spin up an arbitrary number of `gomote` instances so please
take great care to ensure the `gomote` instance type you're spinning up
does *not* have limited capacity, or that you're cleared to do this.

TODO: Make `goswarm` check for and reject limited-capacity instance types
unless the user specifically acknowledges the risks.

The typical use-case is trying to reproduce a rarely-occuring bug, usually with
the goal of capturing a core dump or attaching GDB to the process.
This tool only focuses on the first half of the equation.

To execute `all.bash` on 10 FreeBSD 12.2 gomotes at once (the default), do

```
GOROOT=path/to/go/repo goswarm freebsd-amd64-12_2 go/src/all.bash
```

To capture core dump, add the following file to your Go repository (it does not
need to be in git) as `debug.bash` (with the appropriate file permissions).

```bash
#!/usr/bin/env bash

ulimit -c unlimited
echo "/tmp/core.%e.%p" | sudo tee /proc/sys/kernel/core_pattern
export GOTRACEBACK=crash
$(dirname $0)/all.bash
```

and invoke `goswarm` like so:

```
GOROOT=path/to/go/repo goswarm freebsd-amd64-12_2 go/src/debug.bash
```

`goswarm` will automatically copy down the full working directory on the gomote
back as a gzipped tar (as per `gomote gettar`).

`goswarm` purposefully *does not* clean up instances, so that the failing
instance may be examined in more detail.
The core dump likely must be manually extracted at this point by finding its
location in the filesystem (depends on which process crashed) and using the
`gomote` command to copy it back.

To clean up instances you created of a particular type, use the `-clean` flag.

```
goswarm -clean freebsd-amd64-12_2
```

Finally, it's often useful when reproducing an issue to ignore flakes, if you're
to tackle some specific issue.
For this, `goswarm` provides the `-match` flag that takes a regular expression.
Only if the output of `gomote run` matches that expression will it be considered
a failure.
