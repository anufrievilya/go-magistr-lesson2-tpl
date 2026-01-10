package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <yaml-file>\n", os.Args[0])
		os.Exit(1)
	}

	filePath := os.Args[1]
	content, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot read file: %v\n", filePath, err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot unmarshal YAML: %v\n", filePath, err)
		os.Exit(1)
	}

	if len(root.Content) == 0 {
		fmt.Fprintf(os.Stderr, "%s: empty YAML document\n", filePath)
		os.Exit(1)
	}

	mapper := make(map[string]*yaml.Node)
	for i := 0; i < len(root.Content[0].Content); i += 2 {
		keyNode := root.Content[0].Content[i]
		valueNode := root.Content[0].Content[i+1]
		mapper[keyNode.Value] = valueNode
	}

	errors := validatePod(filePath, mapper)
	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(1)
	}
}

type ValidationError struct {
	File   string
	Line   int
	Field  string
	Detail string
}

func (e *ValidationError) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d %s", e.File, e.Line, e.Detail)
	}
	return fmt.Sprintf("%s %s", e.File, e.Detail)
}

func validatePod(file string, pod map[string]*yaml.Node) []string {
	var errs []string

	// apiVersion
	apiVersionNode, ok := pod["apiVersion"]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s apiVersion is required", file))
	} else if apiVersionNode.Kind != yaml.ScalarNode || apiVersionNode.Value != "v1" {
		errs = append(errs, fmt.Sprintf("%s:%d apiVersion must be 'v1'", file, apiVersionNode.Line))
	}

	// kind
	kindNode, ok := pod["kind"]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s kind is required", file))
	} else if kindNode.Kind != yaml.ScalarNode || kindNode.Value != "Pod" {
		errs = append(errs, fmt.Sprintf("%s:%d kind must be 'Pod'", file, kindNode.Line))
	}

	// metadata
	metaNode, ok := pod["metadata"]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s metadata is required", file))
	} else if metaNode.Kind != yaml.MappingNode {
		errs = append(errs, fmt.Sprintf("%s:%d metadata must be an object", file, metaNode.Line))
	} else {
		metaMap := nodeToMap(metaNode)
		if nameNode, exists := metaMap["name"]; !exists {
			errs = append(errs, fmt.Sprintf("%s name is required", file))
		} else if nameNode.Kind != yaml.ScalarNode {
			errs = append(errs, fmt.Sprintf("%s:%d name must be string", file, nameNode.Line))
		} else if nameNode.Value == "" {
			errs = append(errs, fmt.Sprintf("%s:%d name is required", file, nameNode.Line))
		}
		// namespace and labels are optional
	}

	// spec
	specNode, ok := pod["spec"]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s spec is required", file))
	} else if specNode.Kind != yaml.MappingNode {
		errs = append(errs, fmt.Sprintf("%s:%d spec must be an object", file, specNode.Line))
	} else {
		specMap := nodeToMap(specNode)
		errs = append(errs, validatePodSpec(file, specMap)...)
	}

	return errs
}

func nodeToMap(node *yaml.Node) map[string]*yaml.Node {
	m := make(map[string]*yaml.Node)
	if node.Kind != yaml.MappingNode {
		return m
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]
		if key.Kind == yaml.ScalarNode {
			m[key.Value] = value
		}
	}
	return m
}

func validatePodSpec(file string, spec map[string]*yaml.Node) []string {
	var errs []string

	// os (optional)
	if osNode, exists := spec["os"]; exists {
		if osNode.Kind != yaml.MappingNode {
			errs = append(errs, fmt.Sprintf("%s:%d os must be an object", file, osNode.Line))
		} else {
			osMap := nodeToMap(osNode)
			nameNode, hasName := osMap["name"]
			if !hasName {
				errs = append(errs, fmt.Sprintf("%s name is required", file))
			} else if nameNode.Kind != yaml.ScalarNode {
				errs = append(errs, fmt.Sprintf("%s:%d os.name must be string", file, nameNode.Line))
			} else if nameNode.Value != "linux" && nameNode.Value != "windows" {
				errs = append(errs, fmt.Sprintf("%s:%d os.name has unsupported value '%s'", file, nameNode.Line, nameNode.Value))
			}
		}
	}

	// containers (required)
	containersNode, exists := spec["containers"]
	if !exists {
		errs = append(errs, fmt.Sprintf("%s containers is required", file))
	} else if containersNode.Kind != yaml.SequenceNode {
		errs = append(errs, fmt.Sprintf("%s:%d containers must be a list", file, containersNode.Line))
	} else {
		for idx, containerNode := range containersNode.Content {
			if containerNode.Kind != yaml.MappingNode {
				errs = append(errs, fmt.Sprintf("%s:%d containers[%d] must be an object", file, containerNode.Line, idx))
				continue
			}
			containerMap := nodeToMap(containerNode)
			errs = append(errs, validateContainer(file, containerMap, idx)...)
		}
	}

	return errs
}

