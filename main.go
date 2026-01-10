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
	absolutePath   = regexp.MustCompile(`^/.*$`)
)

type Validator struct {
	errors   []string
	filename string
}

func NewValidator(filename string) *Validator {
	return &Validator{
		errors:   []string{},
		filename: filename,
	}
}

func (v *Validator) addError(line int, message string) {
	if line > 0 {
		v.errors = append(v.errors, fmt.Sprintf("%s:%d %s", v.filename, line, message))
	} else {
		v.errors = append(v.errors, fmt.Sprintf("%s %s", v.filename, message))
	}
}

func (v *Validator) printErrors() {
	for _, err := range v.errors {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (v *Validator) hasErrors() bool {
	return len(v.errors) > 0
}

func (v *Validator) parseMapping(node *yaml.Node) map[string]*yaml.Node {
	result := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i].Value
			value := node.Content[i+1]
			result[key] = value
		}
	}
	return result
}

func (v *Validator) Validate(root *yaml.Node) {
	if root == nil || len(root.Content) == 0 {
		v.addError(0, "empty YAML file")
		return
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		v.addError(doc.Line, "root must be a mapping")
		return
	}

	fields := v.parseMapping(doc)
	v.validateTopLevel(fields)
}

func (v *Validator) validateTopLevel(fields map[string]*yaml.Node) {
	// apiVersion
	if apiVersion, ok := fields["apiVersion"]; !ok {
		v.addError(0, "apiVersion is required")
	} else if apiVersion.Value != "v1" {
		v.addError(apiVersion.Line, "apiVersion has unsupported value '"+apiVersion.Value+"'")
	}

	// kind
	if kind, ok := fields["kind"]; !ok {
		v.addError(0, "kind is required")
	} else if kind.Value != "Pod" {
		v.addError(kind.Line, "kind has unsupported value '"+kind.Value+"'")
	}

	// metadata
	if metadata, ok := fields["metadata"]; !ok {
		v.addError(0, "metadata is required")
	} else if metadata.Kind == yaml.MappingNode {
		v.validateMetadata(metadata)
	}

	// spec
	if spec, ok := fields["spec"]; !ok {
		v.addError(0, "spec is required")
	} else if spec.Kind == yaml.MappingNode {
		v.validateSpec(spec)
	}
}

func (v *Validator) validateMetadata(node *yaml.Node) {
	fields := v.parseMapping(node)

	// name (required)
	if name, ok := fields["name"]; !ok {
		v.addError(0, "metadata.name is required")
	} else if strings.TrimSpace(name.Value) == "" {
		v.addError(name.Line, "metadata.name is required")
	}
}

func (v *Validator) validateSpec(node *yaml.Node) {
	fields := v.parseMapping(node)

	// os (optional)
	if osNode, ok := fields["os"]; ok {
		if osNode.Value != "linux" && osNode.Value != "windows" {
			v.addError(osNode.Line, "os has unsupported value '"+osNode.Value+"'")
		}
	}

	// containers (required)
	if containers, ok := fields["containers"]; !ok {
		v.addError(0, "spec.containers is required")
	} else if containers.Kind == yaml.SequenceNode {
		v.validateContainers(containers)
	}
}

func (v *Validator) validateContainers(node *yaml.Node) {
	for _, container := range node.Content {
		if container.Kind == yaml.MappingNode {
			v.validateContainer(container)
		}
	}
}

