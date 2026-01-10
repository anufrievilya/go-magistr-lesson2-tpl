package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Структуры для валидации
type Pod struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   ObjectMeta        `yaml:"metadata"`
	Spec       PodSpec           `yaml:"spec"`
	Errors     []ValidationError `yaml:"-"`
}

type ObjectMeta struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
	Line      int               `yaml:"-"`
}

type PodSpec struct {
	OS         *PodOS      `yaml:"os"`
	Containers []Container `yaml:"containers"`
	Line       int         `yaml:"-"`
}

type PodOS struct {
	Name string `yaml:"name"`
	Line int    `yaml:"-"`
}

type Container struct {
	Name           string               `yaml:"name"`
	Image          string               `yaml:"image"`
	Ports          []ContainerPort      `yaml:"ports"`
	ReadinessProbe *Probe               `yaml:"readinessProbe"`
	LivenessProbe  *Probe               `yaml:"livenessProbe"`
	Resources      ResourceRequirements `yaml:"resources"`
	Line           int                  `yaml:"-"`
}

type ContainerPort struct {
	ContainerPort int    `yaml:"containerPort"`
	Protocol      string `yaml:"protocol"`
	Line          int    `yaml:"-"`
}

type Probe struct {
	HTTPGet HTTPGetAction `yaml:"httpGet"`
	Line    int           `yaml:"-"`
}

type HTTPGetAction struct {
	Path string `yaml:"path"`
	Port int    `yaml:"port"`
	Line int    `yaml:"-"`
}

type ResourceRequirements struct {
	Limits   ResourceList `yaml:"limits"`
	Requests ResourceList `yaml:"requests"`
	Line     int          `yaml:"-"`
}

type ResourceList struct {
	CPU    interface{} `yaml:"cpu"`
	Memory string      `yaml:"memory"`
	Line   int         `yaml:"-"`
}

type ValidationError struct {
	Filename string
	Line     int
	Message  string
}

// Константы для валидации
const (
	validAPIVersion  = "v1"
	validKind        = "Pod"
	validImageDomain = "registry.bigbrother.io/"
)

// Регулярные выражения для валидации
var (
	snakeCaseRegex    = regexp.MustCompile(`^[a-z][a-z0-9_]*(_[a-z0-9]+)*$`)
	memoryRegex       = regexp.MustCompile(`^(\d+)(Gi|Mi|Ki)$`)
	imageRegex        = regexp.MustCompile(`^registry\.bigbrother\.io/[^:]+:.+$`)
	absolutePathRegex = regexp.MustCompile(`^/`)
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <yaml-file>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]
	errors := validateYAMLFile(filename)

	if len(errors) > 0 {
		for _, err := range errors {
			if err.Line > 0 {
				fmt.Fprintf(os.Stderr, "%s:%d %s\n", err.Filename, err.Line, err.Message)
			} else {
				fmt.Fprintf(os.Stderr, "%s: %s\n", err.Filename, err.Message)
			}
		}
		os.Exit(1)
	}

	os.Exit(0)
}

func validateYAMLFile(filename string) []ValidationError {
	content, err := os.ReadFile(filename)
	if err != nil {
		return []ValidationError{{Filename: filename, Message: fmt.Sprintf("cannot read file: %v", err)}}
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return []ValidationError{{Filename: filename, Message: fmt.Sprintf("cannot parse YAML: %v", err)}}
	}

	// Разбираем в структуру для удобства
	var pod Pod
	if err := yaml.Unmarshal(content, &pod); err != nil {
		return []ValidationError{{Filename: filename, Message: fmt.Sprintf("cannot unmarshal YAML: %v", err)}}
	}

	// Собираем информацию о линиях для каждого поля
	extractLineInfo(&root, &pod)

	// Валидируем структуру
	return validatePod(filename, pod)
}

func extractLineInfo(root *yaml.Node, pod *Pod) {
	// Рекурсивно обходим дерево и извлекаем информацию о линиях
	// Это упрощенная версия - в реальном приложении нужно более детальное извлечение
	traverseNodes(root, pod)
}

func traverseNodes(node *yaml.Node, pod *Pod) {
	if node == nil {
		return
	}

	// Здесь можно добавить логику для сопоставления узлов со структурой
	// Для простоты оставляем базовую реализацию
	for _, child := range node.Content {
		traverseNodes(child, pod)
	}
}

