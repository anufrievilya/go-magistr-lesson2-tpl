package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Регулярные выражения для валидации
var (
	snakeCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	memoryRegex    = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)
	imageRegex     = regexp.MustCompile(`^registry\.bigbrother\.io/[^:]+:.+$`)
	absolutePath   = regexp.MustCompile(`^/.*$`)
)

type ValidationError struct {
	Line    int
	Message string
}

func (e ValidationError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%d %s", e.Line, e.Message)
	}
	return e.Message
}

type Validator struct {
	errors   []ValidationError
	filename string
}

func NewValidator(filename string) *Validator {
	return &Validator{
		errors:   []ValidationError{},
		filename: filename,
	}
}

func (v *Validator) addError(line int, message string) {
	v.errors = append(v.errors, ValidationError{Line: line, Message: message})
}

func (v *Validator) printErrors() {
	for _, err := range v.errors {
		if err.Line > 0 {
			fmt.Fprintf(os.Stderr, "%s:%d %s\n", v.filename, err.Line, err.Message)
		} else {
			fmt.Fprintf(os.Stderr, "%s %s\n", v.filename, err.Message)
		}
	}
}

func (v *Validator) hasErrors() bool {
	return len(v.errors) > 0
}

// Validate main function
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

	// Validate top-level fields
	v.validateTopLevel(fields)
}

func (v *Validator) parseMapping(node *yaml.Node) map[string]*yaml.Node {
	result := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		result[key] = value
	}
	return result
}

func (v *Validator) validateTopLevel(fields map[string]*yaml.Node) {
	// apiVersion
	apiVersion, ok := fields["apiVersion"]
	if !ok {
		v.addError(0, "apiVersion is required")
	} else {
		if apiVersion.Kind != yaml.ScalarNode || apiVersion.Value != "v1" {
			v.addError(apiVersion.Line, "apiVersion has unsupported value '"+apiVersion.Value+"'")
		}
	}

	// kind
	kind, ok := fields["kind"]
	if !ok {
		v.addError(0, "kind is required")
	} else {
		if kind.Kind != yaml.ScalarNode || kind.Value != "Pod" {
			v.addError(kind.Line, "kind has unsupported value '"+kind.Value+"'")
		}
	}

	// metadata
	metadata, ok := fields["metadata"]
	if !ok {
		v.addError(0, "metadata is required")
	} else {
		if metadata.Kind != yaml.MappingNode {
			v.addError(metadata.Line, "metadata must be object")
		} else {
			v.validateMetadata(metadata)
		}
	}

	// spec
	spec, ok := fields["spec"]
	if !ok {
		v.addError(0, "spec is required")
	} else {
		if spec.Kind != yaml.MappingNode {
			v.addError(spec.Line, "spec must be object")
		} else {
			v.validateSpec(spec)
		}
	}
}

func (v *Validator) validateMetadata(node *yaml.Node) {
	fields := v.parseMapping(node)

	// name (required)
	name, ok := fields["name"]
	if !ok {
		v.addError(0, "metadata.name is required")
	} else if name.Kind != yaml.ScalarNode {
		v.addError(name.Line, "metadata.name must be string")
	}

	// namespace (optional)
	if namespace, ok := fields["namespace"]; ok {
		if namespace.Kind != yaml.ScalarNode {
			v.addError(namespace.Line, "metadata.namespace must be string")
		}
	}

	// labels (optional)
	if labels, ok := fields["labels"]; ok {
		if labels.Kind != yaml.MappingNode {
			v.addError(labels.Line, "metadata.labels must be object")
		}
	}
}

func (v *Validator) validateSpec(node *yaml.Node) {
	fields := v.parseMapping(node)

	// os (optional)
	if osNode, ok := fields["os"]; ok {
		if osNode.Kind != yaml.ScalarNode {
			v.addError(osNode.Line, "spec.os must be string")
		} else if osNode.Value != "linux" && osNode.Value != "windows" {
			v.addError(osNode.Line, "spec.os has unsupported value '"+osNode.Value+"'")
		}
	}

	// containers (required)
	containers, ok := fields["containers"]
	if !ok {
		v.addError(0, "spec.containers is required")
	} else {
		if containers.Kind != yaml.SequenceNode {
			v.addError(containers.Line, "spec.containers must be array")
		} else {
			v.validateContainers(containers)
		}
	}
}

func (v *Validator) validateContainers(node *yaml.Node) {
	for _, container := range node.Content {
		if container.Kind != yaml.MappingNode {
			v.addError(container.Line, "container must be object")
			continue
		}
		v.validateContainer(container)
	}
}

func (v *Validator) validateContainer(node *yaml.Node) {
	fields := v.parseMapping(node)

	// name (required)
	name, ok := fields["name"]
	if !ok {
		v.addError(0, "containers.name is required")
	} else {
		if name.Kind != yaml.ScalarNode {
			v.addError(name.Line, "containers.name must be string")
		} else if !snakeCaseRegex.MatchString(name.Value) {
			v.addError(name.Line, "containers.name has invalid format '"+name.Value+"'")
		}
	}

	// image (required)
	image, ok := fields["image"]
	if !ok {
		v.addError(0, "containers.image is required")
	} else {
		if image.Kind != yaml.ScalarNode {
			v.addError(image.Line, "containers.image must be string")
		} else if !imageRegex.MatchString(image.Value) {
			v.addError(image.Line, "containers.image has invalid format '"+image.Value+"'")
		}
	}

	// ports (optional)
	if ports, ok := fields["ports"]; ok {
		if ports.Kind != yaml.SequenceNode {
			v.addError(ports.Line, "containers.ports must be array")
		} else {
			v.validatePorts(ports)
		}
	}

	// readinessProbe (optional)
	if probe, ok := fields["readinessProbe"]; ok {
		if probe.Kind != yaml.MappingNode {
			v.addError(probe.Line, "containers.readinessProbe must be object")
		} else {
			v.validateProbe(probe, "readinessProbe")
		}
	}

	// livenessProbe (optional)
	if probe, ok := fields["livenessProbe"]; ok {
		if probe.Kind != yaml.MappingNode {
			v.addError(probe.Line, "containers.livenessProbe must be object")
		} else {
			v.validateProbe(probe, "livenessProbe")
		}
	}

	// resources (required)
	resources, ok := fields["resources"]
	if !ok {
		v.addError(0, "containers.resources is required")
	} else {
		if resources.Kind != yaml.MappingNode {
			v.addError(resources.Line, "containers.resources must be object")
		} else {
			v.validateResources(resources)
		}
	}
}

