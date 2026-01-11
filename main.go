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
	snakeCaseRegex     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	memoryRegex        = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)
	imageRegex         = regexp.MustCompile(`^registry\.bigbrother\.io/[^:]+:.+$`)
	absolutePathRegexp = regexp.MustCompile(`^/`)
)

type Validator struct {
	filename string
	errors   []string
}

func NewValidator(filename string) *Validator {
	return &Validator{filename: filename, errors: []string{}}
}

func (v *Validator) addError(line int, message string) {
	if line > 0 {
		v.errors = append(v.errors, fmt.Sprintf("%s:%d %s", v.filename, line, message))
	} else {
		v.errors = append(v.errors, fmt.Sprintf("%s %s", v.filename, message))
	}
}

func (v *Validator) hasErrors() bool { return len(v.errors) > 0 }

func (v *Validator) printErrors() {
	for _, e := range v.errors {
		fmt.Fprintln(os.Stderr, e)
	}
}

// parseMap: если node — DocumentNode, берем node.Content[0], затем ожидаем MappingNode.
// Возвращаем map ключ->*yaml.Node. Если не mapping, возвращаем пустую map.
func (v *Validator) parseMap(node *yaml.Node) map[string]*yaml.Node {
	res := make(map[string]*yaml.Node)
	if node == nil {
		return res
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node == nil || node.Kind != yaml.MappingNode {
		return res
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		k := node.Content[i]
		val := node.Content[i+1]
		if k != nil && val != nil {
			res[k.Value] = val
		}
	}
	return res
}

func (v *Validator) Validate(root *yaml.Node) {
	if root == nil || len(root.Content) == 0 {
		v.addError(0, "empty YAML file")
		return
	}
	// Обход всех документов
	for _, doc := range root.Content {
		if doc == nil {
			v.addError(0, "invalid document")
			continue
		}
		fields := v.parseMap(doc)
		v.validateTopLevel(fields)
	}
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
	} else {
		v.validateMetadata(metadata)
	}

	// spec
	if spec, ok := fields["spec"]; !ok {
		v.addError(0, "spec is required")
	} else {
		v.validateSpec(spec)
	}
}

func (v *Validator) validateMetadata(node *yaml.Node) {
	fields := v.parseMap(node)
	if name, ok := fields["name"]; !ok {
		v.addError(0, "metadata.name is required")
	} else {
		if strings.TrimSpace(name.Value) == "" {
			v.addError(name.Line, "name is required")
		}
	}
	// namespace и labels — необязательные
}

func (v *Validator) validateSpec(node *yaml.Node) {
	fields := v.parseMap(node)

	if osNode, ok := fields["os"]; ok {
		if osNode.Value != "linux" && osNode.Value != "windows" {
			v.addError(osNode.Line, "os has unsupported value '"+osNode.Value+"'")
		}
	}

	containersNode, ok := fields["containers"]
	if !ok {
		v.addError(0, "spec.containers is required")
		return
	}
	if containersNode.Kind != yaml.SequenceNode {
		v.addError(containersNode.Line, "spec.containers has invalid format")
		return
	}

	names := map[string]bool{}
	for _, c := range containersNode.Content {
		if c == nil {
			continue
		}
		if c.Kind != yaml.MappingNode {
			v.addError(c.Line, "containers has invalid format")
			continue
		}
		v.validateContainer(c)

		cfields := v.parseMap(c)
		if nameNode, ok := cfields["name"]; ok {
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

	// name
	if name, ok := fields["name"]; !ok {
		v.addError(0, "containers.name is required")
	} else {
		if strings.TrimSpace(name.Value) == "" {
			v.addError(name.Line, "name is required")
		} else if !snakeCaseRegex.MatchString(name.Value) {
			v.addError(name.Line, "containers.name has invalid format '"+name.Value+"'")
		}
	}

	// image
	if image, ok := fields["image"]; !ok {
		v.addError(0, "containers.image is required")
	} else {
		if !imageRegex.MatchString(image.Value) {
			v.addError(image.Line, "containers.image has invalid format '"+image.Value+"'")
		}
	}

	// ports
	if ports, ok := fields["ports"]; ok {
		if ports.Kind != yaml.SequenceNode {
			v.addError(ports.Line, "ports has invalid format")
		} else {
			for _, p := range ports.Content {
				if p == nil {
					continue
				}
				if p.Kind != yaml.MappingNode {
					v.addError(p.Line, "ports has invalid format")
					continue
				}
				v.validatePort(p)
			}
		}
	}

	// readinessProbe/livenessProbe
	if rp, ok := fields["readinessProbe"]; ok {
		v.validateProbe(rp, "readinessProbe")
	}
	if lp, ok := fields["livenessProbe"]; ok {
		v.validateProbe(lp, "livenessProbe")
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

	if cp, ok := fields["containerPort"]; !ok {
		v.addError(0, "containerPort is required")
	} else {
		// пытаемся распарсить как целое
		if _, err := strconv.ParseInt(cp.Value, 10, 64); err != nil {
			// если парсинг не удался -> must be int
			v.addError(cp.Line, "containerPort must be int")
		} else {
			portNum, _ := strconv.ParseInt(cp.Value, 10, 64)
			if portNum <= 0 || portNum >= 65536 {
				v.addError(cp.Line, "containerPort value out of range")
			}
		}
	}

	if proto, ok := fields["protocol"]; ok {
		if proto.Value != "TCP" && proto.Value != "UDP" {
			v.addError(proto.Line, "protocol has unsupported value '"+proto.Value+"'")
		}
	}
}

func (v *Validator) validateProbe(node *yaml.Node, prefix string) {
	fields := v.parseMap(node)
	if httpGet, ok := fields["httpGet"]; !ok {
		v.addError(0, prefix+".httpGet is required")
	} else {
		v.validateHTTPGet(httpGet)
	}
}

func (v *Validator) validateHTTPGet(node *yaml.Node) {
	fields := v.parseMap(node)

	if pathNode, ok := fields["path"]; !ok {
		v.addError(0, "path is required")
	} else {
		if !absolutePathRegexp.MatchString(pathNode.Value) {
			v.addError(pathNode.Line, "path has invalid format '"+pathNode.Value+"'")
		}
	}

	if portNode, ok := fields["port"]; !ok {
		v.addError(0, "port is required")
	} else {
		if _, err := strconv.ParseInt(portNode.Value, 10, 64); err != nil {
			v.addError(portNode.Line, "port must be int")
		} else {
			portNum, _ := strconv.ParseInt(portNode.Value, 10, 64)
			if portNum <= 0 || portNum >= 65536 {
				v.addError(portNode.Line, "port value out of range")
			}
		}
	}
}

func (v *Validator) validateResources(node *yaml.Node) {
	fields := v.parseMap(node)
	if limits, ok := fields["limits"]; ok {
		v.validateResourceList(limits)
	}
	if requests, ok := fields["requests"]; ok {
		v.validateResourceList(requests)
	}
}

func (v *Validator) validateResourceList(node *yaml.Node) {
	fields := v.parseMap(node)
	if cpu, ok := fields["cpu"]; ok {
		// строго: если тег scalar == !!str — это строка => ошибка "must be int"
		if cpu.Tag == "!!str" {
			v.addError(cpu.Line, "cpu must be int")
		} else {
			if _, err := strconv.Atoi(cpu.Value); err != nil {
				v.addError(cpu.Line, "cpu must be int")
			}
		}
	}
	if mem, ok := fields["memory"]; ok {
		if !memoryRegex.MatchString(mem.Value) {
			v.addError(mem.Line, "memory has invalid format '"+mem.Value+"'")
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
	// Разбор YAML
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
