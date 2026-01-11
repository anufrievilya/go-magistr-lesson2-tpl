package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Регулярные выражения для валидации различных форматов
var (
	// Проверка формата snake_case (маленькие буквы, цифры, подчеркивания)
	snakeCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	// Проверка формата памяти (число + единица измерения Gi/Mi/Ki)
	memoryRegex = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)

	// Проверка формата образа (должен быть из домена registry.bigbrother.io с тегом)
	imageRegex = regexp.MustCompile(`^registry\.bigbrother\.io/[^:]+:.+$`)

	// Проверка абсолютного пути (должен начинаться с /)
	absolutePathRegex = regexp.MustCompile(`^/`)
)

// Валидатор - структура для хранения информации об ошибках и имени файла
type Validator struct {
	filename string   // Имя проверяемого файла
	errors   []string // Список найденных ошибок
}

// Создание нового валидатора
func NewValidator(filename string) *Validator {
	return &Validator{
		filename: filename,
		errors:   []string{},
	}
}

// Добавление ошибки в список
// Если line > 0, добавляется номер строки, иначе только сообщение
func (v *Validator) addError(line int, message string) {
	if line > 0 {
		v.errors = append(v.errors, fmt.Sprintf("%s:%d %s", v.filename, line, message))
	} else {
		v.errors = append(v.errors, fmt.Sprintf("%s %s", v.filename, message))
	}
}

// Проверка наличия ошибок
func (v *Validator) hasErrors() bool {
	return len(v.errors) > 0
}

// Вывод всех ошибок в stderr
func (v *Validator) printErrors() {
	for _, err := range v.errors {
		fmt.Fprintln(os.Stderr, err)
	}
}

// Преобразование YAML узла в map для удобного доступа к полям
// YAML хранит пары ключ-значение последовательно, нужно их распарсить
func (v *Validator) parseMap(node *yaml.Node) map[string]*yaml.Node {
	result := make(map[string]*yaml.Node)
	// Проходим по парам: ключ (четный индекс), значение (нечетный индекс)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		result[key] = value
	}
	return result
}

// Главная функция валидации - проверяет весь документ
func (v *Validator) Validate(root *yaml.Node) {
	// Проверка что файл не пустой
	if len(root.Content) == 0 {
		v.addError(0, "empty YAML file")
		return
	}

	// Получаем корневой документ
	doc := root.Content[0]

	// Преобразуем в map для удобства
	fields := v.parseMap(doc)

	// Валидация полей верхнего уровня
	v.validateTopLevel(fields)
}

// Валидация полей верхнего уровня (apiVersion, kind, metadata, spec)
func (v *Validator) validateTopLevel(fields map[string]*yaml.Node) {
	// Проверка apiVersion - обязательное поле, должно быть "v1"
	if apiVersion, ok := fields["apiVersion"]; !ok {
		v.addError(0, "apiVersion is required")
	} else if apiVersion.Value != "v1" {
		v.addError(apiVersion.Line, "apiVersion has unsupported value '"+apiVersion.Value+"'")
	}

	// Проверка kind - обязательное поле, должно быть "Pod"
	if kind, ok := fields["kind"]; !ok {
		v.addError(0, "kind is required")
	} else if kind.Value != "Pod" {
		v.addError(kind.Line, "kind has unsupported value '"+kind.Value+"'")
	}

	// Проверка metadata - обязательное поле, должно быть объектом
	if metadata, ok := fields["metadata"]; !ok {
		v.addError(0, "metadata is required")
	} else {
		v.validateMetadata(metadata)
	}

	// Проверка spec - обязательное поле, должно быть объектом
	if spec, ok := fields["spec"]; !ok {
		v.addError(0, "spec is required")
	} else {
		v.validateSpec(spec)
	}
}

// Валидация секции metadata
func (v *Validator) validateMetadata(node *yaml.Node) {
	fields := v.parseMap(node)

	// name - обязательное поле, не должно быть пустым
	if name, ok := fields["name"]; !ok {
		v.addError(0, "metadata.name is required")
	} else if strings.TrimSpace(name.Value) == "" {
		// Если name пустая строка, это тоже ошибка
		v.addError(name.Line, "name is required")
	}

	// namespace - необязательное поле, проверка не требуется
	// labels - необязательное поле, проверка не требуется
}

// Валидация секции spec
func (v *Validator) validateSpec(node *yaml.Node) {
	fields := v.parseMap(node)

	// os - необязательное поле, но если есть, должно быть linux или windows
	if osNode, ok := fields["os"]; ok {
		if osNode.Value != "linux" && osNode.Value != "windows" {
			v.addError(osNode.Line, "os has unsupported value '"+osNode.Value+"'")
		}
	}

	// containers - обязательное поле, должен быть массив
	if containers, ok := fields["containers"]; !ok {
		v.addError(0, "spec.containers is required")
	} else {
		// Проходим по каждому контейнеру в массиве
		for _, container := range containers.Content {
			v.validateContainer(container)
		}
	}
}