func validateContainer(file string, cont map[string]*yaml.Node, idx int) []string {
	var errs []string

	// name (required)
	nameNode, exists := cont["name"]
	if !exists {
		errs = append(errs, fmt.Sprintf("%s name is required", file))
	} else if nameNode.Kind != yaml.ScalarNode {
		errs = append(errs, fmt.Sprintf("%s:%d containers[%d].name must be string", file, nameNode.Line, idx))
	} else if !isValidSnakeCase(nameNode.Value) {
		errs = append(errs, fmt.Sprintf("%s:%d containers[%d].name has invalid format '%s'", file, nameNode.Line, idx, nameNode.Value))
	}

	// image (required)
	imageNode, exists := cont["image"]
	if !exists {
		errs = append(errs, fmt.Sprintf("%s image is required", file))
	} else if imageNode.Kind != yaml.ScalarNode {
		errs = append(errs, fmt.Sprintf("%s:%d containers[%d].image must be string", file, imageNode.Line, idx))
	} else if !isValidImage(imageNode.Value) {
		errs = append(errs, fmt.Sprintf("%s:%d containers[%d].image has invalid format '%s'", file, imageNode.Line, idx, imageNode.Value))
	}

	// ports (optional)
	if portsNode, exists := cont["ports"]; exists {
		if portsNode.Kind != yaml.SequenceNode {
			errs = append(errs, fmt.Sprintf("%s:%d containers[%d].ports must be a list", file, portsNode.Line, idx))
		} else {
			for pIdx, portNode := range portsNode.Content {
				if portNode.Kind != yaml.MappingNode {
					errs = append(errs, fmt.Sprintf("%s:%d containers[%d].ports[%d] must be an object", file, portNode.Line, idx, pIdx))
					continue
				}
				portMap := nodeToMap(portNode)
				errs = append(errs, validateContainerPort(file, portMap, idx, pIdx)...)
			}
		}
	}

	// readinessProbe (optional)
	if rpNode, exists := cont["readinessProbe"]; exists {
		if rpNode.Kind != yaml.MappingNode {
			errs = append(errs, fmt.Sprintf("%s:%d containers[%d].readinessProbe must be an object", file, rpNode.Line, idx))
		} else {
			rpMap := nodeToMap(rpNode)
			errs = append(errs, validateProbe(file, rpMap, fmt.Sprintf("containers[%d].readinessProbe", idx), rpNode.Line)...)
		}
	}

	// livenessProbe (optional)
	if lpNode, exists := cont["livenessProbe"]; exists {
		if lpNode.Kind != yaml.MappingNode {
			errs = append(errs, fmt.Sprintf("%s:%d containers[%d].livenessProbe must be an object", file, lpNode.Line, idx))
		} else {
			lpMap := nodeToMap(lpNode)
			errs = append(errs, validateProbe(file, lpMap, fmt.Sprintf("containers[%d].livenessProbe", idx), lpNode.Line)...)
		}
	}

	// resources (required)
	resNode, exists := cont["resources"]
	if !exists {
		errs = append(errs, fmt.Sprintf("%s resources is required", file))
	} else if resNode.Kind != yaml.MappingNode {
		errs = append(errs, fmt.Sprintf("%s:%d containers[%d].resources must be an object", file, resNode.Line, idx))
	} else {
		resMap := nodeToMap(resNode)
		errs = append(errs, validateResourceRequirements(file, resMap, idx, resNode.Line)...)
	}

	return errs
}

func validateContainerPort(file string, port map[string]*yaml.Node, cIdx, pIdx int) []string {
	var errs []string

	// containerPort (required)
	cpNode, exists := port["containerPort"]
	if !exists {
		errs = append(errs, fmt.Sprintf("%s containerPort is required", file))
	} else if cpNode.Kind != yaml.ScalarNode {
		errs = append(errs, fmt.Sprintf("%s:%d containers[%d].ports[%d].containerPort must be int", file, cpNode.Line, cIdx, pIdx))
	} else {
		val, err := strconv.Atoi(cpNode.Value)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s:%d containers[%d].ports[%d].containerPort must be int", file, cpNode.Line, cIdx, pIdx))
		} else if val <= 0 || val >= 65536 {
			errs = append(errs, fmt.Sprintf("%s:%d containers[%d].ports[%d].containerPort value out of range", file, cpNode.Line, cIdx, pIdx))
		}
	}

	// protocol (optional)
	if protoNode, exists := port["protocol"]; exists {
		if protoNode.Kind != yaml.ScalarNode {
			errs = append(errs, fmt.Sprintf("%s:%d containers[%d].ports[%d].protocol must be string", file, protoNode.Line, cIdx, pIdx))
		} else if protoNode.Value != "TCP" && protoNode.Value != "UDP" {
			errs = append(errs, fmt.Sprintf("%s:%d containers[%d].ports[%d].protocol has unsupported value '%s'", file, protoNode.Line, cIdx, pIdx, protoNode.Value))
		}
	}

	return errs
}

