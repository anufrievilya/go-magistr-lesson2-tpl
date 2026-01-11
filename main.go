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
	// name must be snake_case
	snakeCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	// memory like 123Gi / 123Mi / 123Ki
	memoryRegex = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)

	// image must be in registry.bigbrother.io and contain a tag
	imageRegex = regexp.MustCompile(`^registry\.bigbrother\.io/.+:.+$`)

	// absolute path starts with /
	absolutePathRegex = regexp.MustCompile(`^/`)
)

// Validator accumulates errors and holds filename for output formatting.
type Validator struct {
	filename string
	errors   []string
}

// NewValidator creates a validator bound to filename.
func NewValidator(filename string) *Validator {
	return &Validator{
		filename: filename,
		errors:   []string{},
	}
}

// addError records an error. If line>0 it will be printed as "filename:line msg",
// otherwise as "filename msg" (used for "is required" messages).
func (v *Validator) addError(line int, message string) {
	if line > 0 {
		v.errors = append(v.errors, fmt.Sprintf("%s:%d %s", v.filename, line, message))
	} else {
		v.errors = append(v.errors, fmt.Sprintf("%s %s", v.filename, message))
	}
}

func (v *Validator) hasErrors() bool {
	return len(v.errors) > 0
}

func (v *Validator) printErrors() {
	for _, e := range v.errors {
		fmt.Fprintln(os.Stderr, e)
	}
}

// parseMap converts a Mapping node into a map[string]*yaml.Node for easier access.
// If node is DocumentNode it will use its first child. If not a mapping node returns empty map.
func (v *Validator) parseMap(node *yaml.Node) map[string]*yaml.Node {
	m := make(map[string]*yaml.Node)
	if node == nil {
		return m
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return m
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		if key != nil && val != nil {
			m[key.Value] = val
		}
	}
	return m
}

// Validate is the entry point for validating the document.
func (v *Validator) Validate(root *yaml.Node) {
	if root == nil || len(root.Content) == 0 {
		v.addError(0, "empty YAML file")
		return
	}

	doc := root.Content[0]
	if doc == nil {
		v.addError(0, "invalid document")
		return
	}

	fields := v.parseMap(doc)
	v.validateTopLevel(fields)
}

func (v *Validator) validateTopLevel(fields map[string]*yaml.Node) {
	// apiVersion required and must equal "v1"
	if apiVersion, ok := fields["apiVersion"]; !ok {
		v.addError(0, "apiVersion is required")
	} else {
		if apiVersion.Value != "v1" {
			v.addError(apiVersion.Line, "apiVersion has unsupported value '"+apiVersion.Value+"'")
		}
	}

	// kind required and must equal "Pod"
	if kind, ok := fields["kind"]; !ok {
		v.addError(0, "kind is required")
	} else {
		if kind.Value != "Pod" {
			v.addError(kind.Line, "kind has unsupported value '"+kind.Value+"'")
		}
	}

	// metadata required
	if metadata, ok := fields["metadata"]; !ok {
		v.addError(0, "metadata is required")
	} else {
		v.validateMetadata(metadata)
	}

	// spec required
	if spec, ok := fields["spec"]; !ok {
		v.addError(0, "spec is required")
	} else {
		v.validateSpec(spec)
	}
}

func (v *Validator) validateMetadata(node *yaml.Node) {
	fields := v.parseMap(node)

	// name required
	if name, ok := fields["name"]; !ok {
		v.addError(0, "metadata.name is required")
	} else {
		if strings.TrimSpace(name.Value) == "" {
			v.addError(name.Line, "name is required")
		}
	}

	// namespace optional, labels optional - no special checks beyond structure
}

func (v *Validator) validateSpec(node *yaml.Node) {
	fields := v.parseMap(node)

	// os optional, but if present must be linux or windows
	if osNode, ok := fields["os"]; ok {
		if osNode.Value != "linux" && osNode.Value != "windows" {
			v.addError(osNode.Line, "os has unsupported value '"+osNode.Value+"'")
		}
	}

	// containers required and must be a sequence
	containersNode, ok := fields["containers"]
	if !ok {
		v.addError(0, "spec.containers is required")
		return
	}
	if containersNode.Kind != yaml.SequenceNode {
		// Not a sequence -> treat as required missing/invalid
		v.addError(containersNode.Line, "spec.containers has invalid format")
		return
	}

	// Validate each container and ensure unique names
	names := map[string]bool{}
	for _, item := range containersNode.Content {
		if item == nil {
			continue
		}
		// each item should be mapping
		if item.Kind != yaml.MappingNode {
			// can't extract fields, skip
			continue
		}
		v.validateContainer(item)

		// check uniqueness
		fields := v.parseMap(item)
		if nameNode, ok := fields["name"]; ok {
			if nameNode.Value != "" {
				if names[nameNode.Value] {
					v.addError(nameNode.Line, "containers.name must be unique")
				} else {
					names[nameNode.Value] = true
				}
			}
		}
	}
}

