// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gomote

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func Create(ctx context.Context, typ string) (string, error) {
	result, err := exec.CommandContext(ctx, "gomote", "create", typ).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result)), nil
}

func Push(ctx context.Context, inst string) error {
	err := exec.CommandContext(ctx, "gomote", "push", inst).Run()
	if err != nil {
		return err
	}
	return nil
}

type Instance struct {
	Name, Type string
}

func List(ctx context.Context) ([]Instance, error) {
	result, err := exec.CommandContext(ctx, "gomote", "list").CombinedOutput()
	if err != nil {
		return nil, err
	}
	rd := bytes.NewReader(result)
	sc := bufio.NewScanner(rd)
	var insts []Instance
	for sc.Scan() {
		line := sc.Text()
		details := strings.Split(line, "\t")
		if len(details) < 2 {
			return nil, fmt.Errorf("unexpected `gomote list` format: %q", line)
		}
		name := strings.TrimSpace(details[0])
		typ := strings.TrimSpace(details[1])
		insts = append(insts, Instance{name, typ})
	}
	if sc.Err() != nil {
		return nil, err
	}
	return insts, nil
}

func Destroy(ctx context.Context, inst string) error {
	err := exec.CommandContext(ctx, "gomote", "destroy", inst).Run()
	if err != nil {
		return err
	}
	return nil
}

func Run(ctx context.Context, inst string, env []string, cmd ...string) ([]byte, error) {
	args := []string{"run"}
	for _, v := range env {
		args = append(args, "-e", v)
	}
	args = append(args, inst)
	args = append(args, cmd...)
	return exec.CommandContext(ctx, "gomote", args...).CombinedOutput()
}

func InstanceTypes(ctx context.Context) ([]string, error) {
	result, err := exec.CommandContext(ctx, "gomote", "create").CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.Error); ok {
			return nil, err
		}
		// Ignore error otherwise because gomote exits with a non-zero exit code.
	}
	rd := bytes.NewReader(result)
	sc := bufio.NewScanner(rd)
	start := false
	var typs []string
	for sc.Scan() {
		line := sc.Text()
		if !start {
			if strings.HasPrefix(line, "Valid types:") {
				start = true
			}
			continue
		}
		cutl := strings.IndexRune(line, '*')
		if cutl < 0 || cutl == len(line)-1 {
			return nil, fmt.Errorf("unexpected `gomote create` format: %q", line)
		}
		cutr := strings.IndexRune(line, '[')
		if cutr < 0 {
			cutr = len(line)
		}
		typs = append(typs, strings.TrimSpace(line[cutl+1:cutr]))
	}
	if sc.Err() != nil {
		return nil, err
	}
	return typs, nil
}
