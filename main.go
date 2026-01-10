package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	snakeCaseRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	memoryRe    = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)
	imageRe     = regexp.MustCompile(`^registry\.bigbrother\.io/[^:]+:.+$`)
	pathRe      = regexp.MustCompile(`^/`)
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: yamlvalid <file>")
		os.Exit(1)
	}

	file := os.Args[1]
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing yaml: %v\n", err)
		os.Exit(1)
	}

	v := &validator{file: file}
	v.run(&root)

	if len(v.errs) > 0 {
		for _, e := range v.errs {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(1)
	}
}

type validator struct {
	file string
	errs []string
}

func (v *validator) err(line int, msg string) {
	if line > 0 {
		v.errs = append(v.errs, fmt.Sprintf("%s:%d %s", v.file, line, msg))
	} else {
		v.errs = append(v.errs, fmt.Sprintf("%s %s", v.file, msg))
	}
}

func (v *validator) m(n *yaml.Node) map[string]*yaml.Node {
	m := map[string]*yaml.Node{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		m[n.Content[i].Value] = n.Content[i+1]
	}
	return m
}

func (v *validator) run(root *yaml.Node) {
	if len(root.Content) == 0 {
		v.err(0, "empty")
		return
	}
	doc := root.Content[0]
	m := v.m(doc)

	// apiVersion
	if x, ok := m["apiVersion"]; !ok {
		v.err(0, "apiVersion is required")
	} else if x.Value != "v1" {
		v.err(x.Line, "apiVersion has unsupported value '"+x.Value+"'")
	}

	// kind
	if x, ok := m["kind"]; !ok {
		v.err(0, "kind is required")
	} else if x.Value != "Pod" {
		v.err(x.Line, "kind has unsupported value '"+x.Value+"'")
	}

	// metadata
	if x, ok := m["metadata"]; !ok {
		v.err(0, "metadata is required")
	} else {
		v.meta(x)
	}

	// spec
	specNode, hasSpec := m["spec"]
	if !hasSpec {
		v.err(0, "spec is required")
	} else {
		v.spec(specNode)
	}

	// ВАЖНО: containers может быть на верхнем уровне (неправильный YAML)
	// Валидируем их в любом случае
	if x, ok := m["containers"]; ok {
		for _, c := range x.Content {
			v.container(c)
		}
	} else if hasSpec {
		// Проверяем что containers есть внутри spec
		specMap := v.m(specNode)
		if _, ok := specMap["containers"]; !ok {
			v.err(0, "spec.containers is required")
		}
	}
}

func (v *validator) meta(n *yaml.Node) {
	m := v.m(n)
	if x, ok := m["name"]; !ok {
		v.err(0, "metadata.name is required")
	} else if strings.TrimSpace(x.Value) == "" {
		v.err(x.Line, "name is required")
	}
}

func (v *validator) spec(n *yaml.Node) {
	m := v.m(n)

	// os
	if x, ok := m["os"]; ok && x.Value != "linux" && x.Value != "windows" {
		v.err(x.Line, "os has unsupported value '"+x.Value+"'")
	}

	// containers
	if x, ok := m["containers"]; ok {
		for _, c := range x.Content {
			v.container(c)
		}
	}
}

func (v *validator) container(n *yaml.Node) {
	m := v.m(n)

	// name
	if x, ok := m["name"]; !ok {
		v.err(0, "containers.name is required")
	} else if strings.TrimSpace(x.Value) == "" {
		v.err(x.Line, "name is required")
	} else if !snakeCaseRe.MatchString(x.Value) {
		v.err(x.Line, "containers.name has invalid format '"+x.Value+"'")
	}

	// image
	if x, ok := m["image"]; !ok {
		v.err(0, "containers.image is required")
	} else if !imageRe.MatchString(x.Value) {
		v.err(x.Line, "containers.image has invalid format '"+x.Value+"'")
	}

	// ports
	if x, ok := m["ports"]; ok {
		for _, p := range x.Content {
			v.port(p)
		}
	}

	// readinessProbe
	if x, ok := m["readinessProbe"]; ok {
		v.probe(x)
	}

	// livenessProbe
	if x, ok := m["livenessProbe"]; ok {
		v.probe(x)
	}

	// resources
	if x, ok := m["resources"]; !ok {
		v.err(0, "containers.resources is required")
	} else {
		v.resources(x)
	}
}

func (v *validator) port(n *yaml.Node) {
	m := v.m(n)
	if x, ok := m["containerPort"]; ok {
		p, e := strconv.ParseInt(x.Value, 10, 64)
		if e != nil {
			v.err(x.Line, "containerPort must be int")
		} else if p <= 0 || p >= 65536 {
			v.err(x.Line, "containerPort value out of range")
		}
	}
	if x, ok := m["protocol"]; ok && x.Value != "TCP" && x.Value != "UDP" {
		v.err(x.Line, "protocol has unsupported value '"+x.Value+"'")
	}
}

func (v *validator) probe(n *yaml.Node) {
	m := v.m(n)
	if x, ok := m["httpGet"]; ok {
		h := v.m(x)
		if p, ok := h["path"]; ok && !pathRe.MatchString(p.Value) {
			v.err(p.Line, "path has invalid format '"+p.Value+"'")
		}
		if p, ok := h["port"]; ok {
			n, e := strconv.ParseInt(p.Value, 10, 64)
			if e != nil {
				v.err(p.Line, "port must be int")
			} else if n <= 0 || n >= 65536 {
				v.err(p.Line, "port value out of range")
			}
		}
	}
}

func (v *validator) resources(n *yaml.Node) {
	m := v.m(n)
	if x, ok := m["limits"]; ok {
		v.reslist(x)
	}
	if x, ok := m["requests"]; ok {
		v.reslist(x)
	}
}

func (v *validator) reslist(n *yaml.Node) {
	m := v.m(n)

	// cpu
	if x, ok := m["cpu"]; ok {
		if x.Tag == "!!str" {
			v.err(x.Line, "cpu must be int")
		} else if _, e := strconv.Atoi(x.Value); e != nil {
			v.err(x.Line, "cpu must be int")
		}
	}

	// memory
	if x, ok := m["memory"]; ok && !memoryRe.MatchString(x.Value) {
		v.err(x.Line, "memory has invalid format '"+x.Value+"'")
	}
}
