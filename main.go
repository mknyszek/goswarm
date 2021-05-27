// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/mknyszek/goswarm/gomote"
	"golang.org/x/sync/errgroup"
)

var (
	instances uint
	env       stringSetVar
)

func init() {
	flag.UintVar(&instances, "i", 10, "number of instances to run in parallel")
	flag.Var(&env, "e", "an environment variable to use on the gomote of the form VAR=value, may be specified multiple times")
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

func main() {
	flag.Parse()
	if err := run(); err != nil {
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

func run() error {
	if flag.NArg() < 2 {
		return fmt.Errorf("expected an instance type, followed by a command")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	typ := flag.Arg(0)
	if err := validateInstanceType(ctx, typ); err != nil {
		return err
	}

	cmd := flag.Args()[1:]

	eg, ctx := errgroup.WithContext(ctx)
	for i := 0; i < int(instances); i++ {
		eg.Go(func() error {
			inst, err := gomote.Create(ctx, typ)
			if err != nil {
				return err
			}
			log.Printf("Created instance %s...", inst)
			if err := gomote.Push(ctx, inst); err != nil {
				return err
			}
			log.Printf("Pushed to %s.", inst)
			for {
				log.Printf("Running command on %s.", inst)
				results, err := gomote.Run(ctx, inst, env, cmd...)
				if err != nil {
					if werr := os.WriteFile(inst+".out", results, 0o644); werr != nil {
						fmt.Fprintf(os.Stderr, "failed to write output: %v\n", werr)
						fmt.Fprintln(os.Stderr, "##### GOMOTE OUTPUT #####")
						fmt.Fprintln(os.Stderr, string(results))
						fmt.Fprintln(os.Stderr, "#########################")
					}
					return err
				}
			}
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	return nil
}