func validatePod(filename string, pod Pod) []ValidationError {
	var errors []ValidationError

	// Валидация верхнего уровня
	if pod.APIVersion == "" {
		errors = append(errors, ValidationError{Filename: filename, Message: "apiVersion is required"})
	} else if pod.APIVersion != validAPIVersion {
		errors = append(errors, ValidationError{Filename: filename, Message: fmt.Sprintf("apiVersion has unsupported value '%s'", pod.APIVersion)})
	}

	if pod.Kind == "" {
		errors = append(errors, ValidationError{Filename: filename, Message: "kind is required"})
	} else if pod.Kind != validKind {
		errors = append(errors, ValidationError{Filename: filename, Message: fmt.Sprintf("kind has unsupported value '%s'", pod.Kind)})
	}

	// Валидация metadata
	metaErrors := validateObjectMeta(filename, pod.Metadata)
	errors = append(errors, metaErrors...)

	// Валидация spec
	specErrors := validatePodSpec(filename, pod.Spec)
	errors = append(errors, specErrors...)

	return errors
}

func validateObjectMeta(filename string, meta ObjectMeta) []ValidationError {
	var errors []ValidationError

	if meta.Name == "" {
		//lineMsg := ""
		//if meta.Line > 0 {
		//	lineMsg = fmt.Sprintf(":%d", meta.Line)
		//}
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     meta.Line,
			Message:  "name is required",
		})
	}

	// Namespace и Labels опциональны
	return errors
}

func validatePodSpec(filename string, spec PodSpec) []ValidationError {
	var errors []ValidationError

	// OS опционально, но если указано - валидируем
	if spec.OS != nil {
		osErrors := validatePodOS(filename, *spec.OS)
		errors = append(errors, osErrors...)
	}

	// Containers обязательно
	if len(spec.Containers) == 0 {
		//lineMsg := ""
		//if spec.Line > 0 {
		//	lineMsg = fmt.Sprintf(":%d", spec.Line)
		//}
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     spec.Line,
			Message:  "containers is required",
		})
	} else {
		for i, container := range spec.Containers {
			containerErrors := validateContainer(filename, container)
			// Добавляем индекс контейнера для лучшей диагностики
			for j := range containerErrors {
				containerErrors[j].Message = fmt.Sprintf("container[%d].%s", i, containerErrors[j].Message)
			}
			errors = append(errors, containerErrors...)
		}
	}

	return errors
}

func validatePodOS(filename string, os PodOS) []ValidationError {
	var errors []ValidationError

	if os.Name == "" {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     os.Line,
			Message:  "os.name is required",
		})
	} else if os.Name != "linux" && os.Name != "windows" {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     os.Line,
			Message:  fmt.Sprintf("os.name has unsupported value '%s'", os.Name),
		})
	}

	return errors
}

func validateContainer(filename string, container Container) []ValidationError {
	var errors []ValidationError

	// Name
	if container.Name == "" {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     container.Line,
			Message:  "name is required",
		})
	} else if !snakeCaseRegex.MatchString(container.Name) {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     container.Line,
			Message:  fmt.Sprintf("name has invalid format '%s'", container.Name),
		})
	}

	// Image
	if container.Image == "" {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     container.Line,
			Message:  "image is required",
		})
	} else if !imageRegex.MatchString(container.Image) {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     container.Line,
			Message:  fmt.Sprintf("image has invalid format '%s'", container.Image),
		})
	} else if !strings.HasPrefix(container.Image, validImageDomain) {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     container.Line,
			Message:  fmt.Sprintf("image must be in domain '%s'", validImageDomain),
		})
	} else if !strings.Contains(container.Image, ":") {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     container.Line,
			Message:  "image tag is required",
		})
	}

	// Ports (опционально)
	for i, port := range container.Ports {
		portErrors := validateContainerPort(filename, port)
		for j := range portErrors {
			portErrors[j].Message = fmt.Sprintf("ports[%d].%s", i, portErrors[j].Message)
		}
		errors = append(errors, portErrors...)
	}

	// ReadinessProbe (опционально)
	if container.ReadinessProbe != nil {
		probeErrors := validateProbe(filename, *container.ReadinessProbe)
		for i := range probeErrors {
			probeErrors[i].Message = fmt.Sprintf("readinessProbe.%s", probeErrors[i].Message)
		}
		errors = append(errors, probeErrors...)
	}

	// LivenessProbe (опционально)
	if container.LivenessProbe != nil {
		probeErrors := validateProbe(filename, *container.LivenessProbe)
		for i := range probeErrors {
			probeErrors[i].Message = fmt.Sprintf("livenessProbe.%s", probeErrors[i].Message)
		}
		errors = append(errors, probeErrors...)
	}

	// Resources (обязательно)
	resErrors := validateResourceRequirements(filename, container.Resources)
	for i := range resErrors {
		resErrors[i].Message = fmt.Sprintf("resources.%s", resErrors[i].Message)
	}
	errors = append(errors, resErrors...)

	return errors
}