func (v *Validator) validateContainer(node *yaml.Node) {
	fields := v.parseMap(node)

	// name required, snake_case
	if name, ok := fields["name"]; !ok {
		v.addError(0, "containers.name is required")
	} else {
		if strings.TrimSpace(name.Value) == "" {
			v.addError(name.Line, "name is required")
		} else if !snakeCaseRegex.MatchString(name.Value) {
			v.addError(name.Line, "containers.name has invalid format '"+name.Value+"'")
		}
	}

	// image required, must be registry.bigbrother.io/<...>:<tag>
	if image, ok := fields["image"]; !ok {
		v.addError(0, "containers.image is required")
	} else {
		if !imageRegex.MatchString(image.Value) {
			v.addError(image.Line, "containers.image has invalid format '"+image.Value+"'")
		}
	}

	// ports optional (sequence)
	if ports, ok := fields["ports"]; ok {
		if ports.Kind != yaml.SequenceNode {
			v.addError(ports.Line, "ports has invalid format")
		} else {
			for _, p := range ports.Content {
				if p != nil && p.Kind == yaml.MappingNode {
					v.validatePort(p)
				}
			}
		}
	}

	// readinessProbe optional but if present must contain httpGet
	if probe, ok := fields["readinessProbe"]; ok {
		v.validateProbe(probe, "readinessProbe")
	}

	// livenessProbe optional but if present must contain httpGet
	if probe, ok := fields["livenessProbe"]; ok {
		v.validateProbe(probe, "livenessProbe")
	}

	// resources required
	if resources, ok := fields["resources"]; !ok {
		v.addError(0, "containers.resources is required")
	} else {
		v.validateResources(resources)
	}
}

func (v *Validator) validatePort(node *yaml.Node) {
	fields := v.parseMap(node)

	// containerPort required
	if containerPort, ok := fields["containerPort"]; !ok {
		v.addError(0, "containerPort is required")
	} else {
		// must be int and in range 0 < x < 65536
		portNum, err := strconv.ParseInt(containerPort.Value, 10, 64)
		if err != nil {
			v.addError(containerPort.Line, "containerPort must be int")
		} else if portNum <= 0 || portNum >= 65536 {
			v.addError(containerPort.Line, "containerPort value out of range")
		}
	}

	// protocol optional default TCP, but if present must be TCP or UDP
	if protocol, ok := fields["protocol"]; ok {
		if protocol.Value != "TCP" && protocol.Value != "UDP" {
			v.addError(protocol.Line, "protocol has unsupported value '"+protocol.Value+"'")
		}
	}
}

func (v *Validator) validateProbe(node *yaml.Node, prefix string) {
	fields := v.parseMap(node)

	// httpGet required inside probe
	if httpGet, ok := fields["httpGet"]; !ok {
		v.addError(0, prefix+".httpGet is required")
	} else {
		v.validateHTTPGet(httpGet, prefix+".httpGet")
	}
}

func (v *Validator) validateHTTPGet(node *yaml.Node, prefix string) {
	fields := v.parseMap(node)

	// path required and must be absolute
	if path, ok := fields["path"]; !ok {
		v.addError(0, prefix+".path is required")
	} else {
		if !absolutePathRegex.MatchString(path.Value) {
			v.addError(path.Line, "path has invalid format '"+path.Value+"'")
		}
	}

	// port required and must be int in range
	if port, ok := fields["port"]; !ok {
		v.addError(0, prefix+".port is required")
	} else {
		portNum, err := strconv.ParseInt(port.Value, 10, 64)
		if err != nil {
			v.addError(port.Line, "port must be int")
		} else if portNum <= 0 || portNum >= 65536 {
			v.addError(port.Line, "port value out of range")
		}
	}
}

func (v *Validator) validateResources(node *yaml.Node) {
	fields := v.parseMap(node)

	// limits optional
	if limits, ok := fields["limits"]; ok {
		v.validateResourceList(limits)
	}

	// requests optional
	if requests, ok := fields["requests"]; ok {
		v.validateResourceList(requests)
	}
}

func (v *Validator) validateResourceList(node *yaml.Node) {
	fields := v.parseMap(node)

	// cpu must be integer (not string)
	if cpu, ok := fields["cpu"]; ok {
		// If YAML parser recognized it as string tag, that's an error per spec.
		if cpu.Tag == "!!str" {
			v.addError(cpu.Line, "cpu must be int")
		} else {
			if _, err := strconv.Atoi(cpu.Value); err != nil {
				v.addError(cpu.Line, "cpu must be int")
			}
		}
	}

	// memory must match memoryRegex
	if memory, ok := fields["memory"]; ok {
		if !memoryRegex.MatchString(memory.Value) {
			v.addError(memory.Line, "memory has invalid format '"+memory.Value+"'")
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: yamlvalid <yaml-file>")
		os.Exit(1)
	}
	filename := os.Args[1]

	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing YAML: %v\n", err)
		os.Exit(1)
	}

	validator := NewValidator(filename)
	validator.Validate(&root)

	if validator.hasErrors() {
		validator.printErrors()
		os.Exit(1)
	}

	os.Exit(0)
}
