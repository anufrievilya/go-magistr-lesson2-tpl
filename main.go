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
	snakeCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	memoryRegex    = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)
	imageRegex     = regexp.MustCompile(`^registry\.bigbrother\.io/[^:]+:.+$`)
	absolutePath   = regexp.MustCompile(`^/`)
)

type Validator struct {
	filename string
	errors   []string
}

func NewValidator(filename string) *Validator {
	return &Validator{filename: filename, errors: []string{}}
}

func (v *Validator) addError(line int, msg string) {
	if line > 0 {
		v.errors = append(v.errors, fmt.Sprintf("%s:%d %s", v.filename, line, msg))
	} else {
		v.errors = append(v.errors, fmt.Sprintf("%s %s", v.filename, msg))
	}
}

func (v *Validator) hasErrors() bool {
	return len(v.errors) > 0
}

func (v *Validator) printErrors() {
	for _, err := range v.errors {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (v *Validator) getMap(node *yaml.Node) map[string]*yaml.Node {
	m := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content)-1; i += 2 {
		m[node.Content[i].Value] = node.Content[i+1]
	}
	return m
}

func (v *Validator) validate(root *yaml.Node) {
	if len(root.Content) == 0 {
		v.addError(0, "empty file")
		return
	}

	doc := root.Content[0]
	fields := v.getMap(doc)

	// apiVersion
	if n, ok := fields["apiVersion"]; !ok {
		v.addError(0, "apiVersion is required")
	} else if n.Value != "v1" {
		v.addError(n.Line, "apiVersion has unsupported value '"+n.Value+"'")
	}

	// kind
	if n, ok := fields["kind"]; !ok {
		v.addError(0, "kind is required")
	} else if n.Value != "Pod" {
		v.addError(n.Line, "kind has unsupported value '"+n.Value+"'")
	}

	// metadata
	if n, ok := fields["metadata"]; !ok {
		v.addError(0, "metadata is required")
	} else {
		v.validateMetadata(n)
	}

	// spec
	if n, ok := fields["spec"]; !ok {
		v.addError(0, "spec is required")
	} else {
		v.validateSpec(n)
	}
}

func (v *Validator) validateMetadata(node *yaml.Node) {
	fields := v.getMap(node)

	// name
	if n, ok := fields["name"]; !ok {
		v.addError(0, "metadata.name is required")
	} else if strings.TrimSpace(n.Value) == "" {
		v.addError(n.Line, "metadata.name is required")
	}
}

func (v *Validator) validateSpec(node *yaml.Node) {
	fields := v.getMap(node)

	// os (optional)
	if n, ok := fields["os"]; ok {
		if n.Value != "linux" && n.Value != "windows" {
			v.addError(n.Line, "os has unsupported value '"+n.Value+"'")
		}
	}

	// containers
	if n, ok := fields["containers"]; !ok {
		v.addError(0, "spec.containers is required")
	} else {
		for _, cont := range n.Content {
			v.validateContainer(cont)
		}
	}
}

func (v *Validator) validateContainer(node *yaml.Node) {
	fields := v.getMap(node)

	// name
	if n, ok := fields["name"]; !ok {
		v.addError(0, "containers.name is required")
	} else if strings.TrimSpace(n.Value) == "" {
		v.addError(n.Line, "name is required")
	} else if !snakeCaseRegex.MatchString(n.Value) {
		v.addError(n.Line, "containers.name has invalid format '"+n.Value+"'")
	}

	// image
	if n, ok := fields["image"]; !ok {
		v.addError(0, "containers.image is required")
	} else if !imageRegex.MatchString(n.Value) {
		v.addError(n.Line, "containers.image has invalid format '"+n.Value+"'")
	}

	// ports
	if n, ok := fields["ports"]; ok {
		for _, p := range n.Content {
			v.validatePort(p)
		}
	}

	// readinessProbe
	if n, ok := fields["readinessProbe"]; ok {
		v.validateProbe(n)
	}

	// livenessProbe
	if n, ok := fields["livenessProbe"]; ok {
		v.validateProbe(n)
	}

	// resources
	if n, ok := fields["resources"]; !ok {
		v.addError(0, "containers.resources is required")
	} else {
		v.validateResources(n)
	}
}

func (v *Validator) validatePort(node *yaml.Node) {
	fields := v.getMap(node)

	if n, ok := fields["containerPort"]; ok {
		port, err := strconv.ParseInt(n.Value, 10, 64)
		if err != nil {
			v.addError(n.Line, "containerPort must be int")
		} else if port <= 0 || port >= 65536 {
			v.addError(n.Line, "containerPort value out of range")
		}
	}

	if n, ok := fields["protocol"]; ok {
		if n.Value != "TCP" && n.Value != "UDP" {
			v.addError(n.Line, "protocol has unsupported value '"+n.Value+"'")
		}
	}
}

func (v *Validator) validateProbe(node *yaml.Node) {
	fields := v.getMap(node)

	if n, ok := fields["httpGet"]; ok {
		httpFields := v.getMap(n)

		if path, ok := httpFields["path"]; ok {
			if !absolutePath.MatchString(path.Value) {
				v.addError(path.Line, "path has invalid format '"+path.Value+"'")
			}
		}

		if portNode, ok := httpFields["port"]; ok {
			port, err := strconv.ParseInt(portNode.Value, 10, 64)
			if err != nil {
				v.addError(portNode.Line, "port must be int")
			} else if port <= 0 || port >= 65536 {
				v.addError(portNode.Line, "port value out of range")
			}
		}
	}
}

func (v *Validator) validateResources(node *yaml.Node) {
	fields := v.getMap(node)

	if n, ok := fields["limits"]; ok {
		v.validateResourceList(n)
	}

	if n, ok := fields["requests"]; ok {
		v.validateResourceList(n)
	}
}

func (v *Validator) validateResourceList(node *yaml.Node) {
	fields := v.getMap(node)

	// cpu
	if n, ok := fields["cpu"]; ok {
		// Проверяем что это INT, а не строка
		if n.Tag == "!!str" {
			v.addError(n.Line, "cpu must be int")
		} else {
			_, err := strconv.Atoi(n.Value)
			if err != nil {
				v.addError(n.Line, "cpu must be int")
			}
		}
	}

	// memory
	if n, ok := fields["memory"]; ok {
		if !memoryRegex.MatchString(n.Value) {
			v.addError(n.Line, "memory has invalid format '"+n.Value+"'")
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: yamlvalid <file>")
		os.Exit(1)
	}

	filename := os.Args[1]

	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read file: %v\n", err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintf(os.Stderr, "cannot parse YAML: %v\n", err)
		os.Exit(1)
	}

	validator := NewValidator(filename)
	validator.validate(&root)

	if validator.hasErrors() {
		validator.printErrors()
		os.Exit(1)
	}

	os.Exit(0)
}
