// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"

	"github.com/mknyszek/goswarm/gomote"
	"golang.org/x/sync/errgroup"
)

var (
	instances uint
	clean     cleanMode = cleanOff
	verbosity uint
	deflakes  uint
	env       stringSetVar
	errMatch  string
	keepGoing bool
)

func init() {
	flag.UintVar(&instances, "i", 10, "number of instances to run in parallel")
	flag.Var(&env, "e", "an environment variable to use on the gomote of the form VAR=value, may be specified multiple times")
	flag.StringVar(&errMatch, "match", "", "stop only if a failure's output matches this regexp")
	flag.Var(&clean, "clean", "off=do not clean up instances, start=clean up existing gomotes of the provided instance type at startup, exit=clean up instances created by goswarm on exit")
	flag.UintVar(&verbosity, "v", 2, "verbosity level: 0 is quiet, 2 is the maximum")
	flag.UintVar(&deflakes, "deflake", 5, "number of times to retry basic gomote operations")
	flag.BoolVar(&keepGoing, "keep-going", false, "keep testing on remaining instances after finding a matching failure")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "goswarm creates a pool of gomotes and executes a command on them until one of them fails.\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Note that goswarm does not tear down gomotes.\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [instance type] [command]\n", os.Args[0])
		flag.PrintDefaults()
	}
}

type stringSetVar []string

func (s *stringSetVar) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSetVar) Set(c string) error {
	*s = append(*s, c)
	return nil
}

type cleanMode string

const (
	cleanOff   cleanMode = "off" // do not clean up.
	cleanStart cleanMode = "start" // clean up old instances before starting.
	cleanExit  cleanMode = "exit" // clean up instances created by goswarm on exit.
)

func (c *cleanMode) String() string {
	if c == nil {
		return ""
	}
	return string(*c)
}

func (c *cleanMode) Set(s string) error {
	switch cleanMode(s) {
	case cleanOff:
		*c = cleanOff
	case cleanStart:
		*c = cleanStart
	case cleanExit:
		*c = cleanExit
	default:
		return fmt.Errorf("unknown clean mode %q", s)
	}
	return nil
}