func validateContainerPort(filename string, port ContainerPort) []ValidationError {
	var errors []ValidationError

	// ContainerPort
	if port.ContainerPort <= 0 || port.ContainerPort >= 65536 {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     port.Line,
			Message:  "containerPort value out of range",
		})
	}

	// Protocol (опционально, по умолчанию TCP)
	if port.Protocol != "" && port.Protocol != "TCP" && port.Protocol != "UDP" {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     port.Line,
			Message:  fmt.Sprintf("protocol has unsupported value '%s'", port.Protocol),
		})
	}

	return errors
}

func validateProbe(filename string, probe Probe) []ValidationError {
	var errors []ValidationError

	// HTTPGet обязательно
	httpErrors := validateHTTPGetAction(filename, probe.HTTPGet)
	for i := range httpErrors {
		httpErrors[i].Message = fmt.Sprintf("httpGet.%s", httpErrors[i].Message)
	}
	errors = append(errors, httpErrors...)

	return errors
}

func validateHTTPGetAction(filename string, httpGet HTTPGetAction) []ValidationError {
	var errors []ValidationError

	// Path
	if httpGet.Path == "" {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     httpGet.Line,
			Message:  "path is required",
		})
	} else if !absolutePathRegex.MatchString(httpGet.Path) {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     httpGet.Line,
			Message:  fmt.Sprintf("path has invalid format '%s' (must be absolute)", httpGet.Path),
		})
	}

	// Port
	if httpGet.Port <= 0 || httpGet.Port >= 65536 {
		errors = append(errors, ValidationError{
			Filename: filename,
			Line:     httpGet.Line,
			Message:  "port value out of range",
		})
	}

	return errors
}

func validateResourceRequirements(filename string, resources ResourceRequirements) []ValidationError {
	var errors []ValidationError

	// Limits и Requests опциональны, но валидируем если указаны
	if resources.Limits.CPU != nil || resources.Limits.Memory != "" {
		limitErrors := validateResourceList(filename, resources.Limits, "limits")
		errors = append(errors, limitErrors...)
	}

	if resources.Requests.CPU != nil || resources.Requests.Memory != "" {
		requestErrors := validateResourceList(filename, resources.Requests, "requests")
		errors = append(errors, requestErrors...)
	}

	return errors
}

func validateResourceList(filename string, resources ResourceList, prefix string) []ValidationError {
	var errors []ValidationError

	// CPU
	if resources.CPU != nil {
		switch v := resources.CPU.(type) {
		case int:
			// OK
		case string:
			// Пробуем преобразовать строку в int
			if _, err := strconv.Atoi(v); err != nil {
				errors = append(errors, ValidationError{
					Filename: filename,
					Line:     resources.Line,
					Message:  fmt.Sprintf("%s.cpu must be int", prefix),
				})
			}
		default:
			errors = append(errors, ValidationError{
				Filename: filename,
				Line:     resources.Line,
				Message:  fmt.Sprintf("%s.cpu must be int", prefix),
			})
		}
	}

	// Memory
	if resources.Memory != "" {
		if !memoryRegex.MatchString(resources.Memory) {
			errors = append(errors, ValidationError{
				Filename: filename,
				Line:     resources.Line,
				Message:  fmt.Sprintf("%s.memory has invalid format '%s'", prefix, resources.Memory),
			})
		}
	}

	return errors
}
