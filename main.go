package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

var (
	snakeCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	memoryRegex    = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)
	imageRegex     = regexp.MustCompile(`^registry\.bigbrother\.io/[^:]+:.+$`)
	absPathRegex   = regexp.MustCompile(`^/`)
)

type Validator struct {
	filename string
	errors   []string
}

func NewValidator(filename string) *Validator {
	// use base name to match test expectations
	return &Validator{filename: filepath.Base(filename), errors: []string{}}
}

func (v *Validator) addErrorLine(line int, message string) {
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

// helpers to safely access mapping/sequence/scalar values

// getMapValue returns value node for a given key in a mapping or document node, or nil.
func getMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	// if document, dive into first child
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]
		if k != nil && k.Value == key {
			return v
		}
	}
	return nil
}

// getSequence ensures node is a sequence and returns it, otherwise nil.
func getSequence(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind == yaml.SequenceNode {
		return node
	}
	return nil
}

// isScalar returns true if node is scalar.
func isScalar(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode
}

// parseInt tries to parse scalar node into integer, returns value and error.
func parseInt(node *yaml.Node) (int64, error) {
	if node == nil {
		return 0, fmt.Errorf("nil node")
	}
	return strconv.ParseInt(node.Value, 10, 64)
}

// Validate a single document node (which should be mapping describing Pod)
func (v *Validator) validateDocument(doc *yaml.Node) {
	if doc == nil {
		v.addErrorLine(0, "invalid document")
		return
	}
	// top-level keys: apiVersion, kind, metadata, spec
	// apiVersion
	api := getMapValue(doc, "apiVersion")
	if api == nil {
		v.addErrorLine(0, "apiVersion is required")
	} else if api.Value != "v1" {
		v.addErrorLine(api.Line, "apiVersion has unsupported value '"+api.Value+"'")
	}

	// kind
	kind := getMapValue(doc, "kind")
	if kind == nil {
		v.addErrorLine(0, "kind is required")
	} else if kind.Value != "Pod" {
		v.addErrorLine(kind.Line, "kind has unsupported value '"+kind.Value+"'")
	}

	// metadata
	metadata := getMapValue(doc, "metadata")
	if metadata == nil {
		v.addErrorLine(0, "metadata is required")
	} else {
		v.validateMetadata(metadata)
	}

	// spec
	spec := getMapValue(doc, "spec")
	if spec == nil {
		v.addErrorLine(0, "spec is required")
	} else {
		v.validateSpec(spec)
	}
}

func (v *Validator) validateMetadata(node *yaml.Node) {
	// name required (must be non-empty string)
	name := getMapValue(node, "name")
	if name == nil {
		// missing field -> message without line
		v.addErrorLine(0, "metadata.name is required")
	} else {
		// empty string is an error at the line
		if name.Value == "" {
			v.addErrorLine(name.Line, "name is required")
		}
	}
	// namespace, labels optional -> no extra checks needed
}

func (v *Validator) validateSpec(node *yaml.Node) {
	// os optional but if present must be linux or windows
	osNode := getMapValue(node, "os")
	if osNode != nil {
		if osNode.Value != "linux" && osNode.Value != "windows" {
			v.addErrorLine(osNode.Line, "os has unsupported value '"+osNode.Value+"'")
		}
	}

	// containers required sequence
	containers := getMapValue(node, "containers")
	if containers == nil {
		v.addErrorLine(0, "spec.containers is required")
		return
	}
	seq := getSequence(containers)
	if seq == nil {
		// if it's present but not a sequence, report with line
		v.addErrorLine(containers.Line, "spec.containers has invalid format")
		return
	}

	seenNames := map[string]bool{}
	for _, c := range seq.Content {
		if c == nil {
			continue
		}
		if c.Kind != yaml.MappingNode {
			v.addErrorLine(c.Line, "containers has invalid format")
			continue
		}
		v.validateContainer(c)

		// check uniqueness of container names (if present)
		nameNode := getMapValue(c, "name")
		if nameNode != nil && nameNode.Value != "" {
			if seenNames[nameNode.Value] {
				v.addErrorLine(nameNode.Line, "containers.name must be unique")
			} else {
				seenNames[nameNode.Value] = true
			}
		}
	}
}