func (v *Validator) validateContainer(node *yaml.Node) {
	fields := v.parseMapping(node)

	// name
	if name, ok := fields["name"]; !ok {
		v.addError(0, "containers.name is required")
	} else if !snakeCaseRegex.MatchString(name.Value) {
		v.addError(name.Line, "containers.name has invalid format '"+name.Value+"'")
	}

	// image
	if image, ok := fields["image"]; !ok {
		v.addError(0, "containers.image is required")
	} else if !imageRegex.MatchString(image.Value) {
		v.addError(image.Line, "containers.image has invalid format '"+image.Value+"'")
	}

	// ports
	if ports, ok := fields["ports"]; ok && ports.Kind == yaml.SequenceNode {
		v.validatePorts(ports)
	}

	// readinessProbe
	if probe, ok := fields["readinessProbe"]; ok && probe.Kind == yaml.MappingNode {
		v.validateProbe(probe, "readinessProbe")
	}

	// livenessProbe
	if probe, ok := fields["livenessProbe"]; ok && probe.Kind == yaml.MappingNode {
		v.validateProbe(probe, "livenessProbe")
	}

	// resources
	if resources, ok := fields["resources"]; !ok {
		v.addError(0, "containers.resources is required")
	} else if resources.Kind == yaml.MappingNode {
		v.validateResources(resources)
	}
}

func (v *Validator) validatePorts(node *yaml.Node) {
	for _, port := range node.Content {
		if port.Kind == yaml.MappingNode {
			fields := v.parseMapping(port)

			if containerPort, ok := fields["containerPort"]; ok {
				portNum, err := strconv.ParseInt(containerPort.Value, 10, 64)
				if err != nil {
					v.addError(containerPort.Line, "containerPort must be int")
				} else if portNum <= 0 || portNum >= 65536 {
					v.addError(containerPort.Line, "containerPort value out of range")
				}
			}

			if protocol, ok := fields["protocol"]; ok {
				if protocol.Value != "TCP" && protocol.Value != "UDP" {
					v.addError(protocol.Line, "containers.ports.protocol has unsupported value '"+protocol.Value+"'")
				}
			}
		}
	}
}

func (v *Validator) validateProbe(node *yaml.Node, probeName string) {
	fields := v.parseMapping(node)

	if httpGet, ok := fields["httpGet"]; ok && httpGet.Kind == yaml.MappingNode {
		v.validateHTTPGet(httpGet, probeName)
	}
}

func (v *Validator) validateHTTPGet(node *yaml.Node, probeName string) {
	fields := v.parseMapping(node)

	// path
	if path, ok := fields["path"]; ok {
		if !absolutePath.MatchString(path.Value) {
			v.addError(path.Line, "containers."+probeName+".httpGet.path has invalid format '"+path.Value+"'")
		}
	}

	// port
	if port, ok := fields["port"]; ok {
		portNum, err := strconv.ParseInt(port.Value, 10, 64)
		if err != nil {
			v.addError(port.Line, "port must be int")
		} else if portNum <= 0 || portNum >= 65536 {
			v.addError(port.Line, "port value out of range")
		}
	}
}

func (v *Validator) validateResources(node *yaml.Node) {
	fields := v.parseMapping(node)

	if limits, ok := fields["limits"]; ok && limits.Kind == yaml.MappingNode {
		v.validateResourceList(limits, "limits")
	}

	if requests, ok := fields["requests"]; ok && requests.Kind == yaml.MappingNode {
		v.validateResourceList(requests, "requests")
	}
}

func (v *Validator) validateResourceList(node *yaml.Node, resourceType string) {
	fields := v.parseMapping(node)

	// cpu - ДОЛЖНО БЫТЬ INT, НЕ СТРОКА!
	if cpu, ok := fields["cpu"]; ok {
		// Проверяем тег YAML - если это строка (!!str), то ошибка
		if cpu.Tag == "!!str" {
			v.addError(cpu.Line, "cpu must be int")
		} else {
			_, err := strconv.Atoi(cpu.Value)
			if err != nil {
				v.addError(cpu.Line, "cpu must be int")
			}
		}
	}

	// memory
	if memory, ok := fields["memory"]; ok {
		if !memoryRegex.MatchString(memory.Value) {
			v.addError(memory.Line, "containers.resources."+resourceType+".memory has invalid format '"+memory.Value+"'")
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <yaml-file>\n", os.Args[0])
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
		fmt.Fprintf(os.Stderr, "cannot unmarshal YAML: %v\n", err)
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