func validateProbe(file string, probe map[string]*yaml.Node, fieldPrefix string, line int) []string {
	var errs []string

	httpGetNode, exists := probe["httpGet"]
	if !exists {
		errs = append(errs, fmt.Sprintf("%s:%d %s.httpGet is required", file, line, fieldPrefix))
	} else if httpGetNode.Kind != yaml.MappingNode {
		errs = append(errs, fmt.Sprintf("%s:%d %s.httpGet must be an object", file, httpGetNode.Line, fieldPrefix))
	} else {
		hgMap := nodeToMap(httpGetNode)
		pathNode, hasPath := hgMap["path"]
		if !hasPath {
			errs = append(errs, fmt.Sprintf("%s path is required", file))
		} else if pathNode.Kind != yaml.ScalarNode {
			errs = append(errs, fmt.Sprintf("%s:%d %s.httpGet.path must be string", file, pathNode.Line, fieldPrefix))
		} else if !strings.HasPrefix(pathNode.Value, "/") {
			errs = append(errs, fmt.Sprintf("%s:%d %s.httpGet.path has invalid format '%s'", file, pathNode.Line, fieldPrefix, pathNode.Value))
		}

		portNode, hasPort := hgMap["port"]
		if !hasPort {
			errs = append(errs, fmt.Sprintf("%s port is required", file))
		} else if portNode.Kind != yaml.ScalarNode {
			errs = append(errs, fmt.Sprintf("%s:%d %s.httpGet.port must be int", file, portNode.Line, fieldPrefix))
		} else {
			val, err := strconv.Atoi(portNode.Value)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s:%d %s.httpGet.port must be int", file, portNode.Line, fieldPrefix))
			} else if val <= 0 || val >= 65536 {
				errs = append(errs, fmt.Sprintf("%s:%d %s.httpGet.port value out of range", file, portNode.Line, fieldPrefix))
			}
		}
	}

	return errs
}

func validateResourceRequirements(file string, res map[string]*yaml.Node, cIdx int, line int) []string {
	var errs []string

	checkResource := func(field string, node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind != yaml.MappingNode {
			errs = append(errs, fmt.Sprintf("%s:%d containers[%d].resources.%s must be an object", file, node.Line, cIdx, field))
			return
		}
		rm := nodeToMap(node)

		if cpuNode, ok := rm["cpu"]; ok {
			if cpuNode.Kind != yaml.ScalarNode {
				errs = append(errs, fmt.Sprintf("%s:%d containers[%d].resources.%s.cpu must be int", file, cpuNode.Line, cIdx, field))
			} else if _, err := strconv.Atoi(cpuNode.Value); err != nil {
				errs = append(errs, fmt.Sprintf("%s:%d containers[%d].resources.%s.cpu must be int", file, cpuNode.Line, cIdx, field))
			}
		}

		if memNode, ok := rm["memory"]; ok {
			if memNode.Kind != yaml.ScalarNode {
				errs = append(errs, fmt.Sprintf("%s:%d containers[%d].resources.%s.memory must be string", file, memNode.Line, cIdx, field))
			} else if !isValidMemory(memNode.Value) {
				errs = append(errs, fmt.Sprintf("%s:%d containers[%d].resources.%s.memory has invalid format '%s'", file, memNode.Line, cIdx, field, memNode.Value))
			}
		}
	}

	checkResource("requests", res["requests"])
	checkResource("limits", res["limits"])

	return errs
}

var snakeCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func isValidSnakeCase(s string) bool {
	return snakeCaseRegex.MatchString(s)
}

func isValidImage(s string) bool {
	if !strings.HasPrefix(s, "registry.bigbrother.io/") {
		return false
	}
	parts := strings.Split(s[len("registry.bigbrother.io/"):], ":")
	if len(parts) != 2 {
		return false
	}
	return parts[1] != ""
}

var memoryRegex = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)

func isValidMemory(s string) bool {
	return memoryRegex.MatchString(s)
}