func (v *Validator) validateContainer(node *yaml.Node) {
	// name required and must be snake_case
	name := getMapValue(node, "name")
	if name == nil {
		v.addErrorLine(0, "containers.name is required")
	} else {
		if name.Value == "" {
			v.addErrorLine(name.Line, "name is required")
		} else if !snakeCaseRegex.MatchString(name.Value) {
			v.addErrorLine(name.Line, "containers.name has invalid format '"+name.Value+"'")
		}
	}

	// image required and must match pattern
	image := getMapValue(node, "image")
	if image == nil {
		v.addErrorLine(0, "containers.image is required")
	} else {
		if !imageRegex.MatchString(image.Value) {
			v.addErrorLine(image.Line, "containers.image has invalid format '"+image.Value+"'")
		}
	}

	// ports optional
	ports := getMapValue(node, "ports")
	if ports != nil {
		seq := getSequence(ports)
		if seq == nil {
			v.addErrorLine(ports.Line, "ports has invalid format")
		} else {
			for _, p := range seq.Content {
				if p == nil {
					continue
				}
				if p.Kind != yaml.MappingNode {
					v.addErrorLine(p.Line, "ports has invalid format")
					continue
				}
				v.validatePort(p)
			}
		}
	}

	// readinessProbe, livenessProbe optional
	if rp := getMapValue(node, "readinessProbe"); rp != nil {
		v.validateProbe(rp, "readinessProbe")
	}
	if lp := getMapValue(node, "livenessProbe"); lp != nil {
		v.validateProbe(lp, "livenessProbe")
	}

	// resources required
	resources := getMapValue(node, "resources")
	if resources == nil {
		v.addErrorLine(0, "containers.resources is required")
	} else {
		v.validateResources(resources)
	}
}

func (v *Validator) validatePort(node *yaml.Node) {
	// containerPort required
	cp := getMapValue(node, "containerPort")
	if cp == nil {
		v.addErrorLine(0, "containerPort is required")
	} else {
		if _, err := strconv.ParseInt(cp.Value, 10, 64); err != nil {
			v.addErrorLine(cp.Line, "containerPort must be int")
		} else {
			val, _ := strconv.ParseInt(cp.Value, 10, 64)
			if val <= 0 || val >= 65536 {
				v.addErrorLine(cp.Line, "containerPort value out of range")
			}
		}
	}

	// protocol optional: if present must be TCP or UDP
	proto := getMapValue(node, "protocol")
	if proto != nil {
		if proto.Value != "TCP" && proto.Value != "UDP" {
			v.addErrorLine(proto.Line, "protocol has unsupported value '"+proto.Value+"'")
		}
	}
}

func (v *Validator) validateProbe(node *yaml.Node, _prefix string) {
	// httpGet required
	httpGet := getMapValue(node, "httpGet")
	if httpGet == nil {
		// missing httpGet — required (no line number)
		v.addErrorLine(0, "httpGet is required")
	} else {
		v.validateHTTPGet(httpGet)
	}
}

func (v *Validator) validateHTTPGet(node *yaml.Node) {
	// path required and must be absolute
	path := getMapValue(node, "path")
	if path == nil {
		v.addErrorLine(0, "path is required")
	} else {
		if !absPathRegex.MatchString(path.Value) {
			v.addErrorLine(path.Line, "path has invalid format '"+path.Value+"'")
		}
	}

	// port required and must be int in range
	port := getMapValue(node, "port")
	if port == nil {
		v.addErrorLine(0, "port is required")
	} else {
		if _, err := strconv.ParseInt(port.Value, 10, 64); err != nil {
			v.addErrorLine(port.Line, "port must be int")
		} else {
			val, _ := strconv.ParseInt(port.Value, 10, 64)
			if val <= 0 || val >= 65536 {
				v.addErrorLine(port.Line, "port value out of range")
			}
		}
	}
}

func (v *Validator) validateResources(node *yaml.Node) {
	limits := getMapValue(node, "limits")
	if limits != nil {
		v.validateResourceList(limits)
	}
	requests := getMapValue(node, "requests")
	if requests != nil {
		v.validateResourceList(requests)
	}
}

func (v *Validator) validateResourceList(node *yaml.Node) {
	// cpu must be integer (not string)
	cpu := getMapValue(node, "cpu")
	if cpu != nil {
		// if YAML parser tagged it as string, that's an error per spec
		if cpu.Tag == "!!str" {
			v.addErrorLine(cpu.Line, "cpu must be int")
		} else {
			if _, err := strconv.Atoi(cpu.Value); err != nil {
				v.addErrorLine(cpu.Line, "cpu must be int")
			}
		}
	}

	// memory must match memory regex
	mem := getMapValue(node, "memory")
	if mem != nil {
		if !memoryRegex.MatchString(mem.Value) {
			v.addErrorLine(mem.Line, "memory has invalid format '"+mem.Value+"'")
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

	// root may contain multiple documents
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		for _, doc := range root.Content {
			validator.validateDocument(doc)
		}
	} else {
		// fallback: validate root as a document
		validator.validateDocument(&root)
	}

	if validator.hasErrors() {
		validator.printErrors()
		os.Exit(1)
	}
	os.Exit(0)
}