func (v *Validator) validatePorts(node *yaml.Node) {
	for _, port := range node.Content {
		if port.Kind != yaml.MappingNode {
			v.addError(port.Line, "port must be object")
			continue
		}

		fields := v.parseMapping(port)

		// containerPort (required)
		containerPort, ok := fields["containerPort"]
		if !ok {
			v.addError(0, "containers.ports.containerPort is required")
		} else {
			if containerPort.Kind != yaml.ScalarNode {
				v.addError(containerPort.Line, "containers.ports.containerPort must be int")
			} else {
				portNum, err := strconv.Atoi(containerPort.Value)
				if err != nil {
					v.addError(containerPort.Line, "containers.ports.containerPort must be int")
				} else if portNum <= 0 || portNum >= 65536 {
					v.addError(containerPort.Line, "containers.ports.containerPort value out of range")
				}
			}
		}

		// protocol (optional)
		if protocol, ok := fields["protocol"]; ok {
			if protocol.Kind != yaml.ScalarNode {
				v.addError(protocol.Line, "containers.ports.protocol must be string")
			} else if protocol.Value != "TCP" && protocol.Value != "UDP" {
				v.addError(protocol.Line, "containers.ports.protocol has unsupported value '"+protocol.Value+"'")
			}
		}
	}
}

func (v *Validator) validateProbe(node *yaml.Node, probeName string) {
	fields := v.parseMapping(node)

	// httpGet (required)
	httpGet, ok := fields["httpGet"]
	if !ok {
		v.addError(0, "containers."+probeName+".httpGet is required")
	} else {
		if httpGet.Kind != yaml.MappingNode {
			v.addError(httpGet.Line, "containers."+probeName+".httpGet must be object")
		} else {
			v.validateHTTPGet(httpGet, probeName)
		}
	}
}

func (v *Validator) validateHTTPGet(node *yaml.Node, probeName string) {
	fields := v.parseMapping(node)

	// path (required)
	path, ok := fields["path"]
	if !ok {
		v.addError(0, "containers."+probeName+".httpGet.path is required")
	} else {
		if path.Kind != yaml.ScalarNode {
			v.addError(path.Line, "containers."+probeName+".httpGet.path must be string")
		} else if !absolutePath.MatchString(path.Value) {
			v.addError(path.Line, "containers."+probeName+".httpGet.path has invalid format '"+path.Value+"'")
		}
	}

	// port (required)
	port, ok := fields["port"]
	if !ok {
		v.addError(0, "containers."+probeName+".httpGet.port is required")
	} else {
		if port.Kind != yaml.ScalarNode {
			v.addError(port.Line, "containers."+probeName+".httpGet.port must be int")
		} else {
			portNum, err := strconv.Atoi(port.Value)
			if err != nil {
				v.addError(port.Line, "containers."+probeName+".httpGet.port must be int")
			} else if portNum <= 0 || portNum >= 65536 {
				v.addError(port.Line, "containers."+probeName+".httpGet.port value out of range")
			}
		}
	}
}

func (v *Validator) validateResources(node *yaml.Node) {
	fields := v.parseMapping(node)

	// limits (optional)
	if limits, ok := fields["limits"]; ok {
		if limits.Kind != yaml.MappingNode {
			v.addError(limits.Line, "containers.resources.limits must be object")
		} else {
			v.validateResourceList(limits, "limits")
		}
	}

	// requests (optional)
	if requests, ok := fields["requests"]; ok {
		if requests.Kind != yaml.MappingNode {
			v.addError(requests.Line, "containers.resources.requests must be object")
		} else {
			v.validateResourceList(requests, "requests")
		}
	}
}

func (v *Validator) validateResourceList(node *yaml.Node, resourceType string) {
	fields := v.parseMapping(node)

	// cpu (optional)
	if cpu, ok := fields["cpu"]; ok {
		if cpu.Kind != yaml.ScalarNode {
			v.addError(cpu.Line, "containers.resources."+resourceType+".cpu must be int")
		} else {
			_, err := strconv.Atoi(cpu.Value)
			if err != nil {
				v.addError(cpu.Line, "containers.resources."+resourceType+".cpu must be int")
			}
		}
	}

	// memory (optional)
	if memory, ok := fields["memory"]; ok {
		if memory.Kind != yaml.ScalarNode {
			v.addError(memory.Line, "containers.resources."+resourceType+".memory must be string")
		} else if !memoryRegex.MatchString(memory.Value) {
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

	// Read file
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read file: %v\n", err)
		os.Exit(1)
	}

	// Parse YAML
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintf(os.Stderr, "cannot unmarshal YAML: %v\n", err)
		os.Exit(1)
	}

	// Validate
	validator := NewValidator(filename)
	validator.Validate(&root)

	if validator.hasErrors() {
		validator.printErrors()
		os.Exit(1)
	}

	os.Exit(0)
}