func main() {
	flag.Parse()
	if err := run(); err != nil && err != errStop {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func validateInstanceType(ctx context.Context, typ string) error {
	typs, err := gomote.InstanceTypes(ctx)
	if err != nil {
		return err
	}
	for _, it := range typs {
		if typ == it {
			return nil
		}
	}
	return fmt.Errorf("invalid instance type: %s", typ)
}

func cleanUpInstances(ctx context.Context, typ string) error {
	insts, err := gomote.List(ctx)
	if err != nil {
		return err
	}
	for _, inst := range insts {
		if inst.Type != typ {
			continue
		}
		log.Printf("Destroying instance %s...", inst.Name)
		if err := gomote.Destroy(ctx, inst.Name); err != nil {
			return err
		}
	}
	return nil
}

var errStop = errors.New("stop execution due to matching failure")

func run() error {
	// No arguments is always wrong.
	if flag.NArg() == 0 {
		return fmt.Errorf("expected an instance type, followed by a command")
	}
	if verbosity == 0 {
		// Quiet mode.
		log.SetOutput(io.Discard)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// We have at least an instance type, so validate that
	// and clean up instances if asked.
	typ := flag.Arg(0)
	if err := validateInstanceType(ctx, typ); err != nil {
		return err
	}
	if clean == cleanStart {
		if err := cleanUpInstances(ctx, typ); err != nil {
			return fmt.Errorf("cleaning up instances: %v", err)
		}
	}
	if flag.NArg() == 1 {
		// No command, so nothing more to do.
		// Surface an error if -clean was not passed.
		if clean != cleanStart {
			return fmt.Errorf("expected a command")
		}
		return nil
	}

	var errRegexp *regexp.Regexp
	if errMatch != "" {
		r, err := regexp.Compile(errMatch)
		if err != nil {
			return fmt.Errorf("compiling regexp: %v", err)
		}
		errRegexp = r
	}

	cmd := flag.Args()[1:]
	eg, ctx := errgroup.WithContext(ctx)
	for i := 0; i < int(instances); i++ {
		eg.Go(func() error {
			// Create instance.
			var inst string
			err := retry(func() error {
				i, err := gomote.Create(ctx, typ)
				inst = i
				return err
			}, deflakes)
			if err != nil {
				log.Printf("Aborting instance creation due to too many errors: %v", unwrap(err))
				return nil
			}
			log.Printf("Created instance %s...", inst)

			if clean == cleanExit {
				defer func() {
					log.Printf("Destroying instance %s...", inst)
					if err := gomote.Destroy(context.Background(), inst); err != nil {
						log.Printf("Error destroying instance %s: %v", inst, err)
					}
				}()
			}

			// Push GOROOT to instance.
			// N.B. GOROOT is implicitly passed to gomote via the environment.
			err = retry(func() error { return gomote.Push(ctx, inst) }, deflakes)
			if err != nil {
				log.Printf("Giving up on %s due to too many errors while pushing: %v", inst, unwrap(err))
				return nil
			}
			log.Printf("Pushed to %s.", inst)

			// Run command in a loop.
			for {
				log.Printf("Running command on %s.", inst)
				results, err := gomote.Run(ctx, inst, env, cmd...)
				select {
				case <-ctx.Done():
					// Context canceled. Return nil.
					return nil
				default:
				}
				if err != nil {
					_, ok := err.(*exec.ExitError)
					if !ok {
						// Failed in some other way.
						return err
					}
					if bytes.Contains(results, []byte(inst)) {
						return fmt.Errorf("lost builder %q", inst)
					}
					if errRegexp != nil && !errRegexp.Match(results) {
						// Only consider failures that match the regexp
						// "real" failures. But if our verbosity level
						// is high enough, dump the failure anyway.
						f, err := os.CreateTemp("", inst)
						if err != nil {
							log.Printf("Failed to write output from %s to temp file: %v", inst, err)
						}
						if _, err := f.Write(results); err != nil {
							log.Printf("Failed to write output from %s to %s: %v", inst, f.Name(), err)
							f.Close()
						}
						f.Close()
						if verbosity < 2 {
							log.Printf("Unmatched failure on %s.", inst)
						} else {
							log.Printf("Unmatched failure on %s:\n%s", inst, string(results))
						}
						log.Printf("Wrote output of %s to %s.", inst, f.Name())
						continue
					}
					log.Printf("Discovered failure on %s.", inst)
					outName := inst + ".out"
					if err := os.WriteFile(outName, results, 0o644); err != nil {
						log.Printf("Dumping output from %s:\n%s", inst, string(results))
						return fmt.Errorf("failed to write output: %v\n", err)
					}
					log.Printf("Wrote output of %s to %s.", inst, outName)
					tarName := inst + ".tar.gz"
					f, err := os.Create(tarName)
					if err != nil {
						return fmt.Errorf("failed to create archive for %s: %v", inst, err)
					}
					defer f.Close()
					if err := gomote.Get(ctx, inst, f); err != nil {
						return fmt.Errorf("failed to download archive for %s: %v", inst, err)
					}
					log.Printf("Downloaded archive of %s to %s.", inst, tarName)
					if keepGoing {
						return nil
					}
					return errStop
				}
			}
		})
	}
	return eg.Wait()
}

func retry(f func() error, retries uint) error {
	i := 0
loop:
	err := f()
	if err == nil {
		return nil
	}
	i++
	if i < int(retries) {
		goto loop
	}
	return err
}

func unwrap(err error) error {
	r, ok := err.(*exec.ExitError)
	if !ok {
		return err
	}
	if len(r.Stderr) == 0 {
		return fmt.Errorf("%v: <no output>", err)
	}
	return fmt.Errorf("%v: <stderr>: %s", err, string(r.Stderr))
}