// Валидация одного контейнера
func (v *Validator) validateContainer(node *yaml.Node) {
	fields := v.parseMap(node)

	// name - обязательное поле, формат snake_case
	if name, ok := fields["name"]; !ok {
		v.addError(0, "containers.name is required")
	} else if strings.TrimSpace(name.Value) == "" {
		// Пустое имя - ошибка
		v.addError(name.Line, "name is required")
	} else if !snakeCaseRegex.MatchString(name.Value) {
		// Неправильный формат snake_case
		v.addError(name.Line, "containers.name has invalid format '"+name.Value+"'")
	}

	// image - обязательное поле, должен быть из домена registry.bigbrother.io с тегом
	if image, ok := fields["image"]; !ok {
		v.addError(0, "containers.image is required")
	} else if !imageRegex.MatchString(image.Value) {
		v.addError(image.Line, "containers.image has invalid format '"+image.Value+"'")
	}

	// ports - необязательное поле
	if ports, ok := fields["ports"]; ok {
		for _, port := range ports.Content {
			v.validatePort(port)
		}
	}

	// readinessProbe - необязательное поле
	if probe, ok := fields["readinessProbe"]; ok {
		v.validateProbe(probe)
	}

	// livenessProbe - необязательное поле
	if probe, ok := fields["livenessProbe"]; ok {
		v.validateProbe(probe)
	}

	// resources - обязательное поле
	if resources, ok := fields["resources"]; !ok {
		v.addError(0, "containers.resources is required")
	} else {
		v.validateResources(resources)
	}
}

// Валидация порта контейнера
func (v *Validator) validatePort(node *yaml.Node) {
	fields := v.parseMap(node)

	// containerPort - должен быть числом в диапазоне 0 < port < 65536
	if containerPort, ok := fields["containerPort"]; ok {
		// Парсим как число (может быть большое число)
		portNum, err := strconv.ParseInt(containerPort.Value, 10, 64)
		if err != nil {
			v.addError(containerPort.Line, "containerPort must be int")
		} else if portNum <= 0 || portNum >= 65536 {
			v.addError(containerPort.Line, "containerPort value out of range")
		}
	}

	// protocol - необязательное поле, если есть - должно быть TCP или UDP
	if protocol, ok := fields["protocol"]; ok {
		if protocol.Value != "TCP" && protocol.Value != "UDP" {
			v.addError(protocol.Line, "protocol has unsupported value '"+protocol.Value+"'")
		}
	}
}

// Валидация probe (readinessProbe или livenessProbe)
func (v *Validator) validateProbe(node *yaml.Node) {
	fields := v.parseMap(node)

	// httpGet - обязательное поле для probe
	if httpGet, ok := fields["httpGet"]; ok {
		v.validateHTTPGet(httpGet)
	}
}

// Валидация httpGet секции в probe
func (v *Validator) validateHTTPGet(node *yaml.Node) {
	fields := v.parseMap(node)

	// path - должен быть абсолютным путем (начинается с /)
	if path, ok := fields["path"]; ok {
		if !absolutePathRegex.MatchString(path.Value) {
			v.addError(path.Line, "path has invalid format '"+path.Value+"'")
		}
	}

	// port - должен быть числом в диапазоне 0 < port < 65536
	if port, ok := fields["port"]; ok {
		portNum, err := strconv.ParseInt(port.Value, 10, 64)
		if err != nil {
			v.addError(port.Line, "port must be int")
		} else if portNum <= 0 || portNum >= 65536 {
			v.addError(port.Line, "port value out of range")
		}
	}
}

// Валидация секции resources
func (v *Validator) validateResources(node *yaml.Node) {
	fields := v.parseMap(node)

	// limits - необязательное поле
	if limits, ok := fields["limits"]; ok {
		v.validateResourceList(limits)
	}

	// requests - необязательное поле
	if requests, ok := fields["requests"]; ok {
		v.validateResourceList(requests)
	}
}

// Валидация списка ресурсов (limits или requests)
func (v *Validator) validateResourceList(node *yaml.Node) {
	fields := v.parseMap(node)

	// cpu - должно быть целым числом (не строкой!)
	if cpu, ok := fields["cpu"]; ok {
		// ВАЖНО: если YAML парсер определил это как строку (тег !!str),
		// значит в файле было "1" вместо 1 - это ошибка
		if cpu.Tag == "!!str" {
			v.addError(cpu.Line, "cpu must be int")
		} else {
			// Дополнительная проверка что это число
			_, err := strconv.Atoi(cpu.Value)
			if err != nil {
				v.addError(cpu.Line, "cpu must be int")
			}
		}
	}

	// memory - должно быть строкой в формате "числоGi/Mi/Ki"
	if memory, ok := fields["memory"]; ok {
		if !memoryRegex.MatchString(memory.Value) {
			v.addError(memory.Line, "memory has invalid format '"+memory.Value+"'")
		}
	}
}

// Главная функция программы
func main() {
	// Проверка аргументов командной строки
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: yamlvalid <yaml-file>")
		os.Exit(1)
	}

	filename := os.Args[1]

	// Чтение файла
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read file: %v\n", err)
		os.Exit(1)
	}

	// Парсинг YAML в структуру Node
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintf(os.Stderr, "cannot parse YAML: %v\n", err)
		os.Exit(1)
	}

	// Создание валидатора и запуск проверки
	validator := NewValidator(filename)
	validator.Validate(&root)

	// Если есть ошибки - выводим их и завершаем с кодом 1
	if validator.hasErrors() {
		validator.printErrors()
		os.Exit(1)
	}

	// Если ошибок нет - завершаем с кодом 0 (успех)
	os.Exit(0)
}
